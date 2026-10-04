package baseline

import (
	"testing"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/flakiness"
)

func rep(v flakiness.Verdict) flakiness.Report { return flakiness.Report{Verdict: v} }

func TestNewlyFlakyAndRecovered(t *testing.T) {
	r := NewRegistry()
	r.SetBaseline("main", map[string]flakiness.Report{
		"a": rep(flakiness.VerdictStable),
		"b": rep(flakiness.VerdictFlaky),
	})
	current := map[string]flakiness.Report{
		"a": rep(flakiness.VerdictFlaky),  // became flaky
		"b": rep(flakiness.VerdictStable), // recovered
		"c": rep(flakiness.VerdictFlaky),  // brand-new flaky test
	}
	comp, ok := r.Compare("main", current)
	if !ok {
		t.Fatal("baseline main not found")
	}
	got := map[string]flakinessReportDiff{}
	for _, d := range comp.Diffs {
		got[d.TestID] = flakinessReportDiff{newlyFlaky: d.NewlyFlaky, recovered: d.Recovered}
	}
	if !got["a"].newlyFlaky || got["a"].recovered {
		t.Error("a should be newly flaky")
	}
	if got["b"].newlyFlaky || !got["b"].recovered {
		t.Error("b should be recovered")
	}
	if !got["c"].newlyFlaky {
		t.Error("c (absent at baseline) should be newly flaky")
	}
}

type flakinessReportDiff struct{ newlyFlaky, recovered bool }

func TestMultipleBaselinesIndependent(t *testing.T) {
	r := NewRegistry()
	r.SetBaseline("main", map[string]flakiness.Report{"a": rep(flakiness.VerdictStable)})
	r.SetBaseline("release-1", map[string]flakiness.Report{"a": rep(flakiness.VerdictFlaky)})

	current := map[string]flakiness.Report{"a": rep(flakiness.VerdictFlaky)}
	main, _ := r.Compare("main", current)
	rel, _ := r.Compare("release-1", current)
	if !main.Diffs[0].NewlyFlaky {
		t.Error("vs main: a should be newly flaky")
	}
	if rel.Diffs[0].NewlyFlaky || rel.Diffs[0].Recovered {
		t.Error("vs release-1: a was already flaky, no change expected")
	}
	// Comparing against one baseline must not disturb the other.
	main2, _ := r.Compare("main", current)
	if !main2.Diffs[0].NewlyFlaky {
		t.Error("main comparison changed after release-1 comparison")
	}
}

func TestUnknownBaseline(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Compare("nope", nil); ok {
		t.Error("expected ok=false for unknown baseline")
	}
}
