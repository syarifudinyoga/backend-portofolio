package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maxAdminPayload = 2 << 20

type authRequest struct {
	Key string `json:"key"`
}

func handleAdminAuth(w http.ResponseWriter, r *http.Request) {
	var request authRequest
	if err := readPayload(r, &request); err != nil {
		http.Error(w, "request JSON tidak valid", http.StatusBadRequest)
		return
	}
	if !validAdminKey(request.Key) {
		http.Error(w, "key tidak valid", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := writeEncryptedJSON(w, http.StatusOK, map[string]bool{"authenticated": true}); err != nil {
		http.Error(w, "gagal mengenkripsi respons", http.StatusInternalServerError)
	}
}

func authorizeAdmin(w http.ResponseWriter, r *http.Request) bool {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) || !validAdminKey(strings.TrimSpace(strings.TrimPrefix(header, prefix))) {
		http.Error(w, "akses admin tidak diizinkan", http.StatusUnauthorized)
		return false
	}
	return true
}

func validAdminKey(candidate string) bool {
	configured := os.Getenv("ADMIN_KEY")
	if len(configured) < 32 || candidate == "" {
		return false
	}
	configuredHash := sha256.Sum256([]byte(configured))
	candidateHash := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(configuredHash[:], candidateHash[:]) == 1
}

func handleSavePortfolio(w http.ResponseWriter, r *http.Request, db *pgxpool.Pool, logger *log.Logger) {
	var data portfolio
	if err := readPayload(r, &data); err != nil {
		http.Error(w, "request JSON tidak valid atau gagal didekripsi: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := validatePortfolio(data); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err := savePortfolio(r, db, data); err != nil {
		logger.Printf("save portfolio: %v", err)
		http.Error(w, "gagal menyimpan portfolio", http.StatusInternalServerError)
		return
	}
	if err := writeEncryptedJSON(w, http.StatusOK, map[string]string{"message": "perubahan berhasil disimpan"}); err != nil {
		w.WriteHeader(http.StatusNoContent)
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminPayload)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request contains multiple JSON values")
		}
		return err
	}
	return nil
}

func validatePortfolio(data portfolio) error {
	if err := requireText("nama profil", data.Profile.Name); err != nil {
		return err
	}
	if err := requireText("peran profil", data.Profile.Role); err != nil {
		return err
	}
	if err := requireText("headline profil", data.Profile.Headline); err != nil {
		return err
	}
	if err := requireText("tentang profil", data.Profile.About); err != nil {
		return err
	}
	if err := requireText("lokasi profil", data.Profile.Location); err != nil {
		return err
	}
	if len(data.Experiences) > 200 || len(data.Education) > 200 || len(data.Certifications) > 200 ||
		len(data.Skills) > 300 || len(data.TechStack) > 300 || len(data.Projects) > 200 {
		return errors.New("jumlah item pada salah satu bagian melebihi batas")
	}
	for i, item := range data.Experiences {
		if err := requireText(fmt.Sprintf("pengalaman %d: perusahaan", i+1), item.Company); err != nil {
			return err
		}
		if err := requireText(fmt.Sprintf("pengalaman %d: peran", i+1), item.Role); err != nil {
			return err
		}
		if err := validDate(fmt.Sprintf("pengalaman %d: tanggal mulai", i+1), item.StartDate, true); err != nil {
			return err
		}
		if err := validDatePointer(fmt.Sprintf("pengalaman %d: tanggal selesai", i+1), item.EndDate); err != nil {
			return err
		}
		if item.EndDate != nil && *item.EndDate != "" && *item.EndDate < item.StartDate {
			return fmt.Errorf("pengalaman %d: tanggal selesai sebelum tanggal mulai", i+1)
		}
	}
	for i, item := range data.Education {
		if err := requireText(fmt.Sprintf("pendidikan %d: institusi", i+1), item.Institution); err != nil {
			return err
		}
		if err := requireText(fmt.Sprintf("pendidikan %d: gelar", i+1), item.Degree); err != nil {
			return err
		}
		if err := validDate(fmt.Sprintf("pendidikan %d: tanggal mulai", i+1), item.StartDate, true); err != nil {
			return err
		}
		if err := validDatePointer(fmt.Sprintf("pendidikan %d: tanggal selesai", i+1), item.EndDate); err != nil {
			return err
		}
		if item.EndDate != nil && *item.EndDate != "" && *item.EndDate < item.StartDate {
			return fmt.Errorf("pendidikan %d: tanggal selesai sebelum tanggal mulai", i+1)
		}
	}
	if len(data.Texts) > 120 {
		return errors.New("jumlah teks tampilan melebihi batas")
	}
	for key, value := range data.Texts {
		if len(key) > 60 || len(value) > 400 {
			return fmt.Errorf("teks tampilan %q terlalu panjang", key)
		}
	}
	for i, item := range data.Certifications {
		if err := requireText(fmt.Sprintf("sertifikasi %d: nama", i+1), item.Name); err != nil {
			return err
		}
		if err := validDatePointer(fmt.Sprintf("sertifikasi %d: tanggal terbit", i+1), item.IssuedDate); err != nil {
			return err
		}
	}
	for i, item := range data.Skills {
		if err := requireText(fmt.Sprintf("skill %d: nama", i+1), item.Name); err != nil {
			return err
		}
		if err := requireText(fmt.Sprintf("skill %d: kategori", i+1), item.Category); err != nil {
			return err
		}
		if item.Level < 1 || item.Level > 5 {
			return fmt.Errorf("skill %d: level harus antara 1 dan 5", i+1)
		}
	}
	for i, item := range data.TechStack {
		if err := requireText(fmt.Sprintf("tech stack %d: nama", i+1), item.Name); err != nil {
			return err
		}
		if err := requireText(fmt.Sprintf("tech stack %d: kategori", i+1), item.Category); err != nil {
			return err
		}
	}
	for i, item := range data.Projects {
		if err := requireText(fmt.Sprintf("project %d: judul", i+1), item.Title); err != nil {
			return err
		}
		if item.Year < 1900 || item.Year > 2200 {
			return fmt.Errorf("project %d: tahun harus antara 1900 dan 2200", i+1)
		}
	}
	return nil
}

func requireText(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s wajib diisi", field)
	}
	return nil
}

func validDate(field, value string, required bool) error {
	if value == "" && !required {
		return nil
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return fmt.Errorf("%s harus menggunakan format YYYY-MM-DD", field)
	}
	return nil
}

func validDatePointer(field string, value *string) error {
	if value == nil || *value == "" {
		return nil
	}
	return validDate(field, *value, true)
}

func savePortfolio(r *http.Request, db *pgxpool.Pool, data portfolio) error {
	if data.Texts == nil {
		data.Texts = map[string]string{}
	}
	ctx := r.Context()
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(731946208125)`); err != nil {
		return fmt.Errorf("lock portfolio update: %w", err)
	}
	for _, table := range []string{"experiences", "education", "certifications", "skills", "tech_stack", "projects"} {
		if _, err := tx.Exec(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO profile (id, name, role, headline, about, location, email, website, avatar_url, resume_url, github_url, instagram_url, twitter_url, linkedin_url, ui_texts, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, now())
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, role = EXCLUDED.role,
			headline = EXCLUDED.headline, about = EXCLUDED.about, location = EXCLUDED.location,
			email = EXCLUDED.email, website = EXCLUDED.website, avatar_url = EXCLUDED.avatar_url,
			resume_url = EXCLUDED.resume_url, github_url = EXCLUDED.github_url,
			instagram_url = EXCLUDED.instagram_url, twitter_url = EXCLUDED.twitter_url,
			linkedin_url = EXCLUDED.linkedin_url, ui_texts = EXCLUDED.ui_texts, updated_at = now()`,
		data.Profile.Name, data.Profile.Role, data.Profile.Headline, data.Profile.About,
		data.Profile.Location, data.Profile.Email, data.Profile.Website, data.Profile.AvatarURL,
		data.Profile.ResumeURL, data.Profile.GithubURL, data.Profile.InstagramURL,
		data.Profile.TwitterURL, data.Profile.LinkedinURL, data.Texts)
	if err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	for _, item := range data.Experiences {
		if _, err := tx.Exec(ctx, `INSERT INTO experiences (company, role, location, start_date, end_date, description, highlights)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			item.Company, item.Role, item.Location, item.StartDate, nullableDate(item.EndDate), item.Description, item.Highlights); err != nil {
			return fmt.Errorf("save experience %q: %w", item.Company, err)
		}
	}
	for _, item := range data.Education {
		if _, err := tx.Exec(ctx, `INSERT INTO education (institution, degree, field, start_date, end_date, description)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			item.Institution, item.Degree, item.Field, item.StartDate, nullableDate(item.EndDate), item.Description); err != nil {
			return fmt.Errorf("save education %q: %w", item.Institution, err)
		}
	}
	for i, item := range data.Certifications {
		if _, err := tx.Exec(ctx, `INSERT INTO certifications (name, issuer, issued_date, credential_url, sort_order)
			VALUES ($1, $2, $3, $4, $5)`,
			item.Name, item.Issuer, nullableDate(item.IssuedDate), item.CredentialURL, i); err != nil {
			return fmt.Errorf("save certification %q: %w", item.Name, err)
		}
	}
	for i, item := range data.Skills {
		if _, err := tx.Exec(ctx, `INSERT INTO skills (name, category, level, sort_order) VALUES ($1, $2, $3, $4)`,
			item.Name, item.Category, item.Level, i); err != nil {
			return fmt.Errorf("save skill %q: %w", item.Name, err)
		}
	}
	for i, item := range data.TechStack {
		if _, err := tx.Exec(ctx, `INSERT INTO tech_stack (name, category, sort_order) VALUES ($1, $2, $3)`,
			item.Name, item.Category, i); err != nil {
			return fmt.Errorf("save tech stack %q: %w", item.Name, err)
		}
	}
	for _, item := range data.Projects {
		projectType := strings.TrimSpace(item.ProjectType)
		if projectType == "" {
			projectType = "work"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO projects (title, category, project_type, description, year, image_url, video_url, live_url, repo_url, tech)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			item.Title, item.Category, projectType, item.Description, item.Year, item.ImageURL, item.VideoURL, item.LiveURL, item.RepoURL, item.Tech); err != nil {
			return fmt.Errorf("save project %q: %w", item.Title, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit portfolio update: %w", err)
	}
	return nil
}

func nullableDate(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode admin response: %v", err)
	}
}
