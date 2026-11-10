package store

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestMigrateTwiceRecordsOneVersion(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	logger := slog.New(slog.DiscardHandler)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Migrate(ctx, databaseURL, logger)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Migrate() error = %v", err)
		}
	}

	if err := Migrate(ctx, databaseURL, logger); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM goose_db_version WHERE version_id = 1 AND is_applied`).Scan(&count); err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if count != 1 {
		t.Fatalf("applied version 1 rows = %d, want 1", count)
	}
}
