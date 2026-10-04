// Package baseline compares current stability profiles against named
// baselines (e.g. a baseline branch) to detect newly flaky and recovered
// tests. Comparisons against different baselines coexist independently.
package baseline

import (
	"sort"
	"sync"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/flakiness"
)

// Diff is the comparison result of one test against one baseline.
type Diff struct {
	TestID          string            `json:"test_id"`
	BaselineVerdict flakiness.Verdict `json:"baseline_verdict"`
	CurrentVerdict  flakiness.Verdict `json:"current_verdict"`
	NewlyFlaky      bool              `json:"newly_flaky"`
	Recovered       bool              `json:"recovered"`
}

// Comparison is the full diff of current reports against one named baseline.
type Comparison struct {
	Baseline string `json:"baseline"`
	Diffs    []Diff `json:"diffs"`
}

// Registry stores named baseline snapshots and computes comparisons. Safe
// for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	baselines map[string]map[string]flakiness.Report
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{baselines: make(map[string]map[string]flakiness.Report)}
}

// SetBaseline records a baseline snapshot under a name (e.g. "main").
// The snapshot is a deep copy; later changes to reports do not affect it.
func (r *Registry) SetBaseline(name string, reports map[string]flakiness.Report) {
	snap := make(map[string]flakiness.Report, len(reports))
	for id, rep := range reports {
		snap[id] = rep
	}
	r.mu.Lock()
	r.baselines[name] = snap
	r.mu.Unlock()
}

// Compare diffs current reports against the named baseline. Results for
// different baselines are independent and never overwrite each other.
// The second return value is false when the baseline does not exist.
func (r *Registry) Compare(name string, current map[string]flakiness.Report) (Comparison, bool) {
	r.mu.RLock()
	base, ok := r.baselines[name]
	if !ok {
		r.mu.RUnlock()
		return Comparison{}, false
	}
	// Copy under lock so the diff is computed on a consistent snapshot.
	snap := make(map[string]flakiness.Report, len(base))
	for id, rep := range base {
		snap[id] = rep
	}
	r.mu.RUnlock()

	ids := make(map[string]struct{}, len(snap)+len(current))
	for id := range snap {
		ids[id] = struct{}{}
	}
	for id := range current {
		ids[id] = struct{}{}
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)

	comp := Comparison{Baseline: name, Diffs: make([]Diff, 0, len(sorted))}
	for _, id := range sorted {
		b, inBase := snap[id]
		c, inCur := current[id]
		d := Diff{TestID: id}
		if inBase {
			d.BaselineVerdict = b.Verdict
		}
		if inCur {
			d.CurrentVerdict = c.Verdict
		}
		// Newly flaky: not flaky at baseline (or absent), flaky now.
		d.NewlyFlaky = c.Verdict == flakiness.VerdictFlaky && (!inBase || b.Verdict != flakiness.VerdictFlaky)
		// Recovered: flaky at baseline, no longer flaky now.
		d.Recovered = inBase && b.Verdict == flakiness.VerdictFlaky && (!inCur || (c.Verdict != flakiness.VerdictFlaky && c.Verdict != flakiness.VerdictInsufficient))
		comp.Diffs = append(comp.Diffs, d)
	}
	return comp, true
}
