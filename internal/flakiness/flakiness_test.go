package flakiness

import (
	"testing"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/model"
)

func runs(statuses ...model.Status) []model.Batch {
	var batches []model.Batch
	for i, st := range statuses {
		batches = append(batches, model.Batch{
			ID:      string(rune('a' + i)),
			Source:  "ci",
			Index:   int64(i),
			Results: []model.CaseResult{{TestID: "t1", Status: st}},
		})
	}
	return batches
}

func verdictOf(t *testing.T, batches []model.Batch, cfg Config) Report {
	t.Helper()
	reps := Analyze(batches, cfg)
	rep, ok := reps["t1"]
	if !ok {
		t.Fatal("no report for t1")
	}
	return rep
}

func TestPersistentFailure(t *testing.T) {
	cfg := DefaultConfig()
	rep := verdictOf(t, runs(model.StatusFail, model.StatusFail, model.StatusFail, model.StatusFail), cfg)
	if rep.Verdict != VerdictPersistent {
		t.Fatalf("verdict = %s, want persistent_failure", rep.Verdict)
	}
	if rep.FailureRate != 1.0 {
		t.Fatalf("rate = %v, want 1.0", rep.FailureRate)
	}
}

func TestIntermittentFlaky(t *testing.T) {
	cfg := DefaultConfig()
	rep := verdictOf(t, runs(model.StatusPass, model.StatusFail, model.StatusPass, model.StatusFail), cfg)
	if rep.Verdict != VerdictFlaky {
		t.Fatalf("verdict = %s, want flaky", rep.Verdict)
	}
	if rep.Transitions != 3 {
		t.Fatalf("transitions = %d, want 3", rep.Transitions)
	}
}

func TestStable(t *testing.T) {
	cfg := DefaultConfig()
	rep := verdictOf(t, runs(model.StatusPass, model.StatusPass, model.StatusPass), cfg)
	if rep.Verdict != VerdictStable {
		t.Fatalf("verdict = %s, want stable", rep.Verdict)
	}
}

func TestInsufficientSamples(t *testing.T) {
	cfg := DefaultConfig() // MinSamples = 3
	// Only 2 pass/fail samples: no deterministic verdict allowed.
	rep := verdictOf(t, runs(model.StatusFail, model.StatusFail), cfg)
	if rep.Verdict != VerdictInsufficient {
		t.Fatalf("verdict = %s, want insufficient_data", rep.Verdict)
	}
	// Skips do not count as samples.
	rep = verdictOf(t, runs(model.StatusPass, model.StatusSkip, model.StatusSkip, model.StatusSkip), cfg)
	if rep.Verdict != VerdictInsufficient {
		t.Fatalf("verdict = %s, want insufficient_data (skips excluded)", rep.Verdict)
	}
	if rep.Samples != 1 || rep.Skip != 3 {
		t.Fatalf("samples = %d skip = %d, want 1/3", rep.Samples, rep.Skip)
	}
}

func TestRunWindow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WindowRuns = 3
	// Old failures outside the window must not affect the verdict.
	batches := runs(model.StatusFail, model.StatusFail, model.StatusFail,
		model.StatusPass, model.StatusPass, model.StatusPass)
	rep := verdictOf(t, batches, cfg)
	if rep.Verdict != VerdictStable {
		t.Fatalf("verdict = %s, want stable (window excludes old failures)", rep.Verdict)
	}
	if rep.Pass != 3 || rep.Fail != 0 {
		t.Fatalf("pass/fail = %d/%d, want 3/0", rep.Pass, rep.Fail)
	}
}

func TestPersistentThresholdConfigurable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PersistentThreshold = 0.8
	rep := verdictOf(t, runs(model.StatusFail, model.StatusFail, model.StatusFail, model.StatusFail, model.StatusPass), cfg)
	if rep.Verdict != VerdictPersistent {
		t.Fatalf("verdict = %s, want persistent at rate 0.8", rep.Verdict)
	}
}
