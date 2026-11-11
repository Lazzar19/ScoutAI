package store

import (
	"context"
	"fmt"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// InsertSource stores one source. The name is unique.
// Config is non-secret settings only.
func (p *Pool) InsertSource(ctx context.Context, source *domain.JobSource) error {
	if source == nil {
		return fmt.Errorf("insert source: source is required")
	}
	ensureID(&source.ID)
	cfg, err := configJSON(source.Config)
	if err != nil {
		return fmt.Errorf("insert source: %w", err)
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO sources (
			id, name, type, enabled, configuration, cursor, last_checked_at, last_success_at
		) VALUES (
			$1::uuid, $2, $3, $4, $5::jsonb, $6, $7, $8
		)`,
		source.ID,
		source.Name,
		string(source.Type),
		source.Enabled,
		cfg,
		source.Cursor,
		source.LastChecked,
		source.LastSuccess,
	)
	return mapWriteErr("insert source", err)
}

// GetSource reads one source by id.
func (p *Pool) GetSource(ctx context.Context, id string) (domain.JobSource, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, name, type, enabled, configuration, cursor, last_checked_at, last_success_at
		FROM sources
		WHERE id = $1::uuid`, id)
	source, err := scanSource(row)
	if err != nil {
		return domain.JobSource{}, mapReadErr("get source", err)
	}
	return source, nil
}

func scanSource(row rowScanner) (domain.JobSource, error) {
	var source domain.JobSource
	var sourceType string
	var cfg []byte
	err := row.Scan(
		&source.ID,
		&source.Name,
		&sourceType,
		&source.Enabled,
		&cfg,
		&source.Cursor,
		&source.LastChecked,
		&source.LastSuccess,
	)
	if err != nil {
		return domain.JobSource{}, err
	}
	source.Type = domain.SourceType(sourceType)
	decoded, err := decodeConfig(cfg)
	if err != nil {
		return domain.JobSource{}, err
	}
	source.Config = decoded
	source.LastChecked = utcPtr(source.LastChecked)
	source.LastSuccess = utcPtr(source.LastSuccess)
	return source, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func utcPtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
