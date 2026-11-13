// Package postgres opens a single service's database. Transactions are explicit.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

func Open(ctx context.Context, dsn string, migrations ...string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(5 * time.Minute)
	setup, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err = db.PingContext(setup); err == nil {
		err = migrate(setup, db, migrations)
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("database setup: %w", err)
	}
	return db, nil
}

func migrate(ctx context.Context, db *sql.DB, migrations []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialise startup of replicas within this service database only.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(31001)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version int PRIMARY KEY)`); err != nil {
		return err
	}
	for i, migration := range migrations {
		version := i + 1
		var applied bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&applied); err != nil {
			return err
		}
		if !applied {
			if _, err = tx.ExecContext(ctx, migration); err != nil {
				return fmt.Errorf("migration %d: %w", version, err)
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations VALUES($1)`, version); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
