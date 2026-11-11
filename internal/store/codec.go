package store

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Lazzar19/ScoutAI/internal/domain"
)

func ensureID(id *string) {
	if *id == "" {
		*id = domain.NewID()
	}
}

// nilIfBlank stores a missing external id as NULL so the canonical-URL key can apply.
func nilIfBlank(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return value
}

func locationsJSON(locations []domain.Location) (string, error) {
	if locations == nil {
		locations = []domain.Location{}
	}
	raw, err := json.Marshal(locations)
	if err != nil {
		return "", fmt.Errorf("encode locations: %w", err)
	}
	return string(raw), nil
}

func decodeLocations(raw []byte) ([]domain.Location, error) {
	if len(raw) == 0 {
		return []domain.Location{}, nil
	}
	var locations []domain.Location
	if err := json.Unmarshal(raw, &locations); err != nil {
		return nil, fmt.Errorf("decode locations: %w", err)
	}
	if locations == nil {
		return []domain.Location{}, nil
	}
	return locations, nil
}

type profileDocument struct {
	PreferredRoles     []string          `json:"preferred_roles"`
	RoleSynonyms       []string          `json:"role_synonyms"`
	PreferredSkills    []string          `json:"preferred_skills"`
	AdditionalSkills   []string          `json:"additional_skills"`
	PreferredLocations []domain.Location `json:"preferred_locations"`
	PreferredCompanies []string          `json:"preferred_companies"`
	ExcludedCompanies  []string          `json:"excluded_companies"`
	ExcludedRoles      []string          `json:"excluded_roles"`
	ExperienceLevel    string            `json:"experience_level"`
}

func profileJSON(profile domain.CandidateProfile) (string, error) {
	raw, err := json.Marshal(profileDocument{
		PreferredRoles:     nonNil(profile.PreferredRoles),
		RoleSynonyms:       nonNil(profile.RoleSynonyms),
		PreferredSkills:    nonNil(profile.PreferredSkills),
		AdditionalSkills:   nonNil(profile.AdditionalSkills),
		PreferredLocations: nonNilLocations(profile.PreferredLocations),
		PreferredCompanies: nonNil(profile.PreferredCompanies),
		ExcludedCompanies:  nonNil(profile.ExcludedCompanies),
		ExcludedRoles:      nonNil(profile.ExcludedRoles),
		ExperienceLevel:    profile.ExperienceLevel,
	})
	if err != nil {
		return "", fmt.Errorf("encode profile: %w", err)
	}
	return string(raw), nil
}

func decodeProfile(raw []byte, profile *domain.CandidateProfile) error {
	var doc profileDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("decode profile: %w", err)
	}
	profile.PreferredRoles = nonNil(doc.PreferredRoles)
	profile.RoleSynonyms = nonNil(doc.RoleSynonyms)
	profile.PreferredSkills = nonNil(doc.PreferredSkills)
	profile.AdditionalSkills = nonNil(doc.AdditionalSkills)
	profile.PreferredLocations = nonNilLocations(doc.PreferredLocations)
	profile.PreferredCompanies = nonNil(doc.PreferredCompanies)
	profile.ExcludedCompanies = nonNil(doc.ExcludedCompanies)
	profile.ExcludedRoles = nonNil(doc.ExcludedRoles)
	profile.ExperienceLevel = doc.ExperienceLevel
	return nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilLocations(values []domain.Location) []domain.Location {
	if values == nil {
		return []domain.Location{}
	}
	return values
}

func configJSON(cfg domain.SourceConfig) (string, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("encode source config: %w", err)
	}
	return string(raw), nil
}

func decodeConfig(raw []byte) (domain.SourceConfig, error) {
	var cfg domain.SourceConfig
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return domain.SourceConfig{}, fmt.Errorf("decode source config: %w", err)
	}
	return cfg, nil
}

// jsonbArg sends JSON text. An empty slice is NULL so a preview can omit extracted facts.
func jsonbArg(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
