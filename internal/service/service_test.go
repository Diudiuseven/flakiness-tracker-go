package service

import (
	"fmt"
	"testing"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/flakiness"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/gate"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/model"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/quarantine"
)

func testConfig() Config {
	return Config{
		Flakiness:  flakiness.DefaultConfig(),
		Policy:     quarantine.Policy{MinSamples: 3, MaxFailureRate: 1.0, Version: "v1"},
		RetainRuns: 50,
	}
}

func ingestRuns(t *testing.T, s *Service, testID string, statuses ...model.Status) []string {
	t.Helper()
	return ingestRunsFrom(t, s, testID, 0, statuses...)
}

func ingestRunsFrom(t *testing.T, s *Service, testID string, start int, statuses ...model.Status) []string {
	t.Helper()
	var ids []string
	for i, st := range statuses {
		id := fmt.Sprintf("%s-run-%d", testID, start+i)
		err := s.Ingest(model.Batch{
			ID:      id,
			Source:  "ci",
			Index:   int64(start + i),
			Results: []model.CaseResult{{TestID: testID, Status: st}},
		})
		if err != nil {
			t.Fatalf("ingest %s: %v", id, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestEndToEndQuarantineAndGate(t *testing.T) {
	s := New(testConfig())
	// flaky test: alternating results
	ingestRuns(t, s, "flaky1", model.StatusPass, model.StatusFail, model.StatusPass, model.StatusFail)
	// persistent failure
	ingestRuns(t, s, "broken1", model.StatusFail, model.StatusFail, model.StatusFail, model.StatusFail)

	reps := s.Reports()
	if reps["flaky1"].Verdict != flakiness.VerdictFlaky {
		t.Fatalf("flaky1 verdict = %s", reps["flaky1"].Verdict)
	}
	if reps["broken1"].Verdict != flakiness.VerdictPersistent {
		t.Fatalf("broken1 verdict = %s", reps["broken1"].Verdict)
	}

	s.ReevaluateQuarantine(s.cfg.Policy)
	members := s.QuarantineMembers()
	if len(members) != 1 || members[0] != "flaky1" {
		t.Fatalf("quarantine members = %v", members)
	}

	// Gate: a run failing only flaky1 is allowed; one failing broken1 blocks.
	d, err := s.Gate([]string{"flaky1-run-1"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != gate.OutcomeAllow {
		t.Fatalf("gate = %s, want allow (%s)", d.Outcome, d.Reason)
	}
	d, err = s.Gate([]string{"broken1-run-0"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != gate.OutcomeBlock {
		t.Fatalf("gate = %s, want block (%s)", d.Outcome, d.Reason)
	}
	// Unknown batch id is an error.
	if _, err := s.Gate([]string{"nope"}); err == nil {
		t.Fatal("expected error for unknown batch")
	}
}

func TestBaselineComparisonFlow(t *testing.T) {
	s := New(testConfig())
	ingestRuns(t, s, "t1", model.StatusPass, model.StatusPass, model.StatusPass)
	s.SetBaseline("main")

	// t1 becomes flaky after baseline.
	ingestRunsFrom(t, s, "t1", 3, model.StatusFail, model.StatusPass, model.StatusFail)
	comp, ok := s.CompareBaseline("main")
	if !ok {
		t.Fatal("baseline main missing")
	}
	found := false
	for _, d := range comp.Diffs {
		if d.TestID == "t1" {
			found = true
			if !d.NewlyFlaky {
				t.Error("t1 should be newly flaky vs main")
			}
		}
	}
	if !found {
		t.Fatal("t1 missing from comparison")
	}
}

func TestRetentionKeepsStatsConsistent(t *testing.T) {
	cfg := testConfig()
	cfg.RetainRuns = 4
	s := New(cfg)
	// 4 old failing runs, then 4 passing runs.
	ingestRuns(t, s, "t1",
		model.StatusFail, model.StatusFail, model.StatusFail, model.StatusFail,
		model.StatusPass, model.StatusPass, model.StatusPass, model.StatusPass)
	if removed := s.Prune(); removed != 4 {
		t.Fatalf("removed = %d, want 4", removed)
	}
	rep := s.Reports()["t1"]
	// After pruning, only the 4 passing runs remain: stats must reflect the
	// remaining window exactly, not stale aggregates.
	if rep.Pass != 4 || rep.Fail != 0 || rep.Verdict != flakiness.VerdictStable {
		t.Fatalf("post-prune report = %+v", rep)
	}
	// Prune is idempotent.
	if removed := s.Prune(); removed != 0 {
		t.Fatalf("second prune removed %d", removed)
	}
}
