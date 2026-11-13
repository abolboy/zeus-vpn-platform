package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net/url"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/abolboy/zeus-vpn-platform/services/control-plane/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const advisoryLockID int64 = 726198430021

func main() {
	dsn := os.Getenv("ZEUS_DATABASE_URL")
	if dsn == "" {
		log.Fatal("ZEUS_DATABASE_URL is required")
	}
	if strings.EqualFold(os.Getenv("ZEUS_ENV"), "production") {
		parsed, err := url.Parse(dsn)
		if err != nil || parsed.Query().Get("sslmode") != "verify-full" {
			log.Fatal("production migrations require sslmode=verify-full")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatal("database configuration is invalid")
	}
	config.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		log.Fatal("could not initialize database connection")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		log.Fatal("database is unreachable")
	}
	if err := migrate(ctx, pool); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
	log.Print("ZEUS database migrations completed")
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer connection.Release()

	if _, err := connection.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockID); err != nil {
		return err
	}
	defer connection.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockID)

	if _, err := connection.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			checksum BYTEA NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return err
	}

	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		content, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		checksum := sha256.Sum256(content)
		var existing []byte
		err = connection.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version = $1", entry.Name()).Scan(&existing)
		if err == nil {
			if !bytes.Equal(existing, checksum[:]) {
				return fmt.Errorf("migration %s changed after it was applied", entry.Name())
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		tx, err := connection.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Conn().PgConn().Exec(ctx, string(content)).ReadAll(); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", entry.Name(), err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", entry.Name(), checksum[:]); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
