package flakiness

import (
	"fmt"
	"testing"
)

// buildPolicyStore 构造：flaky-t（交替）、broken-t（持续失败）、stable-t（全通过）。
func buildPolicyStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	for i := 1; i <= 6; i++ {
		flaky := OutcomePassed
		if i%2 == 0 {
			flaky = OutcomeFailed
		}
		if err := s.Ingest(mkBatch(fmt.Sprintf("b%d", i), int64(i),
			TestResult{TestID: "flaky-t", Outcome: flaky},
			TestResult{TestID: "broken-t", Outcome: OutcomeFailed},
			TestResult{TestID: "stable-t", Outcome: OutcomePassed})); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func testPolicy(version int) QuarantinePolicy {
	return QuarantinePolicy{
		Version:      version,
		Window:       Window{},
		MinSamples:   3,
		FlakeRateMin: 0.1,
		FlakeRateMax: 1.0,
	}
}

func TestQuarantineOnlyFlaky(t *testing.T) {
	s := buildPolicyStore(t)
	entries := s.Quarantine(testPolicy(1))
	if len(entries) != 1 || entries[0].TestID != "flaky-t" {
		t.Fatalf("quarantine = %+v, want only flaky-t", entries)
	}
	// 持续失败用例不得被隔离。
	for _, e := range entries {
		if e.TestID == "broken-t" {
			t.Fatal("consistently failing test must not be quarantined")
		}
	}
}

func TestPolicyThresholdAdjustmentIsAuditable(t *testing.T) {
	s := buildPolicyStore(t)
	s.Quarantine(testPolicy(1))
	// 调整阈值：FlakeRateMin 提到 0.6，flaky-t（0.5）不再满足。
	p2 := testPolicy(2)
	p2.FlakeRateMin = 0.6
	entries := s.Quarantine(p2)
	if len(entries) != 0 {
		t.Fatalf("after threshold raise, quarantine = %+v, want empty", entries)
	}
	evals := s.Evaluations()
	if len(evals) != 2 {
		t.Fatalf("evaluations = %d, want 2", len(evals))
	}
	if evals[0].PolicyVersion != 1 || evals[1].PolicyVersion != 2 {
		t.Fatalf("evaluation versions not traceable: %+v", evals)
	}
	if len(evals[0].Entries) != 1 || len(evals[1].Entries) != 0 {
		t.Fatalf("evaluation entries mismatch: %+v", evals)
	}
	// 每条记录都携带判定理由。
	if evals[0].Entries[0].Reason == "" {
		t.Fatal("quarantine entry must carry a reason")
	}
}

func TestGateDecision(t *testing.T) {
	s := buildPolicyStore(t)
	cfg := GateConfig{
		Window:           Window{},
		MinSamples:       3,
		BlockOnFlaky:     true,
		QuarantinePolicy: testPolicy(1),
	}
	// 隔离 flaky-t 后：只有 flaky-t 失败的运行应放行。
	s.Quarantine(testPolicy(1))
	if err := s.Ingest(mkBatch("run-flaky", 100, TestResult{TestID: "flaky-t", Outcome: OutcomeFailed})); err != nil {
		t.Fatal(err)
	}
	rep := s.Decide([]string{"run-flaky"}, cfg)
	if rep.Decision != GateAllow {
		t.Fatalf("quarantined-only failure should allow, got %s (%v)", rep.Decision, rep.Reasons)
	}
	if len(rep.QuarantinedHit) != 1 || rep.QuarantinedHit[0] != "flaky-t" {
		t.Fatalf("quarantined hit = %v", rep.QuarantinedHit)
	}
	// 持续失败用例失败 -> 阻断。
	if err := s.Ingest(mkBatch("run-broken", 101, TestResult{TestID: "broken-t", Outcome: OutcomeFailed})); err != nil {
		t.Fatal(err)
	}
	rep = s.Decide([]string{"run-broken"}, cfg)
	if rep.Decision != GateBlock {
		t.Fatalf("consistently failing test should block, got %s", rep.Decision)
	}
	// 未知批次 -> 阻断。
	rep = s.Decide([]string{"no-such-batch"}, cfg)
	if rep.Decision != GateBlock || len(rep.UnknownBatches) != 1 {
		t.Fatalf("unknown batch should block: %+v", rep)
	}
	// 可复现：相同输入相同配置 -> 相同结论。
	r1 := s.Decide([]string{"run-broken"}, cfg)
	r2 := s.Decide([]string{"run-broken"}, cfg)
	if fmt.Sprintf("%+v", r1) != fmt.Sprintf("%+v", r2) {
		t.Fatalf("gate decision not reproducible:\n%+v\n%+v", r1, r2)
	}
}
