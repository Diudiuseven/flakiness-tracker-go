package flakiness

// BaselineVerdict 是单个用例相对某基线的对比结论。
type BaselineVerdict string

const (
	VerdictNewlyFlaky BaselineVerdict = "newly_flaky" // 基线中稳定/不存在，当前不稳定
	VerdictRecovered  BaselineVerdict = "recovered"   // 基线中不稳定，当前恢复稳定
	VerdictUnchanged  BaselineVerdict = "unchanged"
)

// BaselineComparison 是一次基线对比结果，按 (baselineID, testID) 索引，互不覆盖。
type BaselineComparison struct {
	BaselineID string
	Verdicts   map[string]BaselineVerdict
}

// isFlakyClass 判定分类是否属于"不稳定"（用于基线对比）。
func isFlakyClass(c Classification) bool { return c == ClassFlaky }

// CompareWithBaseline 将当前窗口统计与指定基线快照对比，并持久化结论。
// baseline 为 testID -> 基线时的分类（通常来自基线分支/基线运行的历史统计）。
// 结论按 baselineID 独立存储，同一用例关联多个基线时互不影响。
func (s *Store) CompareWithBaseline(baselineID string, baseline map[string]Classification, w Window, minSamples int) BaselineComparison {
	current := s.Stats(w, minSamples)

	verdicts := make(map[string]BaselineVerdict)
	testIDs := map[string]struct{}{}
	for id := range baseline {
		testIDs[id] = struct{}{}
	}
	for id := range current {
		testIDs[id] = struct{}{}
	}
	for id := range testIDs {
		baseClass, inBase := baseline[id]
		curClass := ClassInsufficientData
		if st, ok := current[id]; ok {
			curClass = st.Classification
		}
		baseFlaky := inBase && isFlakyClass(baseClass)
		curFlaky := isFlakyClass(curClass)
		switch {
		case !baseFlaky && curFlaky:
			verdicts[id] = VerdictNewlyFlaky
		case baseFlaky && !curFlaky && curClass == ClassStable:
			// 只有当前明确恢复为稳定才算 recovered；样本不足不算恢复。
			verdicts[id] = VerdictRecovered
		default:
			verdicts[id] = VerdictUnchanged
		}
	}

	s.mu.Lock()
	if s.baselines[baselineID] == nil {
		s.baselines[baselineID] = make(map[string]BaselineVerdict)
	}
	for id, v := range verdicts {
		s.baselines[baselineID][id] = v
	}
	s.mu.Unlock()
	return BaselineComparison{BaselineID: baselineID, Verdicts: verdicts}
}

// BaselineVerdictFor 查询某用例相对某基线的结论。
func (s *Store) BaselineVerdictFor(baselineID, testID string) (BaselineVerdict, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.baselines[baselineID]
	if !ok {
		return "", false
	}
	v, ok := m[testID]
	return v, ok
}
