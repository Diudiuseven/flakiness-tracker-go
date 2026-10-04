package flakiness

import (
	"fmt"
	"sort"
	"time"
)

// QuarantinePolicy 隔离策略：只有偶发不稳定（flaky）的用例可进入隔离名单。
// 持续失败（consistently_failing）与样本不足的用例一律不隔离。
type QuarantinePolicy struct {
	Version      int
	Window       Window
	MinSamples   int
	FlakeRateMin float64 // 失败率下界（含），低于该值视为稳定
	FlakeRateMax float64 // 失败率上界（不含），持续失败用例失败率为 1.0，天然被排除
}

// QuarantineEntry 是一条隔离名单记录。
type QuarantineEntry struct {
	TestID        string
	Stats         TestStats
	PolicyVersion int
	DecidedAt     time.Time
	Reason        string
}

// Evaluation 是一次策略重判的可追溯记录。
type Evaluation struct {
	PolicyVersion int
	Policy        QuarantinePolicy
	DecidedAt     time.Time
	Entries       []QuarantineEntry
}

// Quarantine 按当前策略计算隔离名单，并追加一条 Evaluation 审计记录。
// 判定规则：分类为 flaky 且 FlakeRateMin <= FailureRate < FlakeRateMax。
// 相同数据与相同策略得到相同名单（按 TestID 排序输出，保证确定性）。
func (s *Store) Quarantine(p QuarantinePolicy) []QuarantineEntry {
	return s.quarantineAt(p, time.Now())
}

// quarantineAt 允许测试注入时间。
func (s *Store) quarantineAt(p QuarantinePolicy, now time.Time) []QuarantineEntry {
	stats := s.Stats(p.Window, p.MinSamples)
	entries := make([]QuarantineEntry, 0)
	for id, st := range stats {
		if st.Classification != ClassFlaky {
			continue
		}
		if st.FailureRate < p.FlakeRateMin || st.FailureRate >= p.FlakeRateMax {
			continue
		}
		entries = append(entries, QuarantineEntry{
			TestID:        id,
			Stats:         st,
			PolicyVersion: p.Version,
			DecidedAt:     now,
			Reason: fmt.Sprintf("flaky: failure_rate=%.4f in [%g,%g), samples=%d>=%d",
				st.FailureRate, p.FlakeRateMin, p.FlakeRateMax, st.Counted, p.MinSamples),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].TestID < entries[j].TestID })

	s.mu.Lock()
	s.evaluations = append(s.evaluations, Evaluation{
		PolicyVersion: p.Version,
		Policy:        p,
		DecidedAt:     now,
		Entries:       entries,
	})
	s.mu.Unlock()
	return entries
}

// Evaluations 返回历史策略评估记录（审计追溯），按时间升序。
func (s *Store) Evaluations() []Evaluation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Evaluation, len(s.evaluations))
	copy(out, s.evaluations)
	return out
}
