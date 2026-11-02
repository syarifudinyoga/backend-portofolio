package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	sessionHeader     = "X-Session-Id"
	sessionTTL        = 30 * time.Minute
	maxSessions       = 10000
	maxSessionRequest = 1024
	sessionKDFInfo    = "portfolio-e2e-v1"
)

type encryptedEnvelope struct {
	Encrypted bool   `json:"encrypted"`
	IV        string `json:"iv"`
	Data      string `json:"data"`
}

type cryptoSession struct {
	key     []byte
	expires time.Time
}

// sessionStore holds per-session AES keys derived via ECDH; keys never leave memory.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]cryptoSession
}

var sessions = &sessionStore{sessions: map[string]cryptoSession{}}

func (s *sessionStore) put(id string, key []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if len(s.sessions) >= maxSessions {
		for k, v := range s.sessions {
			if now.After(v.expires) {
				delete(s.sessions, k)
			}
		}
		if len(s.sessions) >= maxSessions {
			return errors.New("too many active sessions")
		}
	}
	s.sessions[id] = cryptoSession{key: key, expires: now.Add(sessionTTL)}
	return nil
}

func (s *sessionStore) get(id string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	if time.Now().After(sess.expires) {
		delete(s.sessions, id)
		return nil, false
	}
	return sess.key, true
}

func sessionKey(r *http.Request) ([]byte, bool) {
	id := r.Header.Get(sessionHeader)
	if id == "" {
		return nil, false
	}
	return sessions.get(id)
}

type sessionRequest struct {
	PublicKey string `json:"publicKey"`
}

type sessionResponse struct {
	SessionID string `json:"sessionId"`
	PublicKey string `json:"publicKey"`
	ExpiresIn int    `json:"expiresIn"`
}

// handleSession performs an ECDH (P-256) handshake and derives a per-session AES-256 key with HKDF.
func handleSession(w http.ResponseWriter, r *http.Request) {
	var req sessionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxSessionRequest)).Decode(&req); err != nil {
		http.Error(w, "request JSON tidak valid", http.StatusBadRequest)
		return
	}
	rawClientPub, err := base64.StdEncoding.DecodeString(req.PublicKey)
	if err != nil {
		http.Error(w, "public key tidak valid", http.StatusBadRequest)
		return
	}
	curve := ecdh.P256()
	clientPub, err := curve.NewPublicKey(rawClientPub)
	if err != nil {
		http.Error(w, "public key tidak valid", http.StatusBadRequest)
		return
	}
	serverPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		http.Error(w, "gagal membuat sesi", http.StatusInternalServerError)
		return
	}
	shared, err := serverPriv.ECDH(clientPub)
	if err != nil {
		http.Error(w, "public key tidak valid", http.StatusBadRequest)
		return
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		http.Error(w, "gagal membuat sesi", http.StatusInternalServerError)
		return
	}
	key, err := hkdf.Key(sha256.New, shared, idBytes, sessionKDFInfo, 32)
	if err != nil {
		http.Error(w, "gagal membuat sesi", http.StatusInternalServerError)
		return
	}
	id := base64.RawURLEncoding.EncodeToString(idBytes)
	if err := sessions.put(id, key); err != nil {
		http.Error(w, "server sibuk, coba lagi nanti", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(sessionResponse{
		SessionID: id,
		PublicKey: base64.StdEncoding.EncodeToString(serverPriv.PublicKey().Bytes()),
		ExpiresIn: int(sessionTTL.Seconds()),
	})
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher new gcm: %w", err)
	}
	return gcm, nil
}

func encryptData(key, plaintext []byte) (encryptedEnvelope, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return encryptedEnvelope{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return encryptedEnvelope{}, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	return encryptedEnvelope{
		Encrypted: true,
		IV:        base64.StdEncoding.EncodeToString(nonce),
		Data:      base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func decryptData(key []byte, ivB64, dataB64 string) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(ivB64)
	if err != nil {
		return nil, fmt.Errorf("decode iv: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid nonce size")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt ciphertext: %w", err)
	}
	return plaintext, nil
}

func writeEncryptedJSON(w http.ResponseWriter, r *http.Request, statusCode int, payload any) error {
	key, ok := sessionKey(r)
	if !ok {
		http.Error(w, "sesi enkripsi tidak ditemukan atau kedaluwarsa", http.StatusUnauthorized)
		return nil
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	envelope, err := encryptData(key, raw)
	if err != nil {
		return fmt.Errorf("encrypt payload: %w", err)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Payload-Encrypted", "true")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(envelope)
}

func readPayload(r *http.Request, target any) error {
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 50*1024*1024))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	// Try checking if it's an encrypted envelope
	var envelope encryptedEnvelope
	if err := json.Unmarshal(bodyBytes, &envelope); err == nil && envelope.Encrypted && envelope.IV != "" && envelope.Data != "" {
		key, ok := sessionKey(r)
		if !ok {
			return errors.New("sesi enkripsi tidak ditemukan atau kedaluwarsa")
		}
		decrypted, err := decryptData(key, envelope.IV, envelope.Data)
		if err != nil {
			return fmt.Errorf("decrypt body: %w", err)
		}
		if err := json.Unmarshal(decrypted, target); err != nil {
			return fmt.Errorf("unmarshal decrypted payload: %w", err)
		}
		return nil
	}

	// Otherwise, fallback to plaintext JSON
	if err := json.Unmarshal(bodyBytes, target); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}
	return nil
}
