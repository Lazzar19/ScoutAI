package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// queueTestLock is shared with the worker, pipeline, schedule, and notify tests.
// A session lock keeps a claim in one package from taking another's task.
const queueTestLock int64 = 74821102

func TestMain(m *testing.M) {
	os.Exit(runWithQueueLock(m))
}

func runWithQueueLock(m *testing.M) int {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return m.Run()
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer db.Close()
	conn, err := db.Conn(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), `SELECT pg_advisory_lock($1)`, queueTestLock); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, queueTestLock)
	return m.Run()
}
