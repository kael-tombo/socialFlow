package imagegeneration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/openpost/backend/internal/ai"
	"github.com/openpost/backend/internal/services/aiusage"
	"github.com/openpost/backend/internal/services/workspaceaccess"
	"github.com/uptrace/bun"
)

// HandleJob advances a saved request. A lease fences import and accounting;
// submitting without a receipt is terminally ambiguous after interruption.
func (s *Service) HandleJob(ctx context.Context, payload string) error {
	var input struct {
		RunID string `json:"run_id"`
	}
	if json.Unmarshal([]byte(payload), &input) != nil || input.RunID == "" {
		return ErrInvalid
	}
	if !s.Available() {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var stored record
	if err := s.db.NewSelect().Model(&stored).Where("id = ?", input.RunID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if terminal(stored.State) && (stored.ProviderRequestID == "" || stored.BillingState != "pending") {
		return nil
	}
	now := time.Now().UTC()
	token := uuid.NewString()
	leaseUntil := now.Add(3 * time.Minute)
	claim, err := s.db.NewUpdate().Model((*record)(nil)).Set("lease_token = ?", token).Set("lease_expires_at = ?", leaseUntil).Where("id = ? AND (lease_expires_at IS NULL OR lease_expires_at < ?)", stored.ID, now).Exec(ctx)
	if err != nil {
		return err
	}
	count, err := claim.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrPending
	}
	stored.LeaseToken = token
	defer func() {
		finish, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_, _ = s.db.NewUpdate().Model((*record)(nil)).Set("lease_token = ''").Set("lease_expires_at = NULL").Where("id = ? AND lease_token = ?", stored.ID, token).Exec(finish)
	}()
	// Re-read after claiming, since another completed lease may race.
	if err = s.db.NewSelect().Model(&stored).Where("id = ? AND lease_token = ?", stored.ID, token).Scan(ctx); err != nil {
		return err
	}
	return s.advance(ctx, &stored)
}

func terminal(state string) bool {
	return state == "completed" || state == "failed" || state == "unknown"
}

func (s *Service) advance(ctx context.Context, stored *record) error {
	if terminal(stored.State) {
		return s.reconcileCost(ctx, stored)
	}
	var request ImageGenerationRequest
	var authority workspaceaccess.StoredAuthority
	if json.Unmarshal([]byte(stored.RequestJSON), &request) != nil || json.Unmarshal([]byte(stored.AuthorityJSON), &authority) != nil {
		return s.fail(ctx, stored, "The saved image request is invalid.")
	}
	if stored.State == "submitting" && stored.ProviderRequestID == "" {
		stored.State, stored.ErrorMessage, stored.BillingState = "unknown", "The provider may have accepted this request, but its receipt was not received. Check fal before starting another generation.", "unknown"
		return s.save(ctx, stored)
	}
	if stored.ProviderRequestID == "" {
		return s.submit(ctx, stored, request, authority)
	}
	status, err := s.provider.ImageStatus(ctx, stored.Endpoint, stored.ProviderRequestID)
	if err != nil {
		if time.Since(stored.CreatedAt) > 24*time.Hour {
			return s.fail(ctx, stored, "The provider result could not be recovered. The request receipt is retained for support.")
		}
		return ErrPending
	}
	if status.Failed {
		return s.fail(ctx, stored, "fal could not create this image. Review the prompt or try another image.")
	}
	if status.State != "completed" {
		stored.State = status.State
		if err = s.save(ctx, stored); err != nil {
			return err
		}
		return ErrPending
	}
	return s.importResult(ctx, stored, request, authority)
}

func (s *Service) submit(ctx context.Context, stored *record, request ImageGenerationRequest, authority workspaceaccess.StoredAuthority) error {
	decision, err := workspaceaccess.NewAuthorizer(s.db).AuthorizeStored(ctx, authority, workspaceaccess.LevelEdit)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return s.fail(ctx, stored, "Workspace access no longer allows image generation.")
	}
	input, err := s.providerInput(ctx, request)
	if err != nil {
		return s.fail(ctx, stored, "The source image or selection is no longer available or exceeds the image limits.")
	}
	stored.State = "submitting"
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := aiusage.RecordImageDispatch(ctx, tx, aiusage.EditorIdentity{WorkspaceID: stored.WorkspaceID, UserID: stored.UserID, ProjectID: stored.ProjectID, RunID: "image:" + stored.ID}, authority.OrganizationID, stored.Endpoint, request.Operation); err != nil {
			return err
		}
		return saveRecord(ctx, tx, stored)
	})
	if err != nil {
		return err
	}
	receipt, submitErr := s.provider.SubmitImage(ctx, input)
	// Persist receipts even if the worker is shutting down or the request timed out.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if submitErr != nil {
		if errors.Is(submitErr, ai.ErrImageRejected) {
			return s.fail(writeCtx, stored, "fal rejected this request. Review the prompt, image and provider configuration before trying again.")
		}
		stored.State, stored.ErrorMessage, stored.BillingState = "unknown", "The provider may have accepted this request, but its receipt was not received. Check fal before starting another generation.", "unknown"
		return s.save(writeCtx, stored)
	}
	stored.ProviderRequestID, stored.State = receipt, "queued"
	if err = s.save(writeCtx, stored); err != nil {
		return err
	}
	return ErrPending
}

func (s *Service) importResult(ctx context.Context, stored *record, request ImageGenerationRequest, authority workspaceaccess.StoredAuthority) error {
	// Keep paid success visible even when downloading or Media admission fails.
	stored.State = "saving"
	if err := s.save(ctx, stored); err != nil {
		return err
	}
	content, err := s.provider.ImageResult(ctx, stored.Endpoint, stored.ProviderRequestID)
	if err != nil {
		return s.deliveryFailure(ctx, stored)
	}
	content, err = outputPNG(content, request)
	if err != nil {
		return s.fail(ctx, stored, "The generated image could not be safely decoded.")
	}
	decision, err := workspaceaccess.NewAuthorizer(s.db).AuthorizeStored(ctx, authority, workspaceaccess.LevelEdit)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return s.fail(ctx, stored, "Workspace access changed before the image could be saved.")
	}
	media, err := s.importer.ImportGeneratedImage(ctx, MediaImport{WorkspaceID: stored.WorkspaceID, ParentMediaID: stored.SourceMediaID, ProjectID: request.ProjectID, PageID: request.PageID, RunID: stored.ID, Recipe: recipeJSON(request, stored.ProviderRequestID), Content: content})
	if err != nil {
		return s.deliveryFailure(ctx, stored)
	}
	stored.ResultMediaID, stored.State, stored.ErrorMessage = media.ID, "completed", ""
	if err = s.save(ctx, stored); err != nil {
		return err
	}
	return s.reconcileCost(ctx, stored)
}

func (s *Service) deliveryFailure(ctx context.Context, stored *record) error {
	if time.Since(stored.CreatedAt) > 24*time.Hour {
		return s.fail(ctx, stored, "The generated image could not be saved. The provider receipt and any reported cost are retained.")
	}
	stored.ErrorMessage = "The image is ready at fal. SocialFlow.ai is retrying the download or Media save without generating again."
	if err := s.save(ctx, stored); err != nil {
		return err
	}
	return ErrPending
}

func (s *Service) fail(ctx context.Context, stored *record, message string) error {
	stored.State, stored.ErrorMessage = "failed", message
	if stored.ProviderRequestID == "" {
		stored.BillingState = "not_charged"
	}
	if err := s.save(ctx, stored); err != nil {
		return err
	}
	return s.reconcileCost(ctx, stored)
}

func (s *Service) reconcileCost(ctx context.Context, stored *record) error {
	if stored.ProviderRequestID == "" || stored.BillingState != "pending" {
		return nil
	}
	cost, err := s.provider.ImageCost(ctx, stored.Endpoint, stored.ProviderRequestID)
	if err == nil && cost != nil {
		stored.CostMicrousd, stored.BillingState = cost, "reported"
		return s.save(ctx, stored)
	}
	if time.Since(stored.CreatedAt) > 72*time.Hour {
		stored.BillingState = "unknown"
		return s.save(ctx, stored)
	}
	return ErrBillingPending
}

func saveRecord(ctx context.Context, db bun.IDB, stored *record) error {
	stored.UpdatedAt = time.Now().UTC()
	result, err := db.NewUpdate().Model(stored).Column("state", "provider_request_id", "result_media_id", "billing_state", "cost_microusd", "error_message", "updated_at").Where("id = ? AND lease_token = ?", stored.ID, stored.LeaseToken).Exec(ctx)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return ErrConflict
	}
	return err
}

func (s *Service) save(ctx context.Context, stored *record) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := saveRecord(ctx, tx, stored); err != nil {
			return err
		}
		return aiusage.SetImageOutcome(ctx, tx, "image:"+stored.ID, stored.ProviderRequestID, stored.State, stored.CostMicrousd)
	})
}
