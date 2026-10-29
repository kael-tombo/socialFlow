package imagegeneration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/openpost/backend/internal/jobregistry"
	"github.com/openpost/backend/internal/services/aiusage"
	"github.com/uptrace/bun"
)

// MarkTerminalJobFailure shares the queue's transaction so a failed job cannot
// leave the studio permanently active. Paid receipts retain cost recovery.
func (s *Service) MarkTerminalJobFailure(ctx context.Context, db bun.IDB, payload string) error {
	var input struct {
		RunID string `json:"run_id"`
	}
	if json.Unmarshal([]byte(payload), &input) != nil || input.RunID == "" {
		return nil
	}
	var stored record
	if err := db.NewSelect().Model(&stored).Where("id = ?", input.RunID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if !terminal(stored.State) {
		if stored.ProviderRequestID == "" && stored.State == "submitting" {
			stored.State, stored.BillingState = "unknown", "unknown"
		} else {
			stored.State = "failed"
			if stored.ProviderRequestID == "" {
				stored.BillingState = "not_charged"
			}
		}
		stored.ErrorMessage = "SocialFlow.ai could not finish this request. Any provider receipt and reported cost have been retained."
	}
	if stored.ProviderRequestID != "" && stored.BillingState == "pending" {
		if time.Since(stored.CreatedAt) >= 72*time.Hour {
			stored.BillingState = "unknown"
		} else {
			job, err := jobregistry.NewJob(jobregistry.TypeImageGeneration, payload, time.Now().UTC().Add(5*time.Minute))
			if err != nil {
				return err
			}
			if _, err = db.NewInsert().Model(job).Exec(ctx); err != nil {
				return err
			}
		}
	}
	stored.UpdatedAt = time.Now().UTC()
	_, err := db.NewUpdate().Model(&stored).Column("state", "billing_state", "error_message", "updated_at").Set("lease_token = ''").Set("lease_expires_at = NULL").WherePK().Exec(ctx)
	if err != nil {
		return err
	}
	return aiusage.SetImageOutcome(ctx, db, "image:"+stored.ID, stored.ProviderRequestID, stored.State, stored.CostMicrousd)
}
