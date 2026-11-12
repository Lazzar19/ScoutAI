package store

import (
	"context"
	"fmt"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

// InsertProfile stores one profile version.
// Preferences live in profile_data. The version column is what evaluations record.
func (p *Pool) InsertProfile(ctx context.Context, profile *domain.CandidateProfile) error {
	if profile == nil {
		return fmt.Errorf("insert profile: profile is required")
	}
	ensureID(&profile.ID)
	payload, err := profileJSON(*profile)
	if err != nil {
		return fmt.Errorf("insert profile: %w", err)
	}
	_, err = p.pool.Exec(ctx, `
		INSERT INTO candidate_profiles (id, name, profile_data, profile_version, active)
		VALUES ($1::uuid, $2, $3::jsonb, $4, $5)`,
		profile.ID,
		profile.Name,
		payload,
		profile.Version,
		profile.Active,
	)
	return mapWriteErr("insert profile", err)
}

// UpdateProfile replaces the stored preferences and version.
// The previous evaluation rows stay. A new version misses the evaluation cache key.
func (p *Pool) UpdateProfile(ctx context.Context, profile domain.CandidateProfile) error {
	payload, err := profileJSON(profile)
	if err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE candidate_profiles
		SET name = $2,
		    profile_data = $3::jsonb,
		    profile_version = $4,
		    active = $5,
		    updated_at = now()
		WHERE id = $1::uuid`,
		profile.ID,
		profile.Name,
		payload,
		profile.Version,
		profile.Active,
	)
	if err != nil {
		return mapWriteErr("update profile", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update profile: %w", ErrNotFound)
	}
	return nil
}

// GetProfile reads one profile by id.
func (p *Pool) GetProfile(ctx context.Context, id string) (domain.CandidateProfile, error) {
	var profile domain.CandidateProfile
	var payload []byte
	err := p.pool.QueryRow(ctx, `
		SELECT id, name, profile_data, profile_version, active
		FROM candidate_profiles
		WHERE id = $1::uuid`, id).Scan(
		&profile.ID,
		&profile.Name,
		&payload,
		&profile.Version,
		&profile.Active,
	)
	if err != nil {
		return domain.CandidateProfile{}, mapReadErr("get profile", err)
	}
	if err := decodeProfile(payload, &profile); err != nil {
		return domain.CandidateProfile{}, fmt.Errorf("get profile: %w", err)
	}
	return profile, nil
}

// GetActiveProfile returns the profile used for new match tasks.
func (p *Pool) GetActiveProfile(ctx context.Context) (domain.CandidateProfile, error) {
	var profile domain.CandidateProfile
	var payload []byte
	err := p.pool.QueryRow(ctx, `
		SELECT id, name, profile_data, profile_version, active
		FROM candidate_profiles
		WHERE active
		ORDER BY updated_at DESC
		LIMIT 1`).Scan(
		&profile.ID,
		&profile.Name,
		&payload,
		&profile.Version,
		&profile.Active,
	)
	if err != nil {
		return domain.CandidateProfile{}, mapReadErr("get active profile", err)
	}
	if err := decodeProfile(payload, &profile); err != nil {
		return domain.CandidateProfile{}, fmt.Errorf("get active profile: %w", err)
	}
	return profile, nil
}

// GetProfileByVersion reads the profile a match task named.
// An in-place edit bumps the version, so the previous version is no longer this row.
func (p *Pool) GetProfileByVersion(ctx context.Context, version int) (domain.CandidateProfile, error) {
	if version < 1 {
		return domain.CandidateProfile{}, fmt.Errorf("get profile version: version must be positive")
	}
	var profile domain.CandidateProfile
	var payload []byte
	err := p.pool.QueryRow(ctx, `
		SELECT id, name, profile_data, profile_version, active
		FROM candidate_profiles
		WHERE profile_version = $1
		ORDER BY active DESC, updated_at DESC
		LIMIT 1`, version).Scan(
		&profile.ID,
		&profile.Name,
		&payload,
		&profile.Version,
		&profile.Active,
	)
	if err != nil {
		return domain.CandidateProfile{}, mapReadErr("get profile version", err)
	}
	if err := decodeProfile(payload, &profile); err != nil {
		return domain.CandidateProfile{}, fmt.Errorf("get profile version: %w", err)
	}
	return profile, nil
}
