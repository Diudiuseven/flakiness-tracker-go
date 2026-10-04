// Package model defines the core domain types for the flakiness tracker.
package model

import (
	"errors"
	"fmt"
	"time"
)

// Status is the outcome of a single test case execution.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// Valid reports whether the status is a known value.
func (s Status) Valid() bool {
	switch s {
	case StatusPass, StatusFail, StatusSkip:
		return true
	}
	return false
}

// RejectReason classifies why a report was rejected, so callers can
// distinguish malformed payloads from conflicting duplicates.
type RejectReason string

const (
	ReasonMissingBatchID   RejectReason = "missing_batch_id"
	ReasonMissingTestID    RejectReason = "missing_test_id"
	ReasonInvalidStatus    RejectReason = "invalid_status"
	ReasonMissingSource    RejectReason = "missing_source"
	ReasonNegativeIndex    RejectReason = "negative_run_index"
	ReasonConflictingEntry RejectReason = "conflicting_duplicate"
)

// RejectError is a typed validation/conflict error with a stable reason code.
type RejectError struct {
	Reason  RejectReason
	Message string
}

func (e *RejectError) Error() string {
	return fmt.Sprintf("%s: %s", e.Reason, e.Message)
}

// IsReject reports whether err is a *RejectError and returns its reason.
func IsReject(err error) (RejectReason, bool) {
	var re *RejectError
	if errors.As(err, &re) {
		return re.Reason, true
	}
	return "", false
}

// CaseResult is one test outcome belonging to exactly one run batch.
type CaseResult struct {
	TestID  string `json:"test_id"`
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Batch is a single run (e.g. one CI job) reported as an atomic unit.
// Index is the reporter-provided run ordering (e.g. build number); it makes
// statistics independent of arrival order.
type Batch struct {
	ID        string       `json:"batch_id"`
	Source    string       `json:"source"`
	Branch    string       `json:"branch,omitempty"`
	Index     int64        `json:"run_index"`
	StartedAt time.Time    `json:"started_at"`
	Results   []CaseResult `json:"results"`
}

// Validate checks a single result for completeness/legality.
func (r CaseResult) Validate() error {
	if r.TestID == "" {
		return &RejectError{Reason: ReasonMissingTestID, Message: "test_id is required"}
	}
	if !r.Status.Valid() {
		return &RejectError{Reason: ReasonInvalidStatus, Message: fmt.Sprintf("unknown status %q", r.Status)}
	}
	return nil
}

// Validate checks a batch for completeness/legality (results included).
func (b Batch) Validate() error {
	if b.ID == "" {
		return &RejectError{Reason: ReasonMissingBatchID, Message: "batch_id is required"}
	}
	if b.Source == "" {
		return &RejectError{Reason: ReasonMissingSource, Message: "source is required"}
	}
	if b.Index < 0 {
		return &RejectError{Reason: ReasonNegativeIndex, Message: "run_index must be >= 0"}
	}
	for i, r := range b.Results {
		if err := r.Validate(); err != nil {
			return fmt.Errorf("result[%d]: %w", i, err)
		}
	}
	return nil
}
