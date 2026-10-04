// Package service ties ingestion, analysis, quarantine, baselines, gating and
// retention together behind one facade.
package service

import (
	"fmt"

	"github.com/Diudiuseven/flakiness-tracker-go/internal/baseline"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/flakiness"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/gate"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/model"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/quarantine"
	"github.com/Diudiuseven/flakiness-tracker-go/internal/store"
)

// Config aggregates all tunables.
type Config struct {
	Flakiness  flakiness.Config
	Policy     quarantine.Policy
	RetainRuns int // batches kept by Prune; 0 = unlimited
}

// Service is the application facade.
type Service struct {
	cfg   Config
	store *store.Store
	quar  *quarantine.List
	base  *baseline.Registry
}

// New creates a Service with the given config.
func New(cfg Config) *Service {
	return &Service{
		cfg:   cfg,
		store: store.New(),
		quar:  quarantine.NewList(),
		base:  baseline.NewRegistry(),
	}
}

// Ingest stores a batch (idempotent, atomic).
func (s *Service) Ingest(b model.Batch) error {
	return s.store.Ingest(b)
}

// Reports returns current flakiness reports over the configured window.
func (s *Service) Reports() map[string]flakiness.Report {
	return flakiness.Analyze(s.store.Snapshot().Batches, s.cfg.Flakiness)
}

// ReevaluateQuarantine re-applies the (possibly updated) policy to current
// reports and returns the decisions made. Every re-evaluation is appended to
// the audit log with the policy version, so threshold changes are traceable.
func (s *Service) ReevaluateQuarantine(p quarantine.Policy) []quarantine.Decision {
	s.cfg.Policy = p
	return s.quar.Evaluate(s.Reports(), p)
}

// QuarantineMembers returns the current quarantine set.
func (s *Service) QuarantineMembers() []string {
	return s.quar.Members()
}

// QuarantineAudit returns the quarantine decision history.
func (s *Service) QuarantineAudit() []quarantine.Decision {
	return s.quar.Audit()
}

// SetBaseline snapshots current reports under a baseline name.
func (s *Service) SetBaseline(name string) {
	s.base.SetBaseline(name, s.Reports())
}

// CompareBaseline diffs current reports against a named baseline.
func (s *Service) CompareBaseline(name string) (baseline.Comparison, bool) {
	return s.base.Compare(name, s.Reports())
}

// Gate evaluates the given batches against current reports and quarantine.
func (s *Service) Gate(batchIDs []string) (gate.Decision, error) {
	batches, missing := s.store.Snapshot().Find(batchIDs)
	if len(missing) > 0 {
		return gate.Decision{}, fmt.Errorf("unknown batch ids: %v", missing)
	}
	return gate.Evaluate(batches, s.Reports(), s.quar), nil
}

// Prune enforces the retention limit and returns the number of removed
// batches. Statistics over the remaining window are recomputed from the
// remaining batches, so they stay consistent.
func (s *Service) Prune() int {
	return s.store.Prune(s.cfg.RetainRuns)
}
