package providerwrite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openpost/backend/internal/models"
	"github.com/openpost/backend/internal/platform"
	"github.com/uptrace/bun"
)

const (
	StatusPrepared        = "prepared"
	StatusSending         = "sending"
	StatusAccepted        = "accepted"
	StatusDefiniteFailure = "definite_failure"
	StatusAmbiguous       = "ambiguous"

	DeliveryQueued            = "queued"
	DeliverySubmitted         = "submitted"
	DeliveryProcessing        = "processing"
	DeliveryAwaitingUser      = "awaiting_user"
	DeliveryProviderScheduled = "provider_scheduled"
	DeliveryLive              = "live"
	DeliveryRejected          = "rejected"
	DeliveryAmbiguous         = "ambiguous"
	DeliveryManualResolution  = "manual_resolution"

	RecoveryNone             = "none"
	RecoveryRetry            = "retry"
	RecoveryReconcile        = "reconcile"
	RecoveryManualResolution = "manual_resolution"
)

func DeliveryRecoveryAction(delivery models.ProviderDelivery) string {
	switch delivery.State {
	case DeliveryRejected:
		if delivery.RetrySafety == string(platform.PublishRetrySafe) ||
			delivery.RetrySafety == string(platform.PublishRetryIdempotent) {
			return RecoveryRetry
		}
	case DeliveryProcessing, DeliveryAwaitingUser, DeliveryAmbiguous:
		if delivery.RetrySafety == string(platform.PublishRetryReconcileOnly) {
			return RecoveryReconcile
		}
	case DeliveryManualResolution:
		return RecoveryManualResolution
	}
	return RecoveryNone
}

var (
	ErrOutcomeAmbiguous      = errors.New("provider write outcome is ambiguous; SocialFlow.ai will not replay it")
	ErrOutcomePending        = errors.New("provider write is pending provider reconciliation")
	ErrReconciliationExpired = errors.New("provider reconciliation expired; check the provider before publishing again")
	ErrWriteInProgress       = errors.New("provider write is already in progress")
	ErrFenceNotEntered       = errors.New("provider adapter returned without entering the durable write fence")
	ErrOperationChanged      = errors.New("provider write operation fingerprint or ownership changed")
)

type Input struct {
	OperationID        string
	JobID              string
	AuthorizationID    string
	WorkspaceID        string
	PublicationID      string
	RenditionID        string
	SocialAccountID    string
	TargetKey          string
	Provider           string
	Operation          string
	PayloadFingerprint string
}

type SendFunc func(context.Context, *Control) (platform.PublishResult, error)
type ReconcileFunc func(context.Context, string) (platform.PublishResult, error)
type ResumeFunc func(context.Context, *Control, string) (platform.PublishResult, error)

type OutcomeError struct {
	ProviderState string
	Kind          string
	RetryAfter    time.Duration
	Retryable     bool
	Err           error
}

func (e *OutcomeError) Error() string {
	if e == nil || e.Err == nil {
		return "provider write failed"
	}
	return e.Err.Error()
}

func (e *OutcomeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func IsAmbiguous(err error) bool {
	var outcome *OutcomeError
	return errors.As(err, &outcome) && outcome.Kind == StatusAmbiguous
}

func IsPending(err error) (time.Duration, bool) {
	var outcome *OutcomeError
	if !errors.As(err, &outcome) || outcome.Kind != string(platform.PublishSubmissionPending) {
		return 0, false
	}
	return outcome.RetryAfter, true
}

func IsRetryable(err error) (time.Duration, bool) {
	var outcome *OutcomeError
	if !errors.As(err, &outcome) || !outcome.Retryable {
		return 0, false
	}
	return outcome.RetryAfter, true
}

func Fingerprint(domain string, payload any) (string, error) {
	encoded, err := json.Marshal(struct {
		Domain  string `json:"domain"`
		Payload any    `json:"payload"`
	}{Domain: strings.TrimSpace(domain), Payload: payload})
	if err != nil {
		return "", fmt.Errorf("encode provider write fingerprint: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

type jobExecutionContextKey struct{}

type JobExecution struct {
	ID              string
	Attempt         int
	LockedAt        time.Time
	LeaseGeneration int64
}

func WithJobExecution(ctx context.Context, jobID string, attempt int, lockedAt time.Time, generation ...int64) context.Context {
	var leaseGeneration int64
	if len(generation) > 0 {
		leaseGeneration = generation[0]
	}
	return context.WithValue(ctx, jobExecutionContextKey{}, JobExecution{
		ID: strings.TrimSpace(jobID), Attempt: attempt, LockedAt: lockedAt.UTC(), LeaseGeneration: leaseGeneration,
	})
}

func JobExecutionFromContext(ctx context.Context) (JobExecution, bool) {
	execution, ok := ctx.Value(jobExecutionContextKey{}).(JobExecution)
	return execution, ok && execution.ID != ""
}

func operationIdempotencyKey(operationID string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(operationID)))
	return "pw_" + hex.EncodeToString(digest[:16])
}

// RequireCurrentJobExecution locks a worker's current lease when called in a
// transaction. The acquisition generation survives heartbeats and failure-count
// resets. Synchronous callers without a job retain their existing fence.
func RequireCurrentJobExecution(ctx context.Context, db bun.IDB, jobID string) error {
	execution, ok := JobExecutionFromContext(ctx)
	if !ok {
		return nil
	}
	if jobID != "" && jobID != execution.ID {
		return ErrWriteInProgress
	}
	query := db.NewUpdate().Model((*models.Job)(nil)).Set("id=id").Where("id=? AND status='processing'", execution.ID)
	if execution.LeaseGeneration > 0 {
		query = query.Where("lease_generation=?", execution.LeaseGeneration)
	} else {
		// Compatibility for synchronous callers and older saved execution contexts.
		query = query.Where("attempts=? AND locked_at=?", execution.Attempt, execution.LockedAt)
	}
	result, err := query.Exec(ctx)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrWriteInProgress
	}
	return nil
}
