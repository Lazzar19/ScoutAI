package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Lazzar19/ScoutAI/internal/domain"
	"github.com/Lazzar19/ScoutAI/internal/source"
	"github.com/Lazzar19/ScoutAI/internal/store"
)

// Store is the persistence used while ingesting one snapshot.
type Store interface {
	FindReferenceByExternalID(ctx context.Context, sourceID, externalID string) (domain.SourceReference, error)
	FindReferenceByCanonicalURL(ctx context.Context, sourceID, canonicalURL string) (domain.SourceReference, error)
	InsertJobWithReference(ctx context.Context, job *domain.Job, ref *domain.SourceReference) error
	UpdateJobSnapshot(ctx context.Context, job domain.Job, ref domain.SourceReference) error
	CloseMissingJobs(ctx context.Context, sourceID string, seenExternalIDs []string) (int, error)
	GetActiveProfile(ctx context.Context) (domain.CandidateProfile, error)
	EnqueueMatch(ctx context.Context, jobID, contentHash string, profileVersion int) error
}

// Result is what one snapshot changed.
type Result struct {
	Stored     int
	Closed     int
	ItemErrors []source.ItemError
}

// Apply validates, deduplicates, and stores a snapshot.
// Missing jobs are closed only when the snapshot is complete and every posting was readable.
func Apply(ctx context.Context, db Store, src domain.JobSource, snapshot source.Snapshot, now time.Time) (Result, error) {
	result := Result{ItemErrors: append([]source.ItemError(nil), snapshot.ItemErrors...)}
	seen := make([]string, 0, len(snapshot.Postings))
	for _, posting := range snapshot.Postings {
		job, err := normalize(posting)
		if err != nil {
			result.ItemErrors = append(result.ItemErrors, source.ItemError{
				ExternalID: externalID(posting),
				Message:    err.Error(),
			})
			continue
		}
		ref, found, err := existingReference(ctx, db, src.ID, posting, job.URL)
		if err != nil {
			return result, err
		}
		if found {
			job.ID = ref.JobID
			ref.OriginalURL = strings.TrimSpace(posting.OriginalURL)
			ref.CanonicalURL = job.URL
			if err := db.UpdateJobSnapshot(ctx, job, ref); err != nil {
				return result, err
			}
		} else {
			job.DiscoveredAt = now.UTC()
			ref = domain.SourceReference{
				SourceID:     src.ID,
				ExternalID:   trimmedExternalID(posting.ExternalID),
				OriginalURL:  strings.TrimSpace(posting.OriginalURL),
				CanonicalURL: job.URL,
				DiscoveredAt: job.DiscoveredAt,
			}
			if err := db.InsertJobWithReference(ctx, &job, &ref); err != nil {
				return result, err
			}
		}
		result.Stored++
		if posting.ExternalID != nil && strings.TrimSpace(*posting.ExternalID) != "" {
			seen = append(seen, strings.TrimSpace(*posting.ExternalID))
		}
		if err := enqueue(ctx, db, job); err != nil {
			return result, err
		}
	}
	if snapshot.Complete && len(result.ItemErrors) == 0 {
		closed, err := db.CloseMissingJobs(ctx, src.ID, seen)
		if err != nil {
			return result, err
		}
		result.Closed = closed
	}
	return result, nil
}

func existingReference(ctx context.Context, db Store, sourceID string, posting source.DiscoveredPosting, canonical string) (domain.SourceReference, bool, error) {
	if id := externalID(posting); id != "" {
		ref, err := db.FindReferenceByExternalID(ctx, sourceID, id)
		if errors.Is(err, store.ErrNotFound) {
			return domain.SourceReference{}, false, nil
		}
		if err != nil {
			return domain.SourceReference{}, false, err
		}
		return ref, true, nil
	}
	canonical = strings.TrimSpace(canonical)
	if canonical == "" {
		return domain.SourceReference{}, false, nil
	}
	ref, err := db.FindReferenceByCanonicalURL(ctx, sourceID, canonical)
	if errors.Is(err, store.ErrNotFound) {
		return domain.SourceReference{}, false, nil
	}
	if err != nil {
		return domain.SourceReference{}, false, err
	}
	return ref, true, nil
}

func trimmedExternalID(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func enqueue(ctx context.Context, db Store, job domain.Job) error {
	profile, err := db.GetActiveProfile(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := db.EnqueueMatch(ctx, job.ID, job.ContentHash, profile.Version); err != nil {
		return fmt.Errorf("enqueue match: %w", err)
	}
	return nil
}

func externalID(posting source.DiscoveredPosting) string {
	if posting.ExternalID == nil {
		return ""
	}
	return strings.TrimSpace(*posting.ExternalID)
}
