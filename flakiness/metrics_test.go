package flakiness

import (
	"fmt"
	"testing"
)

// ingestOutcomes 便捷函数：为 testID 按顺序注入 seq=1..n 的结果。
func ingestOutcomes(t *testing.T, s *Store, testID string, outcomes ...Outcome) {
	t.Helper()
	for i, o := range outcomes {
		if err := s.Ingest(mkBatch(fmt.Sprintf("%s-b%d", testID, i), int64(i+1),
			TestResult{TestID: testID, Outcome: o})); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClassification(t *testing.T) {
	s := NewStore()
	ingestOutcomes(t, s, "stable", OutcomePassed, OutcomePassed, OutcomePassed, OutcomePassed)
	ingestOutcomes(t, s, "flaky", OutcomePassed, OutcomeFailed, OutcomePassed, OutcomeFailed)
	ingestOutcomes(t, s, "broken", OutcomeFailed, OutcomeFailed, OutcomeFailed, OutcomeFailed)
	ingestOutcomes(t, s, "newbie", OutcomePassed, OutcomeFailed) // 样本不足
	ingestOutcomes(t, s, "skipped-heavy", OutcomeSkipped, OutcomeSkipped, OutcomePassed)

	stats := s.Stats(Window{}, 3)
	want := map[string]Classification{
		"stable": ClassStable,
		"flaky":  ClassFlaky,
		"broken": ClassConsistentlyFailing,
		"newbie": ClassInsufficientData,
	}
	for id, c := range want {
		if got := stats[id].Classification; got != c {
			t.Errorf("%s: classification = %s, want %s", id, got, c)
		}
	}
	// 大量跳过时 counted 不足，不得给出确定性结论。
	if got := stats["skipped-heavy"].Classification; got != ClassInsufficientData {
		t.Errorf("skipped-heavy: classification = %s, want %s", got, ClassInsufficientData)
	}
	// flaky 失败率 = 0.5。
	if r := stats["flaky"].FailureRate; r != 0.5 {
		t.Errorf("flaky failure rate = %v, want 0.5", r)
	}
}

func TestWindowLastRuns(t *testing.T) {
	s := NewStore()
	// 前 5 次全失败，后 5 次全通过。
	for i := 1; i <= 5; i++ {
		ingestOutcomes(t, s, "t1", OutcomeFailed)
	}
	// ingestOutcomes 每个批次 seq 独立，改为手工注入保证 seq 连续。
	s2 := NewStore()
	for i := 1; i <= 10; i++ {
		o := OutcomeFailed
		if i > 5 {
			o = OutcomePassed
		}
		if err := s2.Ingest(mkBatch(fmt.Sprintf("b%d", i), int64(i), TestResult{TestID: "t1", Outcome: o})); err != nil {
			t.Fatal(err)
		}
	}
	st := s2.StatsFor("t1", Window{LastRuns: 5}, 3)
	if st.Classification != ClassStable {
		t.Fatalf("last-5 window classification = %s, want stable", st.Classification)
	}
	st = s2.StatsFor("t1", Window{}, 3)
	if st.Classification != ClassFlaky {
		t.Fatalf("full window classification = %s, want flaky", st.Classification)
	}
}
