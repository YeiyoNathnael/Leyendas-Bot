package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

func Init(rawURL string) (*sql.DB, error) {
	resolvedURL, err := resolveDBURL(rawURL)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("libsql", resolvedURL)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}

func resolveDBURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("DBURL environment variable is empty")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid DBURL: %w", err)
	}

	q := parsed.Query()
	if strings.TrimSpace(q.Get("authToken")) == "" {
		token := strings.TrimSpace(os.Getenv("TURSO_AUTH_TOKEN"))
		if token == "" {
			token = strings.TrimSpace(os.Getenv("DB_AUTH_TOKEN"))
		}
		if token != "" {
			q.Set("authToken", token)
			parsed.RawQuery = q.Encode()
		}
	}

	if strings.TrimSpace(parsed.Query().Get("authToken")) == "" {
		return "", fmt.Errorf("database auth token missing: include authToken in DBURL or set TURSO_AUTH_TOKEN/DB_AUTH_TOKEN")
	}

	return parsed.String(), nil
}
