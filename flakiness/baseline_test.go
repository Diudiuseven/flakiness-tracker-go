package flakiness

import "testing"

func TestBaselineComparison(t *testing.T) {
	s := NewStore()
	// 当前：t-new 交替（flaky），t-rec 全通过（stable），t-same 交替（flaky）。
	ingestOutcomes(t, s, "t-new", OutcomePassed, OutcomeFailed, OutcomePassed, OutcomeFailed)
	ingestOutcomes(t, s, "t-rec", OutcomePassed, OutcomePassed, OutcomePassed, OutcomePassed)
	ingestOutcomes(t, s, "t-same", OutcomeFailed, OutcomePassed, OutcomeFailed, OutcomePassed)

	baselineMain := map[string]Classification{
		"t-new":  ClassStable, // 基线稳定 -> 新出现的不稳定
		"t-rec":  ClassFlaky,  // 基线不稳定 -> 已恢复
		"t-same": ClassFlaky,  // 基线不稳定，当前仍不稳定 -> 不变
	}
	cmp := s.CompareWithBaseline("main", baselineMain, Window{}, 3)
	if got := cmp.Verdicts["t-new"]; got != VerdictNewlyFlaky {
		t.Errorf("t-new = %s, want newly_flaky", got)
	}
	if got := cmp.Verdicts["t-rec"]; got != VerdictRecovered {
		t.Errorf("t-rec = %s, want recovered", got)
	}
	if got := cmp.Verdicts["t-same"]; got != VerdictUnchanged {
		t.Errorf("t-same = %s, want unchanged", got)
	}

	// 同一用例关联另一基线，结论独立存储、互不覆盖。
	baselineRelease := map[string]Classification{"t-new": ClassFlaky}
	s.CompareWithBaseline("release-1.0", baselineRelease, Window{}, 3)
	if got, _ := s.BaselineVerdictFor("release-1.0", "t-new"); got != VerdictUnchanged {
		t.Errorf("release-1.0/t-new = %s, want unchanged", got)
	}
	if got, _ := s.BaselineVerdictFor("main", "t-new"); got != VerdictNewlyFlaky {
		t.Errorf("main/t-new overwritten by other baseline: %s", got)
	}
	if _, ok := s.BaselineVerdictFor("no-such", "t-new"); ok {
		t.Error("unknown baseline must return ok=false")
	}
}
