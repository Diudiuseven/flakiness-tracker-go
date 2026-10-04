package flakiness

import (
	"fmt"
	"sort"
)

// GateDecision 是运行放行判定结论。
type GateDecision string

const (
	GateAllow GateDecision = "allow"
	GateBlock GateDecision = "block"
)

// GateConfig 放行判定配置。
type GateConfig struct {
	Window           Window
	MinSamples       int
	BlockOnFlaky     bool // 失败用例属于未隔离的 flaky 时是否阻断
	QuarantinePolicy QuarantinePolicy
}

// GateReport 是判定输出，包含可复现的理由。
type GateReport struct {
	Decision       GateDecision
	Reasons        []string
	QuarantinedHit []string // 失败但命中隔离名单的用例（不阻断）
	FlakyHit       []string // 失败且属于未隔离 flaky 的用例
	HardFailed     []string // 失败且分类为持续失败的用例
	UnknownBatches []string // 未找到的批次
}

// Decide 对给定批次集合做放行判定。
// 判定规则（确定性：相同输入与相同配置必得相同结论）：
//  1. 批次不存在 -> block（输入非法）。
//  2. 批次中失败的用例，按窗口统计分类：
//     - 命中隔离名单（flaky 且已隔离）-> 记录但不阻断；
//     - 持续失败 -> block；
//     - 未隔离 flaky -> BlockOnFlaky 时 block，否则仅记录；
//     - 稳定/样本不足 -> 视为偶发，不阻断。
func (s *Store) Decide(batchIDs []string, cfg GateConfig) GateReport {
	rep := GateReport{Decision: GateAllow}

	// 收集失败用例（去重）。
	failed := map[string]bool{}
	for _, bid := range batchIDs {
		res, ok := s.Results(bid)
		if !ok {
			rep.UnknownBatches = append(rep.UnknownBatches, bid)
			continue
		}
		for id, o := range res {
			if o == OutcomeFailed {
				failed[id] = true
			}
		}
	}
	sort.Strings(rep.UnknownBatches)
	if len(rep.UnknownBatches) > 0 {
		rep.Decision = GateBlock
		rep.Reasons = append(rep.Reasons, fmt.Sprintf("unknown batches: %v", rep.UnknownBatches))
	}

	// 隔离名单（基于策略，纯函数式计算，不写审计，保证判定可复现）。
	stats := s.Stats(cfg.QuarantinePolicy.Window, cfg.QuarantinePolicy.MinSamples)
	quarantined := map[string]bool{}
	for id, st := range stats {
		if st.Classification == ClassFlaky &&
			st.FailureRate >= cfg.QuarantinePolicy.FlakeRateMin &&
			st.FailureRate < cfg.QuarantinePolicy.FlakeRateMax {
			quarantined[id] = true
		}
	}

	gateStats := s.Stats(cfg.Window, cfg.MinSamples)
	for id := range failed {
		st := gateStats[id]
		switch {
		case quarantined[id]:
			rep.QuarantinedHit = append(rep.QuarantinedHit, id)
		case st.Classification == ClassConsistentlyFailing:
			rep.HardFailed = append(rep.HardFailed, id)
		case st.Classification == ClassFlaky:
			rep.FlakyHit = append(rep.FlakyHit, id)
		}
	}
	sort.Strings(rep.QuarantinedHit)
	sort.Strings(rep.FlakyHit)
	sort.Strings(rep.HardFailed)

	if len(rep.HardFailed) > 0 {
		rep.Decision = GateBlock
		rep.Reasons = append(rep.Reasons, fmt.Sprintf("consistently failing tests: %v", rep.HardFailed))
	}
	if len(rep.FlakyHit) > 0 {
		if cfg.BlockOnFlaky {
			rep.Decision = GateBlock
			rep.Reasons = append(rep.Reasons, fmt.Sprintf("flaky (not quarantined) tests failed: %v", rep.FlakyHit))
		} else {
			rep.Reasons = append(rep.Reasons, fmt.Sprintf("flaky (not quarantined) tests failed but tolerated: %v", rep.FlakyHit))
		}
	}
	if len(rep.QuarantinedHit) > 0 {
		rep.Reasons = append(rep.Reasons, fmt.Sprintf("quarantined tests failed (ignored): %v", rep.QuarantinedHit))
	}
	if rep.Decision == GateAllow && len(rep.Reasons) == 0 {
		rep.Reasons = append(rep.Reasons, "no blocking failures")
	}
	return rep
}
