package flakiness

// RejectReason 区分上报被拒绝的原因。
type RejectReason string

const (
	ReasonEmptyBatchID      RejectReason = "empty_batch_id"
	ReasonEmptyTestID       RejectReason = "empty_test_id"
	ReasonUnknownOutcome    RejectReason = "unknown_outcome"
	ReasonNonPositiveSeq    RejectReason = "non_positive_seq"
	ReasonConflictingResult RejectReason = "conflicting_result"
	ReasonEmptyResults      RejectReason = "empty_results"
)

// RejectError 表示一次被拒绝的上报，Reason 可用于程序化区分。
type RejectError struct {
	Reason  RejectReason
	Message string
}

func (e *RejectError) Error() string { return string(e.Reason) + ": " + e.Message }
