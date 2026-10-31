package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	_ "modernc.org/sqlite"
)

type Connection struct {
	ID        string                 `json:"id"`
	Provider  string                 `json:"provider"`
	AuthType  string                 `json:"authType"`
	Name      *string                `json:"name"`
	Email     *string                `json:"email"`
	Priority  *int                   `json:"priority"`
	IsActive  bool                   `json:"isActive"`
	Data      map[string]interface{} `json:"data,omitempty"`
	CreatedAt string                 `json:"createdAt"`
	UpdatedAt string                 `json:"updatedAt"`
}

type Combo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Kind      *string  `json:"kind,omitempty"`
	Models    []string `json:"models"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

type CustomModel struct {
	ID            string `json:"id"`
	ProviderAlias string `json:"providerAlias"`
	Type          string `json:"type,omitempty"`
}

type Settings struct {
	RequireApiKey bool                   `json:"requireApiKey"`
	RequireLogin  bool                   `json:"requireLogin"`
	Raw           map[string]interface{} `json:"-"`
}

var (
	dbInstance *sql.DB
	dbOnce     sync.Once
	dbErr      error
)

func resolveDatabasePath() (string, error) {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir != "" {
		candidates := []string{
			filepath.Join(dataDir, "db", "data.sqlite"),
			filepath.Join(dataDir, "9router.db"),
			filepath.Join(dataDir, "data.sqlite"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c, nil
			}
		}
		return filepath.Join(dataDir, "db", "data.sqlite"), nil
	}

	var baseDir string
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData != "" {
			candidates := []string{
				filepath.Join(appData, "9router", "db", "data.sqlite"),
				filepath.Join(appData, "9router", "9router.db"),
			}
			for _, c := range candidates {
				if _, err := os.Stat(c); err == nil {
					return c, nil
				}
			}
			baseDir = filepath.Join(appData, "9router")
		} else {
			baseDir = filepath.Join(home, "AppData", "Roaming", "9router")
		}
	} else {
		baseDir = filepath.Join(home, ".9router")
	}

	candidates := []string{
		filepath.Join(baseDir, "db", "data.sqlite"),
		filepath.Join(baseDir, "9router.db"),
		filepath.Join(home, ".9router", "db", "data.sqlite"),
		filepath.Join(home, ".9router", "9router.db"),
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	return filepath.Join(baseDir, "db", "data.sqlite"), nil
}

func GetDB() (*sql.DB, error) {
	dbOnce.Do(func() {
		path, err := resolveDatabasePath()
		if err != nil {
			dbErr = err
			return
		}

		db, err := sql.Open("sqlite", path)
		if err != nil {
			dbErr = fmt.Errorf("failed to open sqlite database at %s: %w", path, err)
			return
		}

		pragmas := []string{
			"PRAGMA journal_mode = WAL;",
			"PRAGMA synchronous = NORMAL;",
			"PRAGMA busy_timeout = 5000;",
		}
		for _, p := range pragmas {
			if _, err := db.Exec(p); err != nil {
				dbErr = fmt.Errorf("failed to set pragma %s: %w", p, err)
				return
			}
		}

		dbInstance = db
	})

	return dbInstance, dbErr
}

func ValidateApiKey(key string) (bool, error) {
	if key == "" {
		return false, nil
	}

	db, err := GetDB()
	if err != nil {
		return false, err
	}

	var isActive int
	query := "SELECT isActive FROM apiKeys WHERE key = ?"
	err = db.QueryRow(query, key).Scan(&isActive)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("query error validating api key: %w", err)
	}

	return isActive == 1, nil
}

func GetProviderConnections() ([]Connection, error) {
	db, err := GetDB()
	if err != nil {
		return nil, err
	}

	query := `SELECT id, provider, authType, name, email, priority, isActive, data, createdAt, updatedAt FROM providerConnections`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query error getting provider connections: %w", err)
	}
	defer rows.Close()

	var connections []Connection
	for rows.Next() {
		var (
			conn     Connection
			isActive int
			dataRaw  string
		)

		err := rows.Scan(
			&conn.ID,
			&conn.Provider,
			&conn.AuthType,
			&conn.Name,
			&conn.Email,
			&conn.Priority,
			&isActive,
			&dataRaw,
			&conn.CreatedAt,
			&conn.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan error reading provider connection: %w", err)
		}

		conn.IsActive = isActive == 1
		if dataRaw != "" {
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(dataRaw), &parsed); err == nil {
				conn.Data = parsed
			}
		}

		connections = append(connections, conn)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error reading provider connections: %w", err)
	}

	return connections, nil
}

func GetProviderConnectionByID(id string) (*Connection, error) {
	db, err := GetDB()
	if err != nil {
		return nil, err
	}

	query := `SELECT id, provider, authType, name, email, priority, isActive, data, createdAt, updatedAt FROM providerConnections WHERE id = ? OR name = ? LIMIT 1`
	var (
		conn     Connection
		isActive int
		dataRaw  string
	)
	err = db.QueryRow(query, id, id).Scan(
		&conn.ID,
		&conn.Provider,
		&conn.AuthType,
		&conn.Name,
		&conn.Email,
		&conn.Priority,
		&isActive,
		&dataRaw,
		&conn.CreatedAt,
		&conn.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query error reading provider connection %s: %w", id, err)
	}

	conn.IsActive = isActive == 1
	if dataRaw != "" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(dataRaw), &parsed); err == nil {
			conn.Data = parsed
		}
	}

	return &conn, nil
}

func GetCombos() ([]Combo, error) {
	db, err := GetDB()
	if err != nil {
		return nil, err
	}

	query := `SELECT id, name, kind, models, createdAt, updatedAt FROM combos`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query error getting combos: %w", err)
	}
	defer rows.Close()

	var combos []Combo
	for rows.Next() {
		var (
			c         Combo
			modelsRaw string
		)

		err := rows.Scan(
			&c.ID,
			&c.Name,
			&c.Kind,
			&modelsRaw,
			&c.CreatedAt,
			&c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan error reading combo: %w", err)
		}

		if modelsRaw != "" {
			var parsedModels []string
			if err := json.Unmarshal([]byte(modelsRaw), &parsedModels); err == nil {
				c.Models = parsedModels
			}
		}

		combos = append(combos, c)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error reading combos: %w", err)
	}

	return combos, nil
}

func GetSettings() (*Settings, error) {
	db, err := GetDB()
	if err != nil {
		return nil, err
	}

	var dataRaw string
	query := "SELECT data FROM settings WHERE id = 1"
	err = db.QueryRow(query).Scan(&dataRaw)
	if err != nil {
		if err == sql.ErrNoRows {
			return &Settings{
				RequireApiKey: true,
				RequireLogin:  true,
			}, nil
		}
		return nil, fmt.Errorf("query error reading settings: %w", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(dataRaw), &parsed); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings data: %w", err)
	}

	settings := &Settings{
		RequireApiKey: true,
		RequireLogin:  true,
		Raw:           parsed,
	}

	if val, ok := parsed["requireApiKey"].(bool); ok {
		settings.RequireApiKey = val
	}
	if val, ok := parsed["requireLogin"].(bool); ok {
		settings.RequireLogin = val
	}

	return settings, nil
}
