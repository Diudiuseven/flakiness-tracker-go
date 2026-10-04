package gate

import (
	"testing"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/flakiness"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/model"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/quarantine"
)

func failedBatch(id string, tests ...string) model.Batch {
	b := model.Batch{ID: id, Source: "ci"}
	for _, t := range tests {
		b.Results = append(b.Results, model.CaseResult{TestID: t, Status: model.StatusFail})
	}
	return b
}

func quarantined(ids ...string) *quarantine.List {
	l := quarantine.NewList()
	reports := map[string]flakiness.Report{}
	for _, id := range ids {
		reports[id] = flakiness.Report{TestID: id, Samples: 10, FailureRate: 0.3, Verdict: flakiness.VerdictFlaky}
	}
	l.Evaluate(reports, quarantine.Policy{MinSamples: 3, MaxFailureRate: 1.0, Version: "v1"})
	return l
}

func TestBlocksUnquarantinedFailure(t *testing.T) {
	reports := map[string]flakiness.Report{
		"t1": {TestID: "t1", Samples: 10, FailureRate: 1.0, Verdict: flakiness.VerdictPersistent},
	}
	d := Evaluate([]model.Batch{failedBatch("b1", "t1")}, reports, quarantine.NewList())
	if d.Outcome != OutcomeBlock {
		t.Fatalf("outcome = %s, want block", d.Outcome)
	}
	if len(d.Failures) != 1 || !d.Failures[0].Blocking {
		t.Fatalf("failures = %+v", d.Failures)
	}
}

func TestAllowsQuarantinedFailure(t *testing.T) {
	reports := map[string]flakiness.Report{
		"t1": {TestID: "t1", Samples: 10, FailureRate: 0.3, Verdict: flakiness.VerdictFlaky},
	}
	d := Evaluate([]model.Batch{failedBatch("b1", "t1")}, reports, quarantined("t1"))
	if d.Outcome != OutcomeAllow {
		t.Fatalf("outcome = %s, want allow", d.Outcome)
	}
	if d.Failures[0].Blocking {
		t.Error("quarantined failure must not block")
	}
}

func TestDeterministic(t *testing.T) {
	reports := map[string]flakiness.Report{
		"t1": {TestID: "t1", Samples: 10, FailureRate: 0.3, Verdict: flakiness.VerdictFlaky},
		"t2": {TestID: "t2", Samples: 10, FailureRate: 1.0, Verdict: flakiness.VerdictPersistent},
	}
	batches := []model.Batch{failedBatch("b1", "t2", "t1"), failedBatch("b2", "t1")}
	q := quarantined("t1")
	d1 := Evaluate(batches, reports, q)
	for i := 0; i < 20; i++ {
		d2 := Evaluate(batches, reports, q)
		if d1.Outcome != d2.Outcome || d1.Reason != d2.Reason || len(d1.Failures) != len(d2.Failures) {
			t.Fatalf("non-deterministic decision: %+v vs %+v", d1, d2)
		}
		for j := range d1.Failures {
			if d1.Failures[j] != d2.Failures[j] {
				t.Fatalf("failure %d differs", j)
			}
		}
	}
	if d1.Outcome != OutcomeBlock {
		t.Fatal("t2 persistent failure should block")
	}
}
