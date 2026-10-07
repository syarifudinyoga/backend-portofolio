package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const defaultEncryptionSecret = "portfolio-vault-key-2026-secure-secret-token"

type encryptedEnvelope struct {
	Encrypted bool   `json:"encrypted"`
	IV        string `json:"iv"`
	Data      string `json:"data"`
}

func getEncryptionKey() [32]byte {
	secret := strings.TrimSpace(os.Getenv("API_ENCRYPTION_SECRET"))
	if secret == "" {
		secret = defaultEncryptionSecret
	}
	return sha256.Sum256([]byte(secret))
}

func encryptData(plaintext []byte) (encryptedEnvelope, error) {
	key := getEncryptionKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return encryptedEnvelope{}, fmt.Errorf("aes new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return encryptedEnvelope{}, fmt.Errorf("cipher new gcm: %w", err)
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

func decryptData(ivB64, dataB64 string) ([]byte, error) {
	key := getEncryptionKey()
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("aes new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher new gcm: %w", err)
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

func writeEncryptedJSON(w http.ResponseWriter, statusCode int, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	envelope, err := encryptData(raw)
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
		decrypted, err := decryptData(envelope.IV, envelope.Data)
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
