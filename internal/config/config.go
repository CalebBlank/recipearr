// Package config holds process-level configuration read from the environment.
// Runtime settings (Tandoor URL/token, enrichment options, taxonomy) live in the
// settings table instead, so they're editable from the web UI.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	DataDir  string // directory for the SQLite db (and anything else stateful)
	BindAddr string // host:port the web server listens on
	DBPath   string // computed: <DataDir>/recipearr.db

	// Optional one-time seeds: if set on first run, they populate the settings table.
	SeedTandoorURL   string
	SeedTandoorToken string
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// Load reads configuration from the environment, applying sensible defaults.
func Load() Config {
	dataDir := env("RECIPEARR_DATA_DIR", "data")
	c := Config{
		DataDir:          dataDir,
		BindAddr:         env("RECIPEARR_ADDR", "0.0.0.0:8585"),
		DBPath:           filepath.Join(dataDir, "recipearr.db"),
		SeedTandoorURL:   env("TANDOOR_URL", ""),
		SeedTandoorToken: env("TANDOOR_TOKEN", ""),
	}
	return c
}
