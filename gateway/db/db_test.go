package db_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"9router-gateway/db"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) string {
	dir, err := os.MkdirTemp("", "9router-db-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbDir := filepath.Join(dir, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatalf("failed to create db dir: %v", err)
	}

	dbPath := filepath.Join(dbDir, "data.sqlite")
	sqliteDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite test db: %v", err)
	}
	defer sqliteDB.Close()

	schema := `
	CREATE TABLE apiKeys (
		id TEXT PRIMARY KEY,
		key TEXT NOT NULL,
		name TEXT,
		isActive INTEGER DEFAULT 1
	);
	CREATE TABLE settings (
		id INTEGER PRIMARY KEY,
		data TEXT NOT NULL
	);
	CREATE TABLE providerConnections (
		id TEXT PRIMARY KEY,
		provider TEXT NOT NULL,
		authType TEXT NOT NULL,
		name TEXT,
		email TEXT,
		priority INTEGER DEFAULT 1,
		isActive INTEGER DEFAULT 1,
		data TEXT,
		createdAt TEXT,
		updatedAt TEXT
	);
	CREATE TABLE combos (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		kind TEXT,
		models TEXT,
		createdAt TEXT,
		updatedAt TEXT
	);
	`
	if _, err := sqliteDB.Exec(schema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	_, err = sqliteDB.Exec("INSERT INTO apiKeys (id, key, name, isActive) VALUES ('1', 'sk-valid-key', 'Test', 1)")
	if err != nil {
		t.Fatalf("failed to insert test api key: %v", err)
	}

	_, err = sqliteDB.Exec("INSERT INTO settings (id, data) VALUES (1, '{\"requireApiKey\": false, \"requireLogin\": true}')")
	if err != nil {
		t.Fatalf("failed to insert test settings: %v", err)
	}

	_, err = sqliteDB.Exec("INSERT INTO providerConnections (id, provider, authType, name, isActive, data, createdAt, updatedAt) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		"conn-uuid-123", "claude", "oauth", "TestConn", 1, `{"apiKey":"sk-test"}`, "2026-10-31", "2026-10-31")
	if err != nil {
		t.Fatalf("failed to insert test provider connection: %v", err)
	}

	return dir
}

func TestDatabaseOperations(t *testing.T) {
	tempDir := setupTestDB(t)
	defer os.RemoveAll(tempDir)

	os.Setenv("DATA_DIR", tempDir)
	defer os.Unsetenv("DATA_DIR")

	conn, err := db.GetProviderConnectionByID("conn-uuid-123")
	if err != nil {
		t.Fatalf("GetProviderConnectionByID failed: %v", err)
	}
	if conn == nil || conn.ID != "conn-uuid-123" {
		t.Fatalf("expected conn-uuid-123, got %v", conn)
	}

	connByName, err := db.GetProviderConnectionByID("TestConn")
	if err != nil {
		t.Fatalf("GetProviderConnectionByID by name failed: %v", err)
	}
	if connByName == nil || connByName.ID != "conn-uuid-123" {
		t.Fatalf("expected conn-uuid-123 by name, got %v", connByName)
	}

	connMissing, err := db.GetProviderConnectionByID("non-existent-uuid")
	if err != nil {
		t.Fatalf("GetProviderConnectionByID missing returned err: %v", err)
	}
	if connMissing != nil {
		t.Fatalf("expected nil for missing connection, got %v", connMissing)
	}

	valid, err := db.ValidateApiKey("sk-valid-key")
	if err != nil {
		t.Fatalf("ValidateApiKey returned error: %v", err)
	}
	if !valid {
		t.Fatalf("expected valid key to return true")
	}

	invalid, err := db.ValidateApiKey("sk-non-existent")
	if err != nil {
		t.Fatalf("ValidateApiKey returned error on invalid key: %v", err)
	}
	if invalid {
		t.Fatalf("expected invalid key to return false")
	}

	settings, err := db.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings returned error: %v", err)
	}
	if settings.RequireApiKey {
		t.Fatalf("expected requireApiKey to be false")
	}
}
