// Package flakiness computes per-test stability verdicts over a configurable
// run window.
package flakiness

import (
	"fmt"
	"sort"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/model"
)

// Verdict is the stability classification of a test.
type Verdict string

const (
	VerdictStable       Verdict = "stable"
	VerdictFlaky        Verdict = "flaky"
	VerdictPersistent   Verdict = "persistent_failure"
	VerdictInsufficient Verdict = "insufficient_data"
)

// Config controls windowing and classification thresholds.
type Config struct {
	// WindowRuns limits analysis to the most recent N runs (0 = all retained).
	WindowRuns int
	// MinSamples is the minimum number of pass/fail samples required before a
	// deterministic verdict is given.
	MinSamples int
	// FlakyThreshold is the failure rate strictly above which a test counts
	// as flaky (any pass/fail transition also counts as flaky).
	FlakyThreshold float64
	// PersistentThreshold is the failure rate at/above which a test counts as
	// a persistent failure rather than flaky.
	PersistentThreshold float64
}

// DefaultConfig returns sane defaults.
func DefaultConfig() Config {
	return Config{WindowRuns: 0, MinSamples: 3, FlakyThreshold: 0.0, PersistentThreshold: 1.0}
}

// Report is the stability profile of one test over the analysis window.
type Report struct {
	TestID      string  `json:"test_id"`
	Pass        int     `json:"pass"`
	Fail        int     `json:"fail"`
	Skip        int     `json:"skip"`
	Samples     int     `json:"samples"`
	FailureRate float64 `json:"failure_rate"`
	Transitions int     `json:"transitions"`
	Verdict     Verdict `json:"verdict"`
	Reason      string  `json:"reason"`
}

// Analyze computes reports for every test observed in the given batches.
// Batches must be ordered by run index (oldest first); only the most recent
// cfg.WindowRuns batches are considered when WindowRuns > 0.
func Analyze(batches []model.Batch, cfg Config) map[string]Report {
	if cfg.WindowRuns > 0 && len(batches) > cfg.WindowRuns {
		batches = batches[len(batches)-cfg.WindowRuns:]
	}

	type acc struct {
		pass, fail, skip int
		transitions      int
		last             model.Status // last pass/fail status seen
	}
	stats := make(map[string]*acc)
	for _, b := range batches {
		// Deterministic per-batch iteration order.
		results := append([]model.CaseResult(nil), b.Results...)
		sort.Slice(results, func(i, j int) bool { return results[i].TestID < results[j].TestID })
		for _, r := range results {
			a := stats[r.TestID]
			if a == nil {
				a = &acc{}
				stats[r.TestID] = a
			}
			switch r.Status {
			case model.StatusPass:
				a.pass++
			case model.StatusFail:
				a.fail++
			case model.StatusSkip:
				a.skip++
				continue // skips do not affect transitions
			}
			if a.last != "" && a.last != r.Status {
				a.transitions++
			}
			a.last = r.Status
		}
	}

	reports := make(map[string]Report, len(stats))
	for id, a := range stats {
		samples := a.pass + a.fail
		rep := Report{
			TestID:      id,
			Pass:        a.pass,
			Fail:        a.fail,
			Skip:        a.skip,
			Samples:     samples,
			Transitions: a.transitions,
		}
		if samples > 0 {
			rep.FailureRate = float64(a.fail) / float64(samples)
		}
		rep.Verdict, rep.Reason = classify(rep, cfg)
		reports[id] = rep
	}
	return reports
}

func classify(rep Report, cfg Config) (Verdict, string) {
	if rep.Samples < cfg.MinSamples {
		return VerdictInsufficient, fmt.Sprintf("only %d pass/fail samples, need at least %d", rep.Samples, cfg.MinSamples)
	}
	if rep.FailureRate >= cfg.PersistentThreshold {
		return VerdictPersistent, fmt.Sprintf("failure rate %.2f >= persistent threshold %.2f", rep.FailureRate, cfg.PersistentThreshold)
	}
	if rep.FailureRate > cfg.FlakyThreshold || rep.Transitions > 0 {
		return VerdictFlaky, fmt.Sprintf("failure rate %.2f with %d pass/fail transitions", rep.FailureRate, rep.Transitions)
	}
	return VerdictStable, "no failures and no transitions in window"
}
