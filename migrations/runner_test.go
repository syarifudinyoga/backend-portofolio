package migrations

import (
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		version int64
		wantErr bool
	}{
		{name: "001_initial.sql", version: 1},
		{name: "002_add_profile_bio.sql", version: 2},
		{name: "01_short.sql", wantErr: true},
		{name: "abc_initial.sql", wantErr: true},
		{name: "001_.sql", wantErr: true},
		{name: "000_initial.sql", wantErr: true},
		{name: "001_initial.txt", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseVersion(test.name)
			if test.wantErr {
				if err == nil {
					t.Fatalf("parseVersion(%q) returned no error", test.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseVersion(%q): %v", test.name, err)
			}
			if got != test.version {
				t.Errorf("parseVersion(%q) = %d, want %d", test.name, got, test.version)
			}
		})
	}
}

func TestLoadMigrations(t *testing.T) {
	got, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations(): %v", err)
	}
	if len(got) < 1 || got[len(got)-1].version != int64(len(got)) {
		t.Fatalf("loadMigrations() returned %d migrations, want contiguous versions starting at 1", len(got))
	}
	if got[0].version != 1 || got[0].name != "001_initial.sql" {
		t.Errorf("first migration = %03d %q, want 001_initial.sql", got[0].version, got[0].name)
	}
	if got[0].checksum == "" || !strings.Contains(got[0].sql, "CREATE TABLE IF NOT EXISTS profile") {
		t.Error("initial migration was not embedded with a checksum")
	}
}
