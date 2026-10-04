package main

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDatabaseURLUsesEncodedCredentials(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("POSTGRES_USER", "portfolio user")
	t.Setenv("POSTGRES_PASSWORD", "p@ss:/word?&")
	t.Setenv("POSTGRES_HOST", "db.internal")
	t.Setenv("POSTGRES_PORT", "5433")
	t.Setenv("POSTGRES_DB", "portfolio data")
	t.Setenv("POSTGRES_SSLMODE", "require")

	config, err := pgxpool.ParseConfig(databaseURL())
	if err != nil {
		t.Fatalf("parse generated database URL: %v", err)
	}
	if config.ConnConfig.User != "portfolio user" {
		t.Errorf("user = %q, want %q", config.ConnConfig.User, "portfolio user")
	}
	if config.ConnConfig.Password != "p@ss:/word?&" {
		t.Errorf("password = %q, want credentials to survive URL encoding", config.ConnConfig.Password)
	}
	if config.ConnConfig.Host != "db.internal" || config.ConnConfig.Port != 5433 {
		t.Errorf("address = %s:%d, want db.internal:5433", config.ConnConfig.Host, config.ConnConfig.Port)
	}
	if config.ConnConfig.Database != "portfolio data" {
		t.Errorf("database = %q, want %q", config.ConnConfig.Database, "portfolio data")
	}
	if config.ConnConfig.TLSConfig == nil {
		t.Fatal("TLSConfig is nil, want TLS enabled for sslmode=require")
	}
}
