//go:build saga_integration

// Package testdb isolates each integration test from live service relays.
package testdb

import (
	"context"
	"database/sql"
	"net/url"
	"testing"

	"example.com/strangler/internal/contract"
	"example.com/strangler/internal/postgres"
)

func Open(t *testing.T, dsn string, migrations ...string) *sql.DB {
	t.Helper()
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := contract.ID("test_")
	if _, err = admin.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	address, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := address.Query()
	query.Set("search_path", schema)
	address.RawQuery = query.Encode()
	db, err := postgres.Open(context.Background(), address.String(), migrations...)
	if err != nil {
		admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`)
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	return db
}
