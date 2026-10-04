package flakiness

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// storedBatch 是落盘后的批次记录。
type storedBatch struct {
	meta    Batch
	results map[string]Outcome // testID -> outcome
}

// Store 保存所有批次与用例结果，读写并发安全。
// 读操作在单把 RWMutex 保护下返回快照，保证任一时刻读到的是自洽的批次视图。
type Store struct {
	mu          sync.RWMutex
	batches     map[string]*storedBatch // batchID -> batch
	evaluations []Evaluation            // 策略评估审计记录
	baselines   map[string]map[string]BaselineVerdict
}

// NewStore 创建空存储。
func NewStore() *Store {
	return &Store{
		batches:   make(map[string]*storedBatch),
		baselines: make(map[string]map[string]BaselineVerdict),
	}
}

func validOutcome(o Outcome) bool {
	switch o {
	case OutcomePassed, OutcomeFailed, OutcomeSkipped:
		return true
	}
	return false
}

// Ingest 接收一个批次上报，幂等且容忍乱序；非法上报返回 *RejectError。
// 幂等语义：同一 (batchID, testID) 重复上报相同结果不改变统计；
// 同一 (batchID, testID) 上报不同结果视为冲突，整批拒绝。
// 乱序语义：批次按 Seq 排序参与窗口统计，与到达顺序无关。
func (s *Store) Ingest(b Batch) error {
	if b.ID == "" {
		return &RejectError{Reason: ReasonEmptyBatchID, Message: "batch id must not be empty"}
	}
	if b.Seq <= 0 {
		return &RejectError{Reason: ReasonNonPositiveSeq, Message: fmt.Sprintf("batch %q: seq must be positive, got %d", b.ID, b.Seq)}
	}
	if len(b.Results) == 0 {
		return &RejectError{Reason: ReasonEmptyResults, Message: fmt.Sprintf("batch %q: no results", b.ID)}
	}
	results := make(map[string]Outcome, len(b.Results))
	for _, r := range b.Results {
		if r.TestID == "" {
			return &RejectError{Reason: ReasonEmptyTestID, Message: fmt.Sprintf("batch %q: test id must not be empty", b.ID)}
		}
		if !validOutcome(r.Outcome) {
			return &RejectError{Reason: ReasonUnknownOutcome, Message: fmt.Sprintf("batch %q test %q: unknown outcome %q", b.ID, r.TestID, r.Outcome)}
		}
		if prev, dup := results[r.TestID]; dup && prev != r.Outcome {
			return &RejectError{Reason: ReasonConflictingResult, Message: fmt.Sprintf("batch %q test %q: conflicting outcomes %q vs %q within batch", b.ID, r.TestID, prev, r.Outcome)}
		}
		results[r.TestID] = r.Outcome
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.batches[b.ID]; ok {
		// 重复批次：逐条比对，全部一致则为幂等重放（无操作），否则冲突拒绝。
		for id, o := range results {
			if prev, ok := existing.results[id]; ok && prev != o {
				return &RejectError{Reason: ReasonConflictingResult, Message: fmt.Sprintf("batch %q test %q: already recorded as %q, got %q", b.ID, id, prev, o)}
			}
		}
		for id, o := range results {
			existing.results[id] = o
		}
		return nil
	}
	s.batches[b.ID] = &storedBatch{meta: b, results: results}
	return nil
}

// Batches 返回当前所有批次的元信息（按 Seq 升序，Seq 相同按 ID 字典序保证确定性）。
func (s *Store) Batches() []Batch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Batch, 0, len(s.batches))
	for _, b := range s.batches {
		out = append(out, b.meta)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seq != out[j].Seq {
			return out[i].Seq < out[j].Seq
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Results 返回指定批次内全部用例结果的快照（拷贝，调用方修改不影响存储）。
func (s *Store) Results(batchID string) (map[string]Outcome, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.batches[batchID]
	if !ok {
		return nil, false
	}
	out := make(map[string]Outcome, len(b.results))
	for id, o := range b.results {
		out[id] = o
	}
	return out, true
}

// PruneBeforeSeq 清理 Seq 小于给定值的批次，返回清理数量。
// 窗口统计只基于剩余批次计算，清理窗口外数据不影响窗口内结果。
func (s *Store) PruneBeforeSeq(seq int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, b := range s.batches {
		if b.meta.Seq < seq {
			delete(s.batches, id)
			n++
		}
	}
	return n
}

// PruneBeforeTime 清理 StartedAt 早于给定时间的批次，返回清理数量。
func (s *Store) PruneBeforeTime(t time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, b := range s.batches {
		if b.meta.StartedAt.Before(t) {
			delete(s.batches, id)
			n++
		}
	}
	return n
}
