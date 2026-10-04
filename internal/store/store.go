// Package store provides a concurrency-safe in-memory repository for run
// batches and case results. Batch ingestion is atomic: readers never observe
// a partially written batch.
package store

import (
	"fmt"
	"sort"
	"sync"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/model"
)

// Snapshot is a consistent, read-only view of the store at a point in time.
// Batches are ordered by (Index, StartedAt, ID) so statistics never depend on
// arrival order.
type Snapshot struct {
	Batches []model.Batch
}

// Find returns the batches with the given IDs, in request order. Missing IDs
// are reported in the second return value.
func (s Snapshot) Find(ids []string) ([]model.Batch, []string) {
	byID := make(map[string]model.Batch, len(s.Batches))
	for _, b := range s.Batches {
		byID[b.ID] = b
	}
	found := make([]model.Batch, 0, len(ids))
	var missing []string
	for _, id := range ids {
		if b, ok := byID[id]; ok {
			found = append(found, b)
		} else {
			missing = append(missing, id)
		}
	}
	return found, missing
}

type batchEntry struct {
	meta    model.Batch
	results map[string]model.CaseResult
}

// Store keeps batches and their results, deduplicated by (batch, test) and
// ordered by the reporter-provided run index.
type Store struct {
	mu      sync.RWMutex
	batches map[string]*batchEntry
}

// New creates an empty store.
func New() *Store {
	return &Store{
		batches: make(map[string]*batchEntry),
	}
}

// Ingest validates and atomically stores a batch. Re-ingesting an identical
// batch is a no-op (idempotent); re-ingesting a batch whose results conflict
// with what is already stored is rejected with ReasonConflictingEntry.
func (s *Store) Ingest(b model.Batch) error {
	if err := b.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.batches[b.ID]; ok {
		// Duplicate batch: identical entries are ignored, conflicts rejected.
		for _, r := range b.Results {
			prev, seen := existing.results[r.TestID]
			if seen && prev.Status != r.Status {
				return &model.RejectError{
					Reason:  model.ReasonConflictingEntry,
					Message: fmt.Sprintf("batch %q test %q already recorded as %q, cannot re-report as %q", b.ID, r.TestID, prev.Status, r.Status),
				}
			}
		}
		for _, r := range b.Results {
			existing.results[r.TestID] = r
		}
		return nil
	}

	entry := &batchEntry{meta: b, results: make(map[string]model.CaseResult, len(b.Results))}
	for _, r := range b.Results {
		if prev, seen := entry.results[r.TestID]; seen && prev.Status != r.Status {
			return &model.RejectError{
				Reason:  model.ReasonConflictingEntry,
				Message: fmt.Sprintf("batch %q reports test %q twice with different statuses", b.ID, r.TestID),
			}
		}
		entry.results[r.TestID] = r
	}
	s.batches[b.ID] = entry
	return nil
}

// Snapshot returns a consistent view of all retained batches, ordered by
// (Index, StartedAt, ID).
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Batch, 0, len(s.batches))
	for _, e := range s.batches {
		b := e.meta
		b.Results = make([]model.CaseResult, 0, len(e.results))
		for _, r := range e.results {
			b.Results = append(b.Results, r)
		}
		sort.Slice(b.Results, func(i, j int) bool { return b.Results[i].TestID < b.Results[j].TestID })
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Index != out[j].Index {
			return out[i].Index < out[j].Index
		}
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.Before(out[j].StartedAt)
		}
		return out[i].ID < out[j].ID
	})
	return Snapshot{Batches: out}
}

// Prune removes all but the most recent keepLast batches (by run index) and
// returns the number of removed batches. keepLast <= 0 removes nothing.
func (s *Store) Prune(keepLast int) int {
	if keepLast <= 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.batches) <= keepLast {
		return 0
	}
	ids := make([]string, 0, len(s.batches))
	for id := range s.batches {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := s.batches[ids[i]].meta, s.batches[ids[j]].meta
		if a.Index != b.Index {
			return a.Index < b.Index
		}
		if !a.StartedAt.Equal(b.StartedAt) {
			return a.StartedAt.Before(b.StartedAt)
		}
		return a.ID < b.ID
	})
	remove := len(ids) - keepLast
	for _, id := range ids[:remove] {
		delete(s.batches, id)
	}
	return remove
}

// Len returns the number of retained batches.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.batches)
}
