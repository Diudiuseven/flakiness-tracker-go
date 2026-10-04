// Package quarantine decides which flaky tests are isolated, based on
// configurable policy, and keeps an audit trail of decisions.
package quarantine

import (
	"fmt"
	"sort"
	"sync"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/flakiness"
)

// Policy controls which tests may be quarantined.
type Policy struct {
	// MinSamples is the minimum pass/fail sample count required before a test
	// may be quarantined.
	MinSamples int `json:"min_samples"`
	// MaxFailureRate: tests failing at or above this rate are persistent
	// failures and must NOT be quarantined.
	MaxFailureRate float64 `json:"max_failure_rate"`
	// Version identifies the policy for audit traceability.
	Version string `json:"version"`
}

// Decision records the outcome of evaluating one test under one policy.
type Decision struct {
	TestID        string `json:"test_id"`
	Quarantined   bool   `json:"quarantined"`
	PolicyVersion string `json:"policy_version"`
	Reason        string `json:"reason"`
}

// List is the current quarantine set with its audit log. Safe for concurrent
// use.
type List struct {
	mu      sync.RWMutex
	members map[string]struct{}
	audit   []Decision
}

// NewList creates an empty quarantine list.
func NewList() *List {
	return &List{members: make(map[string]struct{})}
}

// Evaluate re-evaluates all reports under the given policy, replacing the
// current set and appending audit entries. Only intermittently flaky tests
// are quarantined; persistent failures and insufficient-data tests are
// excluded. Decisions are returned in deterministic (sorted) order.
func (l *List) Evaluate(reports map[string]flakiness.Report, p Policy) []Decision {
	ids := make([]string, 0, len(reports))
	for id := range reports {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	decisions := make([]Decision, 0, len(ids))
	members := make(map[string]struct{})
	for _, id := range ids {
		rep := reports[id]
		d := Decision{TestID: id, PolicyVersion: p.Version}
		switch {
		case rep.Samples < p.MinSamples:
			d.Reason = fmt.Sprintf("insufficient samples: %d < %d", rep.Samples, p.MinSamples)
		case rep.FailureRate >= p.MaxFailureRate:
			d.Reason = fmt.Sprintf("persistent failure (rate %.2f >= %.2f) must not be quarantined", rep.FailureRate, p.MaxFailureRate)
		case rep.Verdict == flakiness.VerdictFlaky:
			d.Quarantined = true
			d.Reason = fmt.Sprintf("intermittent failure (rate %.2f, %d transitions)", rep.FailureRate, rep.Transitions)
		default:
			d.Reason = fmt.Sprintf("verdict %s does not qualify", rep.Verdict)
		}
		if d.Quarantined {
			members[id] = struct{}{}
		}
		decisions = append(decisions, d)
	}

	l.mu.Lock()
	l.members = members
	l.audit = append(l.audit, decisions...)
	l.mu.Unlock()
	return decisions
}

// Contains reports whether a test is currently quarantined.
func (l *List) Contains(testID string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, ok := l.members[testID]
	return ok
}

// Members returns the current quarantined test IDs (sorted).
func (l *List) Members() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]string, 0, len(l.members))
	for id := range l.members {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Audit returns the full decision history, oldest first.
func (l *List) Audit() []Decision {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]Decision(nil), l.audit...)
}
