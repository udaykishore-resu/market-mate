package storage

import (
	"strings"
	"testing"
)

// TestLoadMigrations runs without a database: an embed that silently ships no
// files would otherwise only surface as an empty schema at boot.
func TestLoadMigrations(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations were embedded")
	}

	var previous string
	for i, m := range migrations {
		if !strings.HasSuffix(m.name, ".sql") {
			t.Errorf("migration %d is not a .sql file: %q", i, m.name)
		}
		if strings.TrimSpace(m.sql) == "" {
			t.Errorf("migration %q is empty", m.name)
		}
		if m.name <= previous {
			t.Errorf("migration %q is out of order after %q; they are applied in this order", m.name, previous)
		}
		previous = m.name
	}

	if first := migrations[0].name; first != "0001_init.sql" {
		t.Errorf("first migration = %q, want 0001_init.sql", first)
	}
}

// TestFirstMigrationDefinesTheSchema is a cheap guard against an edit that
// removes one of the objects the store queries by name.
func TestFirstMigrationDefinesTheSchema(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	sql := migrations[0].sql

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS videos",
		"CREATE TABLE IF NOT EXISTS extractions",
		"CREATE OR REPLACE VIEW recipes",
		"PRIMARY KEY (video_id, model_version)",
		"REFERENCES videos",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0001_init.sql no longer contains %q", want)
		}
	}
}
