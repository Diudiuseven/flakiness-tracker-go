package flakiness

import "sort"

// Window 定义统计窗口：按最近 N 次运行。
type Window struct {
	LastRuns int // >0 时取最近 N 个批次（按 Seq 升序的尾部）；<=0 表示全部批次
}

// classify 根据计数给出稳定性分类。
// 样本不足（counted < minSamples）一律为 ClassInsufficientData，不给确定性结论。
func classify(counted, failed int, minSamples int) Classification {
	if counted < minSamples {
		return ClassInsufficientData
	}
	if failed == 0 {
		return ClassStable
	}
	if failed == counted {
		return ClassConsistentlyFailing
	}
	return ClassFlaky
}

// windowBatches 在读锁内取出窗口内的批次快照（按 Seq 升序）。
func (s *Store) windowBatches(w Window) []*storedBatch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]*storedBatch, 0, len(s.batches))
	for _, b := range s.batches {
		all = append(all, b)
	}
	sortBatches(all)
	if w.LastRuns > 0 && len(all) > w.LastRuns {
		all = all[len(all)-w.LastRuns:]
	}
	return all
}

func sortBatches(bs []*storedBatch) {
	// 插入排序之外的简单做法：复用 sort。
	sort.Slice(bs, func(i, j int) bool {
		if bs[i].meta.Seq != bs[j].meta.Seq {
			return bs[i].meta.Seq < bs[j].meta.Seq
		}
		return bs[i].meta.ID < bs[j].meta.ID
	})
}

// Stats 计算窗口内每个用例的统计画像。
// minSamples：Counted（passed+failed，skipped 不计）小于该值时分类为 ClassInsufficientData。
func (s *Store) Stats(w Window, minSamples int) map[string]TestStats {
	batches := s.windowBatches(w)
	acc := map[string]*TestStats{}
	for _, b := range batches {
		for id, o := range b.results {
			st, ok := acc[id]
			if !ok {
				st = &TestStats{TestID: id}
				acc[id] = st
			}
			st.Total++
			switch o {
			case OutcomePassed:
				st.Passed++
			case OutcomeFailed:
				st.Failed++
			case OutcomeSkipped:
				st.Skipped++
			}
		}
	}
	out := make(map[string]TestStats, len(acc))
	for id, st := range acc {
		st.Counted = st.Passed + st.Failed
		if st.Counted > 0 {
			st.FailureRate = float64(st.Failed) / float64(st.Counted)
		}
		st.Classification = classify(st.Counted, st.Failed, minSamples)
		out[id] = *st
	}
	return out
}

// StatsFor 计算单个用例的统计画像。
func (s *Store) StatsFor(testID string, w Window, minSamples int) TestStats {
	if st, ok := s.Stats(w, minSamples)[testID]; ok {
		return st
	}
	return TestStats{TestID: testID, Classification: ClassInsufficientData}
}
