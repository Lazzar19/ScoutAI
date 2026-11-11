package store

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrConflict means a unique key rejected the write.
var ErrConflict = errors.New("unique constraint violated")

// ErrNotFound means the requested row does not exist.
var ErrNotFound = errors.New("row not found")

func mapWriteErr(op string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%s: %w (%s)", op, ErrConflict, pgErr.ConstraintName)
	}
	return fmt.Errorf("%s: %w", op, err)
}

func mapReadErr(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", op, ErrNotFound)
	}
	return fmt.Errorf("%s: %w", op, err)
}
