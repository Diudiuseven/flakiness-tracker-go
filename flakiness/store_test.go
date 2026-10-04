package flakiness

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func mkBatch(id string, seq int64, results ...TestResult) Batch {
	return Batch{
		ID:        id,
		Source:    "ci",
		Seq:       seq,
		StartedAt: time.Unix(seq, 0),
		Results:   results,
	}
}

func TestIngestRejectsInvalid(t *testing.T) {
	s := NewStore()
	cases := []struct {
		name   string
		batch  Batch
		reason RejectReason
	}{
		{"empty batch id", mkBatch("", 1, TestResult{TestID: "t1", Outcome: OutcomePassed}), ReasonEmptyBatchID},
		{"zero seq", mkBatch("b1", 0, TestResult{TestID: "t1", Outcome: OutcomePassed}), ReasonNonPositiveSeq},
		{"negative seq", mkBatch("b1", -2, TestResult{TestID: "t1", Outcome: OutcomePassed}), ReasonNonPositiveSeq},
		{"empty results", mkBatch("b1", 1), ReasonEmptyResults},
		{"empty test id", mkBatch("b1", 1, TestResult{TestID: "", Outcome: OutcomePassed}), ReasonEmptyTestID},
		{"bad outcome", mkBatch("b1", 1, TestResult{TestID: "t1", Outcome: "exploded"}), ReasonUnknownOutcome},
		{"conflict within batch", mkBatch("b1", 1,
			TestResult{TestID: "t1", Outcome: OutcomePassed},
			TestResult{TestID: "t1", Outcome: OutcomeFailed}), ReasonConflictingResult},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.Ingest(tc.batch)
			var re *RejectError
			if !errors.As(err, &re) {
				t.Fatalf("expected RejectError, got %v", err)
			}
			if re.Reason != tc.reason {
				t.Fatalf("reason = %s, want %s", re.Reason, tc.reason)
			}
		})
	}
	if len(s.Batches()) != 0 {
		t.Fatalf("rejected batches must not be stored, got %d", len(s.Batches()))
	}
}

func TestIngestDuplicateAndOutOfOrder(t *testing.T) {
	s := NewStore()
	// 乱序到达：seq 3 先于 seq 1、2。
	if err := s.Ingest(mkBatch("b3", 3, TestResult{TestID: "t1", Outcome: OutcomeFailed})); err != nil {
		t.Fatal(err)
	}
	if err := s.Ingest(mkBatch("b1", 1, TestResult{TestID: "t1", Outcome: OutcomePassed})); err != nil {
		t.Fatal(err)
	}
	if err := s.Ingest(mkBatch("b2", 2, TestResult{TestID: "t1", Outcome: OutcomePassed})); err != nil {
		t.Fatal(err)
	}
	// 重复上报相同内容：幂等，不改变统计。
	if err := s.Ingest(mkBatch("b2", 2, TestResult{TestID: "t1", Outcome: OutcomePassed})); err != nil {
		t.Fatal(err)
	}
	if err := s.Ingest(mkBatch("b3", 3, TestResult{TestID: "t1", Outcome: OutcomeFailed})); err != nil {
		t.Fatal(err)
	}
	st := s.StatsFor("t1", Window{}, 1)
	if st.Total != 3 || st.Passed != 2 || st.Failed != 1 {
		t.Fatalf("duplicate/out-of-order ingest changed stats: %+v", st)
	}
	// 窗口按 Seq 排序而非到达顺序。
	bs := s.Batches()
	for i, want := range []string{"b1", "b2", "b3"} {
		if bs[i].ID != want {
			t.Fatalf("batch order[%d] = %s, want %s", i, bs[i].ID, want)
		}
	}
	// 冲突重放被拒绝。
	err := s.Ingest(mkBatch("b2", 2, TestResult{TestID: "t1", Outcome: OutcomeFailed}))
	var re *RejectError
	if !errors.As(err, &re) || re.Reason != ReasonConflictingResult {
		t.Fatalf("expected conflicting_result, got %v", err)
	}
	// 冲突拒绝后统计不变。
	st = s.StatsFor("t1", Window{}, 1)
	if st.Passed != 2 || st.Failed != 1 {
		t.Fatalf("conflicting ingest mutated stats: %+v", st)
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 1; i <= 50; i++ {
				_ = s.Ingest(mkBatch(fmt.Sprintf("w%d-b%d", w, i), int64(i),
					TestResult{TestID: fmt.Sprintf("t%d", w), Outcome: OutcomePassed}))
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = s.Batches()
				_ = s.Stats(Window{LastRuns: 10}, 3)
			}
		}()
	}
	wg.Wait()
	if got := len(s.Batches()); got != 8*50 {
		t.Fatalf("batches = %d, want %d", got, 8*50)
	}
}

func TestPruneKeepsWindowStatsStable(t *testing.T) {
	s := NewStore()
	for i := int64(1); i <= 10; i++ {
		o := OutcomePassed
		if i%2 == 0 {
			o = OutcomeFailed
		}
		if err := s.Ingest(mkBatch(fmt.Sprintf("b%d", i), i, TestResult{TestID: "t1", Outcome: o})); err != nil {
			t.Fatal(err)
		}
	}
	before := s.StatsFor("t1", Window{LastRuns: 5}, 1)
	// 清理窗口外的旧批次（保留 seq>=6，即最近 5 个）。
	if n := s.PruneBeforeSeq(6); n != 5 {
		t.Fatalf("pruned = %d, want 5", n)
	}
	after := s.StatsFor("t1", Window{LastRuns: 5}, 1)
	if before != after {
		t.Fatalf("window stats jumped after prune: before=%+v after=%+v", before, after)
	}
	// 时间维度清理。
	if n := s.PruneBeforeTime(time.Unix(8, 0)); n != 2 {
		t.Fatalf("pruned by time = %d, want 2", n)
	}
	if got := len(s.Batches()); got != 3 {
		t.Fatalf("remaining batches = %d, want 3", got)
	}
}
