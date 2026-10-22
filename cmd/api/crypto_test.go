package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetEncryptionKey(t *testing.T) {
	t.Run("default key", func(t *testing.T) {
		t.Setenv("API_ENCRYPTION_SECRET", "")
		key := getEncryptionKey()
		if len(key) != 32 {
			t.Fatalf("expected 32 bytes key, got %d", len(key))
		}
	})

	t.Run("custom key", func(t *testing.T) {
		t.Setenv("API_ENCRYPTION_SECRET", "custom-secret-password-123")
		customKey := getEncryptionKey()
		t.Setenv("API_ENCRYPTION_SECRET", "another-secret-456")
		anotherKey := getEncryptionKey()

		if bytes.Equal(customKey[:], anotherKey[:]) {
			t.Fatal("expected different keys for different secrets")
		}
	})
}

func TestEncryptDecryptData(t *testing.T) {
	t.Setenv("API_ENCRYPTION_SECRET", "test-secret-key-32-bytes-long!")
	original := []byte("Hello, SonarQube & Unit Testing in Go!")

	envelope, err := encryptData(original)
	if err != nil {
		t.Fatalf("encryptData failed: %v", err)
	}

	if !envelope.Encrypted {
		t.Fatal("envelope.Encrypted should be true")
	}
	if envelope.IV == "" || envelope.Data == "" {
		t.Fatal("envelope IV and Data must not be empty")
	}

	decrypted, err := decryptData(envelope.IV, envelope.Data)
	if err != nil {
		t.Fatalf("decryptData failed: %v", err)
	}

	if string(decrypted) != string(original) {
		t.Fatalf("decrypted data %q != original %q", string(decrypted), string(original))
	}
}

func TestDecryptDataErrors(t *testing.T) {
	t.Setenv("API_ENCRYPTION_SECRET", "test-secret-key-32-bytes-long!")

	t.Run("invalid iv base64", func(t *testing.T) {
		_, err := decryptData("!not-base64!", "validbase64==")
		if err == nil {
			t.Fatal("expected error for invalid IV base64")
		}
	})

	t.Run("invalid nonce size", func(t *testing.T) {
		shortNonce := base64.StdEncoding.EncodeToString([]byte("short"))
		_, err := decryptData(shortNonce, "validbase64==")
		if err == nil {
			t.Fatal("expected error for invalid nonce size")
		}
	})

	t.Run("invalid ciphertext base64", func(t *testing.T) {
		validNonce := base64.StdEncoding.EncodeToString(make([]byte, 12))
		_, err := decryptData(validNonce, "!not-base64!")
		if err == nil {
			t.Fatal("expected error for invalid ciphertext base64")
		}
	})

	t.Run("corrupted ciphertext", func(t *testing.T) {
		envelope, err := encryptData([]byte("test message"))
		if err != nil {
			t.Fatalf("encryptData failed: %v", err)
		}
		// Tamper with data
		corruptedData := base64.StdEncoding.EncodeToString([]byte("corruptedciphertextlongerthan16bytes"))
		_, err = decryptData(envelope.IV, corruptedData)
		if err == nil {
			t.Fatal("expected decryption error for tampered ciphertext")
		}
	})
}

func TestWriteEncryptedJSON(t *testing.T) {
	t.Setenv("API_ENCRYPTION_SECRET", "test-secret-key-for-write-json")
	rec := httptest.NewRecorder()

	payload := map[string]string{"status": "ok", "project": "portfolio"}
	err := writeEncryptedJSON(rec, http.StatusAccepted, payload)
	if err != nil {
		t.Fatalf("writeEncryptedJSON failed: %v", err)
	}

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, rec.Code)
	}
	if rec.Header().Get("X-Payload-Encrypted") != "true" {
		t.Fatalf("expected X-Payload-Encrypted header to be 'true'")
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected Content-Type application/json, got %q", rec.Header().Get("Content-Type"))
	}

	var env encryptedEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to decode response as encryptedEnvelope: %v", err)
	}

	decrypted, err := decryptData(env.IV, env.Data)
	if err != nil {
		t.Fatalf("failed to decrypt response data: %v", err)
	}

	var res map[string]string
	if err := json.Unmarshal(decrypted, &res); err != nil {
		t.Fatalf("failed to unmarshal decrypted payload: %v", err)
	}
	if res["project"] != "portfolio" {
		t.Fatalf("unexpected decrypted value: %v", res)
	}
}

func TestReadPayload(t *testing.T) {
	t.Setenv("API_ENCRYPTION_SECRET", "test-secret-key-for-read-payload")

	t.Run("plaintext JSON payload", func(t *testing.T) {
		body := `{"key": "my-secret-key"}`
		req := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(body))

		var target authRequest
		if err := readPayload(req, &target); err != nil {
			t.Fatalf("readPayload plaintext failed: %v", err)
		}
		if target.Key != "my-secret-key" {
			t.Fatalf("expected key %q, got %q", "my-secret-key", target.Key)
		}
	})

	t.Run("encrypted envelope payload", func(t *testing.T) {
		original := authRequest{Key: "secret-bearer-token"}
		raw, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		envelope, err := encryptData(raw)
		if err != nil {
			t.Fatalf("encryptData failed: %v", err)
		}

		envelopeBytes, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal envelope failed: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/auth", bytes.NewReader(envelopeBytes))
		var target authRequest
		if err := readPayload(req, &target); err != nil {
			t.Fatalf("readPayload encrypted failed: %v", err)
		}
		if target.Key != original.Key {
			t.Fatalf("expected key %q, got %q", original.Key, target.Key)
		}
	})

	t.Run("invalid encrypted envelope with bad ciphertext", func(t *testing.T) {
		fakeEnv := encryptedEnvelope{
			Encrypted: true,
			IV:        base64.StdEncoding.EncodeToString(make([]byte, 12)),
			Data:      base64.StdEncoding.EncodeToString([]byte("bad-encrypted-data")),
		}
		fakeBytes, _ := json.Marshal(fakeEnv)
		req := httptest.NewRequest(http.MethodPost, "/auth", bytes.NewReader(fakeBytes))

		var target authRequest
		if err := readPayload(req, &target); err == nil {
			t.Fatal("expected error on bad encrypted payload, got nil")
		}
	})

	t.Run("invalid unencrypted json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(`{invalid-json`))
		var target authRequest
		if err := readPayload(req, &target); err == nil {
			t.Fatal("expected error on malformed json, got nil")
		}
	})
}
