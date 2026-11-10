// Package store opens PostgreSQL and applies embedded Goose migrations.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Pool is the process database pool.
type Pool struct {
	pool *pgxpool.Pool
}

// Open connects to PostgreSQL and checks that the server answers.
func Open(ctx context.Context, databaseURL string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 4

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Pool{pool: pool}, nil
}

// Ping reports whether PostgreSQL is reachable.
func (p *Pool) Ping(ctx context.Context) error {
	if err := p.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// Close releases the pool.
func (p *Pool) Close() {
	p.pool.Close()
}

// Migrate applies embedded migrations once. Goose takes a Postgres session advisory lock
// so a second process cannot apply the same version at the same time.
func Migrate(ctx context.Context, databaseURL string, logger *slog.Logger) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open database for migrations: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database for migrations: %w", err)
	}

	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("create migration lock: %w", err)
	}
	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations,
		goose.WithSessionLocker(locker),
		goose.WithSlog(logger),
	)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
