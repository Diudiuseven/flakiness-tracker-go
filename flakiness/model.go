// Package flakiness 提供测试不稳定度跟踪与可靠性判定能力。
package flakiness

import "time"

// Outcome 表示单次用例运行结果。
type Outcome string

const (
	OutcomePassed  Outcome = "passed"
	OutcomeFailed  Outcome = "failed"
	OutcomeSkipped Outcome = "skipped"
)

// TestResult 是一条用例在某个运行批次中的结果。
type TestResult struct {
	TestID  string
	Outcome Outcome
}

// Batch 是一次运行批次的上报单元。
type Batch struct {
	ID        string
	Source    string
	Seq       int64
	StartedAt time.Time
	Results   []TestResult
}

// Classification 是用例在给定窗口内的稳定性分类。
type Classification string

const (
	ClassStable              Classification = "stable"
	ClassFlaky               Classification = "flaky"
	ClassConsistentlyFailing Classification = "consistently_failing"
	ClassInsufficientData    Classification = "insufficient_data"
)

// TestStats 是一个用例在窗口内的统计画像。
type TestStats struct {
	TestID         string
	Total          int
	Passed         int
	Failed         int
	Skipped        int
	Counted        int // passed+failed，跳过不计入不稳定度
	FailureRate    float64
	Classification Classification
}
