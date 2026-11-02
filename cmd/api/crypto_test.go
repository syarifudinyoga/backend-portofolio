package main

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestSession registers a random AES key as an active session and returns its ID and key.
func newTestSession(t *testing.T) (string, []byte) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	id := base64.RawURLEncoding.EncodeToString(key[:16])
	if err := sessions.put(id, key); err != nil {
		t.Fatalf("put session: %v", err)
	}
	return id, key
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	original := []byte(`{"hello":"world"}`)

	envelope, err := encryptData(key, original)
	if err != nil {
		t.Fatalf("encryptData failed: %v", err)
	}
	if !envelope.Encrypted || envelope.IV == "" || envelope.Data == "" {
		t.Fatalf("invalid envelope: %+v", envelope)
	}

	decrypted, err := decryptData(key, envelope.IV, envelope.Data)
	if err != nil {
		t.Fatalf("decryptData failed: %v", err)
	}
	if !bytes.Equal(decrypted, original) {
		t.Fatalf("expected %q, got %q", original, decrypted)
	}
}

func TestDecryptDataErrors(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)

	t.Run("invalid key size", func(t *testing.T) {
		if _, err := encryptData([]byte("short"), []byte("x")); err == nil {
			t.Fatal("expected error for invalid key size")
		}
		if _, err := decryptData([]byte("short"), "a", "b"); err == nil {
			t.Fatal("expected error for invalid key size")
		}
	})
	t.Run("invalid base64 iv", func(t *testing.T) {
		if _, err := decryptData(key, "!not-base64!", "validbase64=="); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid nonce size", func(t *testing.T) {
		short := base64.StdEncoding.EncodeToString([]byte("short"))
		if _, err := decryptData(key, short, "validbase64=="); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid base64 data", func(t *testing.T) {
		iv := base64.StdEncoding.EncodeToString(make([]byte, 12))
		if _, err := decryptData(key, iv, "!not-base64!"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("tampered ciphertext and wrong key", func(t *testing.T) {
		envelope, err := encryptData(key, []byte("test message"))
		if err != nil {
			t.Fatalf("encryptData failed: %v", err)
		}
		if _, err := decryptData(key, envelope.IV, base64.StdEncoding.EncodeToString([]byte("garbage-garbage-garbage"))); err == nil {
			t.Fatal("expected error for tampered ciphertext")
		}
		if _, err := decryptData(bytes.Repeat([]byte{9}, 32), envelope.IV, envelope.Data); err == nil {
			t.Fatal("expected error for wrong key")
		}
	})
}

func TestHandleSession(t *testing.T) {
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handleSession(rec, req)
		return rec
	}

	t.Run("derives matching key on both sides", func(t *testing.T) {
		clientPriv, err := ecdh.P256().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(sessionRequest{PublicKey: base64.StdEncoding.EncodeToString(clientPriv.PublicKey().Bytes())})
		rec := post(string(body))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp sessionResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}

		rawServerPub, _ := base64.StdEncoding.DecodeString(resp.PublicKey)
		serverPub, err := ecdh.P256().NewPublicKey(rawServerPub)
		if err != nil {
			t.Fatal(err)
		}
		shared, err := clientPriv.ECDH(serverPub)
		if err != nil {
			t.Fatal(err)
		}
		salt, _ := base64.RawURLEncoding.DecodeString(resp.SessionID)
		clientKey, err := hkdf.Key(sha256.New, shared, salt, sessionKDFInfo, 32)
		if err != nil {
			t.Fatal(err)
		}

		serverKey, ok := sessions.get(resp.SessionID)
		if !ok {
			t.Fatal("session was not stored")
		}
		if !bytes.Equal(serverKey, clientKey) {
			t.Fatal("derived keys differ")
		}
	})

	t.Run("rejects invalid input", func(t *testing.T) {
		for _, body := range []string{`{bad`, `{"publicKey":"!!"}`, `{"publicKey":"AAAA"}`} {
			if rec := post(body); rec.Code != http.StatusBadRequest {
				t.Fatalf("body %q: expected 400, got %d", body, rec.Code)
			}
		}
	})
}

func TestSessionStoreExpiry(t *testing.T) {
	store := &sessionStore{sessions: map[string]cryptoSession{}}
	if err := store.put("a", []byte("k")); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.get("a"); !ok {
		t.Fatal("expected active session")
	}
	s := store.sessions["a"]
	s.expires = s.expires.Add(-2 * sessionTTL)
	store.sessions["a"] = s
	if _, ok := store.get("a"); ok {
		t.Fatal("expected expired session to be rejected")
	}
	if _, ok := store.get("missing"); ok {
		t.Fatal("expected missing session to be rejected")
	}
}

func TestWriteEncryptedJSON(t *testing.T) {
	id, key := newTestSession(t)

	t.Run("encrypts with session key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(sessionHeader, id)
		rec := httptest.NewRecorder()

		if err := writeEncryptedJSON(rec, req, http.StatusAccepted, map[string]string{"project": "portfolio"}); err != nil {
			t.Fatalf("writeEncryptedJSON failed: %v", err)
		}
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected status %d, got %d", http.StatusAccepted, rec.Code)
		}
		if rec.Header().Get("X-Payload-Encrypted") != "true" {
			t.Fatal("expected X-Payload-Encrypted header")
		}

		var env encryptedEnvelope
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		decrypted, err := decryptData(key, env.IV, env.Data)
		if err != nil {
			t.Fatal(err)
		}
		var res map[string]string
		if err := json.Unmarshal(decrypted, &res); err != nil || res["project"] != "portfolio" {
			t.Fatalf("unexpected payload: %v %v", res, err)
		}
	})

	t.Run("requires session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		_ = writeEncryptedJSON(rec, req, http.StatusOK, map[string]string{"a": "b"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "sesi enkripsi") {
			t.Fatalf("unexpected body: %q", rec.Body.String())
		}
	})
}

func TestReadPayload(t *testing.T) {
	id, key := newTestSession(t)

	t.Run("plaintext JSON payload", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(`{"key": "my-secret-key"}`))
		var target authRequest
		if err := readPayload(req, &target); err != nil {
			t.Fatalf("readPayload plaintext failed: %v", err)
		}
		if target.Key != "my-secret-key" {
			t.Fatalf("unexpected key %q", target.Key)
		}
	})

	t.Run("encrypted envelope payload", func(t *testing.T) {
		raw, _ := json.Marshal(authRequest{Key: "secret-bearer-token"})
		envelope, err := encryptData(key, raw)
		if err != nil {
			t.Fatal(err)
		}
		envelopeBytes, _ := json.Marshal(envelope)

		req := httptest.NewRequest(http.MethodPost, "/auth", bytes.NewReader(envelopeBytes))
		req.Header.Set(sessionHeader, id)
		var target authRequest
		if err := readPayload(req, &target); err != nil {
			t.Fatalf("readPayload encrypted failed: %v", err)
		}
		if target.Key != "secret-bearer-token" {
			t.Fatalf("unexpected key %q", target.Key)
		}

		noSession := httptest.NewRequest(http.MethodPost, "/auth", bytes.NewReader(envelopeBytes))
		if err := readPayload(noSession, &target); err == nil {
			t.Fatal("expected error without session")
		}
	})

	t.Run("invalid encrypted envelope with bad ciphertext", func(t *testing.T) {
		fakeBytes, _ := json.Marshal(encryptedEnvelope{
			Encrypted: true,
			IV:        base64.StdEncoding.EncodeToString(make([]byte, 12)),
			Data:      base64.StdEncoding.EncodeToString([]byte("bad-encrypted-data")),
		})
		req := httptest.NewRequest(http.MethodPost, "/auth", bytes.NewReader(fakeBytes))
		req.Header.Set(sessionHeader, id)
		var target authRequest
		if err := readPayload(req, &target); err == nil {
			t.Fatal("expected error on bad encrypted payload")
		}
	})

	t.Run("invalid unencrypted json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(`{invalid-json`))
		var target authRequest
		if err := readPayload(req, &target); err == nil {
			t.Fatal("expected error on malformed json")
		}
	})
}
