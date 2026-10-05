package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	maxUploadBytes = 8 << 20
	mediaPrefix    = "images/"
)

var allowedImageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

type mediaStore struct {
	client *minio.Client
	bucket string
}

// connectMedia returns nil when MinIO is not configured, so uploads stay optional.
func connectMedia(ctx context.Context, logger *log.Logger) *mediaStore {
	endpoint := strings.TrimSpace(os.Getenv("MINIO_ENDPOINT"))
	accessKey := strings.TrimSpace(os.Getenv("MINIO_ACCESS_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("MINIO_SECRET_KEY"))
	if endpoint == "" || accessKey == "" || secretKey == "" {
		logger.Printf("minio: not configured (set MINIO_ENDPOINT, MINIO_ACCESS_KEY, MINIO_SECRET_KEY); image upload disabled")
		return nil
	}
	bucket := envOr("MINIO_BUCKET", "portfolio")
	useSSL, _ := strconv.ParseBool(os.Getenv("MINIO_USE_SSL"))
	logger.Printf("minio: connecting endpoint=%s bucket=%s ssl=%t", endpoint, bucket, useSSL)

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		logger.Printf("minio: connection failed: %v", err)
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	exists, err := client.BucketExists(checkCtx, bucket)
	if err != nil {
		logger.Printf("minio: connection failed: %v", err)
		return nil
	}
	if !exists {
		if err := client.MakeBucket(checkCtx, bucket, minio.MakeBucketOptions{}); err != nil {
			logger.Printf("minio: create bucket %q failed: %v", bucket, err)
			return nil
		}
		logger.Printf("minio: bucket %q created", bucket)
	}
	logger.Printf("minio: connected successfully bucket=%s", bucket)
	return &mediaStore{client: client, bucket: bucket}
}

func (m *mediaStore) handleUpload(w http.ResponseWriter, r *http.Request, logger *log.Logger) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes+1<<20)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		http.Error(w, "upload gagal atau file lebih dari 8 MB", http.StatusRequestEntityTooLarge)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "field file wajib diisi", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if header.Size > maxUploadBytes {
		http.Error(w, "ukuran file maksimal 8 MB", http.StatusRequestEntityTooLarge)
		return
	}
	sniff := make([]byte, 512)
	n, err := io.ReadFull(file, sniff)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		http.Error(w, "file tidak bisa dibaca", http.StatusBadRequest)
		return
	}
	contentType := http.DetectContentType(sniff[:n])
	ext, ok := allowedImageTypes[contentType]
	if !ok {
		http.Error(w, "format harus JPG, PNG, WebP, atau GIF", http.StatusUnsupportedMediaType)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "file tidak bisa dibaca", http.StatusBadRequest)
		return
	}
	token := make([]byte, 12)
	if _, err := rand.Read(token); err != nil {
		http.Error(w, "gagal membuat nama file", http.StatusInternalServerError)
		return
	}
	name := mediaPrefix + hex.EncodeToString(token) + ext
	if _, err := m.client.PutObject(r.Context(), m.bucket, name, file, header.Size,
		minio.PutObjectOptions{ContentType: contentType}); err != nil {
		logger.Printf("minio: upload %s failed: %v", name, err)
		http.Error(w, "gagal menyimpan ke MinIO: "+err.Error(), http.StatusBadGateway)
		return
	}
	logger.Printf("minio: uploaded %s (%d bytes)", name, header.Size)
	writeJSON(w, http.StatusCreated, map[string]string{"url": "/api/media/" + name})
}

func (m *mediaStore) handleServe(w http.ResponseWriter, r *http.Request, logger *log.Logger) {
	name := r.PathValue("name")
	if strings.Contains(name, "..") || !strings.HasPrefix(name, mediaPrefix) || strings.ContainsAny(name, "\\\x00") {
		http.NotFound(w, r)
		return
	}
	object, err := m.client.GetObject(r.Context(), m.bucket, name, minio.GetObjectOptions{})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer object.Close()
	info, err := object.Stat()
	if err != nil {
		if minio.ToErrorResponse(err).StatusCode == http.StatusNotFound {
			http.NotFound(w, r)
			return
		}
		logger.Printf("minio: read %s failed: %v", name, err)
		http.Error(w, "gambar tidak tersedia", http.StatusBadGateway)
		return
	}
	if _, ok := map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "image/gif": true}[info.ContentType]; !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", fmt.Sprint(info.Size))
	if _, err := io.Copy(w, object); err != nil {
		logger.Printf("minio: stream %s: %v", name, err)
	}
}
