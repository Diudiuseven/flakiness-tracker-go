// Package gate makes reproducible allow/block decisions for runs based on
// flakiness reports and the quarantine list.
package gate

import (
	"fmt"
	"sort"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/flakiness"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/model"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/quarantine"
)

// Outcome is the gate conclusion for a set of runs.
type Outcome string

const (
	OutcomeAllow Outcome = "allow"
	OutcomeBlock Outcome = "block"
)

// Failure explains one blocking or tolerated failure.
type Failure struct {
	TestID      string `json:"test_id"`
	BatchID     string `json:"batch_id"`
	Blocking    bool   `json:"blocking"`
	Explanation string `json:"explanation"`
}

// Decision is the deterministic gate result.
type Decision struct {
	Outcome  Outcome   `json:"outcome"`
	Failures []Failure `json:"failures"`
	Reason   string    `json:"reason"`
}

// Evaluate inspects the given batches: a failed test blocks the run unless it
// is currently quarantined (known intermittent). The decision is a pure
// function of (batches, reports, quarantine list): identical inputs and
// configuration always yield identical decisions.
func Evaluate(batches []model.Batch, reports map[string]flakiness.Report, q *quarantine.List) Decision {
	var failures []Failure
	for _, b := range batches {
		results := append([]model.CaseResult(nil), b.Results...)
		sort.Slice(results, func(i, j int) bool { return results[i].TestID < results[j].TestID })
		for _, r := range results {
			if r.Status != model.StatusFail {
				continue
			}
			f := Failure{TestID: r.TestID, BatchID: b.ID}
			rep, known := reports[r.TestID]
			switch {
			case q.Contains(r.TestID):
				f.Blocking = false
				f.Explanation = fmt.Sprintf("failed but quarantined as intermittent (rate %.2f)", rep.FailureRate)
			case known && rep.Verdict == flakiness.VerdictPersistent:
				f.Blocking = true
				f.Explanation = fmt.Sprintf("persistent failure (rate %.2f), not quarantinable", rep.FailureRate)
			case known:
				f.Blocking = true
				f.Explanation = fmt.Sprintf("failed with verdict %s and not quarantined", rep.Verdict)
			default:
				f.Blocking = true
				f.Explanation = "failed with no stability history and not quarantined"
			}
			failures = append(failures, f)
		}
	}

	d := Decision{Failures: failures}
	for _, f := range failures {
		if f.Blocking {
			d.Outcome = OutcomeBlock
			break
		}
	}
	if d.Outcome == OutcomeBlock {
		d.Reason = "one or more non-quarantined failures detected"
	} else {
		d.Outcome = OutcomeAllow
		if len(failures) > 0 {
			d.Reason = "all failures belong to quarantined intermittent tests"
		} else {
			d.Reason = "no failures in evaluated runs"
		}
	}
	return d
}
