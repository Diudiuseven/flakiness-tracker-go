package quarantine

import (
	"testing"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/flakiness"
)

func rep(id string, samples int, rate float64, v flakiness.Verdict) flakiness.Report {
	return flakiness.Report{TestID: id, Samples: samples, FailureRate: rate, Verdict: v}
}

func TestOnlyFlakyQuarantined(t *testing.T) {
	l := NewList()
	reports := map[string]flakiness.Report{
		"flaky1":      rep("flaky1", 10, 0.3, flakiness.VerdictFlaky),
		"persistent1": rep("persistent1", 10, 1.0, flakiness.VerdictPersistent),
		"stable1":     rep("stable1", 10, 0.0, flakiness.VerdictStable),
		"new1":        rep("new1", 1, 1.0, flakiness.VerdictInsufficient),
	}
	p := Policy{MinSamples: 3, MaxFailureRate: 1.0, Version: "v1"}
	decisions := l.Evaluate(reports, p)

	if !l.Contains("flaky1") {
		t.Error("flaky1 should be quarantined")
	}
	for _, id := range []string{"persistent1", "stable1", "new1"} {
		if l.Contains(id) {
			t.Errorf("%s must not be quarantined", id)
		}
	}
	// Persistent failure must carry an explicit non-quarantine reason.
	for _, d := range decisions {
		if d.TestID == "persistent1" && d.Quarantined {
			t.Error("persistent failure quarantined")
		}
	}
}

func TestThresholdAdjustmentTraceable(t *testing.T) {
	l := NewList()
	reports := map[string]flakiness.Report{
		"t1": rep("t1", 10, 0.9, flakiness.VerdictFlaky),
	}
	// v1: rate 0.9 < 1.0 -> quarantined.
	l.Evaluate(reports, Policy{MinSamples: 3, MaxFailureRate: 1.0, Version: "v1"})
	if !l.Contains("t1") {
		t.Fatal("t1 should be quarantined under v1")
	}
	// v2: stricter, rate 0.9 >= 0.5 -> persistent, removed from quarantine.
	l.Evaluate(reports, Policy{MinSamples: 3, MaxFailureRate: 0.5, Version: "v2"})
	if l.Contains("t1") {
		t.Fatal("t1 should be released under v2")
	}
	audit := l.Audit()
	if len(audit) != 2 {
		t.Fatalf("audit len = %d, want 2", len(audit))
	}
	if audit[0].PolicyVersion != "v1" || audit[1].PolicyVersion != "v2" {
		t.Fatalf("audit versions = %s,%s", audit[0].PolicyVersion, audit[1].PolicyVersion)
	}
	if !audit[0].Quarantined || audit[1].Quarantined {
		t.Fatal("audit does not explain the decision change")
	}
}
