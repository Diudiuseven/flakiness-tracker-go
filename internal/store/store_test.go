package store

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/model"
)

func batch(id string, idx int64, results ...model.CaseResult) model.Batch {
	return model.Batch{
		ID:        id,
		Source:    "ci",
		Index:     idx,
		StartedAt: time.Unix(1000+idx, 0),
		Results:   results,
	}
}

func TestIngestRejectsMalformed(t *testing.T) {
	s := New()
	cases := []struct {
		name   string
		b      model.Batch
		reason model.RejectReason
	}{
		{"missing batch id", model.Batch{Source: "ci"}, model.ReasonMissingBatchID},
		{"missing source", model.Batch{ID: "b1"}, model.ReasonMissingSource},
		{"negative index", batch("b1", -1), model.ReasonNegativeIndex},
		{"missing test id", batch("b1", 0, model.CaseResult{Status: model.StatusPass}), model.ReasonMissingTestID},
		{"invalid status", batch("b1", 0, model.CaseResult{TestID: "t1", Status: "boom"}), model.ReasonInvalidStatus},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.Ingest(tc.b)
			if err == nil {
				t.Fatal("expected rejection")
			}
			reason, ok := model.IsReject(err)
			if !ok {
				t.Fatalf("expected RejectError, got %T", err)
			}
			if reason != tc.reason {
				t.Fatalf("reason = %q, want %q", reason, tc.reason)
			}
		})
	}
	if s.Len() != 0 {
		t.Fatalf("rejected batches must not be stored, len = %d", s.Len())
	}
}

func TestIngestIdempotentDuplicate(t *testing.T) {
	s := New()
	b := batch("b1", 1, model.CaseResult{TestID: "t1", Status: model.StatusPass})
	for i := 0; i < 3; i++ {
		if err := s.Ingest(b); err != nil {
			t.Fatalf("duplicate ingest %d: %v", i, err)
		}
	}
	snap := s.Snapshot()
	if len(snap.Batches) != 1 || len(snap.Batches[0].Results) != 1 {
		t.Fatalf("duplicate changed state: %+v", snap)
	}
}

func TestIngestConflictingDuplicateRejected(t *testing.T) {
	s := New()
	if err := s.Ingest(batch("b1", 1, model.CaseResult{TestID: "t1", Status: model.StatusPass})); err != nil {
		t.Fatal(err)
	}
	err := s.Ingest(batch("b1", 1, model.CaseResult{TestID: "t1", Status: model.StatusFail}))
	reason, ok := model.IsReject(err)
	if !ok || reason != model.ReasonConflictingEntry {
		t.Fatalf("expected conflicting_duplicate, got %v", err)
	}
	// Original result must be unchanged.
	snap := s.Snapshot()
	if snap.Batches[0].Results[0].Status != model.StatusPass {
		t.Fatal("conflicting re-report mutated stored result")
	}
}

func TestOutOfOrderArrivalSameStatistics(t *testing.T) {
	mk := func() []model.Batch {
		return []model.Batch{
			batch("b1", 1, model.CaseResult{TestID: "t1", Status: model.StatusPass}),
			batch("b2", 2, model.CaseResult{TestID: "t1", Status: model.StatusFail}),
			batch("b3", 3, model.CaseResult{TestID: "t1", Status: model.StatusPass}),
		}
	}
	inOrder := New()
	for _, b := range mk() {
		inOrder.Ingest(b)
	}
	reversed := New()
	bs := mk()
	for i := len(bs) - 1; i >= 0; i-- {
		reversed.Ingest(bs[i])
	}
	a, b := inOrder.Snapshot(), reversed.Snapshot()
	if len(a.Batches) != len(b.Batches) {
		t.Fatal("batch count differs")
	}
	for i := range a.Batches {
		if a.Batches[i].ID != b.Batches[i].ID {
			t.Fatalf("order differs at %d: %s vs %s", i, a.Batches[i].ID, b.Batches[i].ID)
		}
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = s.Ingest(batch(fmt.Sprintf("w%d-b%d", w, i), int64(i),
					model.CaseResult{TestID: "t1", Status: model.StatusPass}))
			}
		}(w)
	}
	// Readers must always observe whole batches, never partial state.
	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
				snap := s.Snapshot()
				for _, b := range snap.Batches {
					if b.ID == "" || b.Source == "" {
						t.Error("observed partially written batch")
					}
				}
			}
		}
	}()
	wg.Wait()
	close(stop)
	<-readerDone
	if s.Len() != 200 {
		t.Fatalf("len = %d, want 200", s.Len())
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	s := New()
	for i := int64(0); i < 10; i++ {
		s.Ingest(batch(fmt.Sprintf("b%d", i), i, model.CaseResult{TestID: "t1", Status: model.StatusPass}))
	}
	if removed := s.Prune(3); removed != 7 {
		t.Fatalf("removed = %d, want 7", removed)
	}
	snap := s.Snapshot()
	if len(snap.Batches) != 3 {
		t.Fatalf("len = %d, want 3", len(snap.Batches))
	}
	for i, b := range snap.Batches {
		want := int64(7 + i)
		if b.Index != want {
			t.Fatalf("batch %d index = %d, want %d", i, b.Index, want)
		}
	}
	// Pruning again removes nothing.
	if removed := s.Prune(3); removed != 0 {
		t.Fatalf("second prune removed %d", removed)
	}
}
