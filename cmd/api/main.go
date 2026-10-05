package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"portfolio/backend/migrations"
)

type portfolio struct {
	Profile        profile           `json:"profile"`
	Experiences    []experience      `json:"experiences"`
	Education      []education       `json:"education"`
	Certifications []certification   `json:"certifications"`
	Texts          map[string]string `json:"texts"`
	Skills         []skill           `json:"skills"`
	TechStack      []tech            `json:"techStack"`
	Projects       []project         `json:"projects"`
}

type profile struct {
	Name         string `json:"name"`
	Role         string `json:"role"`
	Headline     string `json:"headline"`
	About        string `json:"about"`
	Location     string `json:"location"`
	Email        string `json:"email"`
	Website      string `json:"website"`
	AvatarURL    string `json:"avatarUrl"`
	ResumeURL    string `json:"resumeUrl"`
	GithubURL    string `json:"githubUrl"`
	InstagramURL string `json:"instagramUrl"`
	TwitterURL   string `json:"twitterUrl"`
	LinkedinURL  string `json:"linkedinUrl"`
}

type experience struct {
	ID          int64    `json:"id"`
	Company     string   `json:"company"`
	Role        string   `json:"role"`
	Location    string   `json:"location"`
	StartDate   string   `json:"startDate"`
	EndDate     *string  `json:"endDate"`
	Description string   `json:"description"`
	Highlights  []string `json:"highlights"`
}

type education struct {
	ID          int64   `json:"id"`
	Institution string  `json:"institution"`
	Degree      string  `json:"degree"`
	Field       string  `json:"field"`
	StartDate   string  `json:"startDate"`
	EndDate     *string `json:"endDate"`
	Description string  `json:"description"`
}

type certification struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Issuer        string  `json:"issuer"`
	IssuedDate    *string `json:"issuedDate"`
	CredentialURL string  `json:"credentialUrl"`
}

type skill struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Level    int    `json:"level"`
}

type tech struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
}

type project struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	Year        int      `json:"year"`
	ImageURL    string   `json:"imageUrl"`
	LiveURL     string   `json:"liveUrl"`
	RepoURL     string   `json:"repoUrl"`
	Tech        []string `json:"tech"`
}

func main() {
	logger := log.New(os.Stdout, "portfolio-api ", log.LstdFlags|log.LUTC)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := connectDatabase(ctx, logger, databaseURL())
	if err != nil {
		logger.Fatalf("startup aborted: database connection failed: %v", err)
	}
	defer pool.Close()
	logger.Printf("database: connected successfully")
	if err := migrations.Apply(ctx, pool); err != nil {
		logger.Fatalf("apply database migrations: %v", err)
	}
	logger.Printf("database: migrations are up to date")

	media := connectMedia(ctx, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/portfolio", func(w http.ResponseWriter, r *http.Request) {
		data, err := loadPortfolio(r.Context(), pool)
		if err != nil {
			logger.Printf("load portfolio: %v", err)
			http.Error(w, "could not load portfolio", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(data); err != nil {
			logger.Printf("encode portfolio response: %v", err)
		}
	})
	mux.HandleFunc("POST /api/admin/auth", handleAdminAuth)
	mux.HandleFunc("PUT /api/admin/portfolio", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeAdmin(w, r) {
			return
		}
		handleSavePortfolio(w, r, pool, logger)
	})

	mux.HandleFunc("POST /api/admin/upload", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeAdmin(w, r) {
			return
		}
		if media == nil {
			http.Error(w, "MinIO belum dikonfigurasi atau tidak terhubung; cek log backend dan isi MINIO_* di .env", http.StatusServiceUnavailable)
			return
		}
		media.handleUpload(w, r, logger)
	})
	mux.HandleFunc("GET /api/media/{name...}", func(w http.ResponseWriter, r *http.Request) {
		if media == nil {
			http.NotFound(w, r)
			return
		}
		media.handleServe(w, r, logger)
	})

	server := &http.Server{
		Addr:              net.JoinHostPort(envOr("HOST", "127.0.0.1"), envOr("PORT", "8080")),
		Handler:           withHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Printf("http: listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("serve HTTP: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Printf("graceful shutdown: %v", err)
	}
}

func databaseURL() string {
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	connectionURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(envOr("POSTGRES_USER", "portfolio"), envOr("POSTGRES_PASSWORD", "portfolio")),
		Host:   net.JoinHostPort(envOr("POSTGRES_HOST", "localhost"), envOr("POSTGRES_PORT", "5432")),
		Path:   "/" + envOr("POSTGRES_DB", "portfolio"),
	}
	query := connectionURL.Query()
	query.Set("sslmode", envOr("POSTGRES_SSLMODE", "disable"))
	connectionURL.RawQuery = query.Encode()
	return connectionURL.String()
}

func connectDatabase(ctx context.Context, logger *log.Logger, connectionURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connectionURL)
	if err != nil {
		logger.Printf("database: invalid connection configuration: %v", err)
		return nil, err
	}
	config.MaxConns = 10
	config.MinConns = 1
	config.MaxConnLifetime = 30 * time.Minute

	logger.Printf("database: connecting host=%s port=%d database=%s user=%s",
		config.ConnConfig.Host, config.ConnConfig.Port, config.ConnConfig.Database, config.ConnConfig.User)
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		logger.Printf("database: connection failed: %v", err)
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		logger.Printf("database: connection failed: %v", err)
		return nil, err
	}
	return pool, nil
}

func loadPortfolio(ctx context.Context, db *pgxpool.Pool) (portfolio, error) {
	var result portfolio
	if err := db.QueryRow(ctx, `SELECT name, role, headline, about, location, email, website, avatar_url, resume_url, github_url, instagram_url, twitter_url, linkedin_url, ui_texts FROM profile WHERE id = 1`).
		Scan(&result.Profile.Name, &result.Profile.Role, &result.Profile.Headline, &result.Profile.About,
			&result.Profile.Location, &result.Profile.Email, &result.Profile.Website,
			&result.Profile.AvatarURL, &result.Profile.ResumeURL, &result.Profile.GithubURL,
			&result.Profile.InstagramURL, &result.Profile.TwitterURL, &result.Profile.LinkedinURL, &result.Texts); err != nil {
		return portfolio{}, err
	}
	result.Experiences = []experience{}
	rows, err := db.Query(ctx, `SELECT id, company, role, location, start_date::text, end_date::text, description, highlights
		FROM experiences ORDER BY start_date DESC, id DESC`)
	if err != nil {
		return portfolio{}, err
	}
	for rows.Next() {
		var item experience
		var endDate *string
		if err := rows.Scan(&item.ID, &item.Company, &item.Role, &item.Location, &item.StartDate,
			&endDate, &item.Description, &item.Highlights); err != nil {
			rows.Close()
			return portfolio{}, err
		}
		item.EndDate = endDate
		result.Experiences = append(result.Experiences, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return portfolio{}, err
	}
	rows.Close()

	result.Education = []education{}
	rows, err = db.Query(ctx, `SELECT id, institution, degree, field, start_date::text, end_date::text, description
		FROM education ORDER BY start_date DESC, id DESC`)
	if err != nil {
		return portfolio{}, err
	}
	for rows.Next() {
		var item education
		if err := rows.Scan(&item.ID, &item.Institution, &item.Degree, &item.Field,
			&item.StartDate, &item.EndDate, &item.Description); err != nil {
			rows.Close()
			return portfolio{}, err
		}
		result.Education = append(result.Education, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return portfolio{}, err
	}
	rows.Close()

	result.Certifications = []certification{}
	rows, err = db.Query(ctx, `SELECT id, name, issuer, issued_date::text, credential_url
		FROM certifications ORDER BY issued_date DESC NULLS LAST, sort_order, id`)
	if err != nil {
		return portfolio{}, err
	}
	for rows.Next() {
		var item certification
		if err := rows.Scan(&item.ID, &item.Name, &item.Issuer, &item.IssuedDate, &item.CredentialURL); err != nil {
			rows.Close()
			return portfolio{}, err
		}
		result.Certifications = append(result.Certifications, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return portfolio{}, err
	}
	rows.Close()

	result.Skills = []skill{}
	rows, err = db.Query(ctx, `SELECT id, name, category, level FROM skills ORDER BY category, sort_order, name`)
	if err != nil {
		return portfolio{}, err
	}
	for rows.Next() {
		var item skill
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.Level); err != nil {
			rows.Close()
			return portfolio{}, err
		}
		result.Skills = append(result.Skills, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return portfolio{}, err
	}
	rows.Close()

	result.TechStack = []tech{}
	rows, err = db.Query(ctx, `SELECT id, name, category FROM tech_stack ORDER BY category, sort_order, name`)
	if err != nil {
		return portfolio{}, err
	}
	for rows.Next() {
		var item tech
		if err := rows.Scan(&item.ID, &item.Name, &item.Category); err != nil {
			rows.Close()
			return portfolio{}, err
		}
		result.TechStack = append(result.TechStack, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return portfolio{}, err
	}
	rows.Close()

	result.Projects = []project{}
	rows, err = db.Query(ctx, `SELECT id, title, category, description, year, image_url, live_url, repo_url, tech
		FROM projects ORDER BY year DESC, id DESC`)
	if err != nil {
		return portfolio{}, err
	}
	for rows.Next() {
		var item project
		if err := rows.Scan(&item.ID, &item.Title, &item.Category, &item.Description,
			&item.Year, &item.ImageURL, &item.LiveURL, &item.RepoURL, &item.Tech); err != nil {
			rows.Close()
			return portfolio{}, err
		}
		result.Projects = append(result.Projects, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return portfolio{}, err
	}
	rows.Close()
	return result, nil
}

func withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
