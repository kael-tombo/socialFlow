package handlers

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/openpost/backend/internal/services/engagement"
)

const (
	mcpToolInboxList    = "list_inbox"
	mcpToolInboxGet     = "get_inbox_item"
	mcpToolInboxRefresh = "refresh_inbox"
	mcpToolInboxState   = "set_inbox_state"
	mcpToolInboxRead    = "mark_inbox_read"
	mcpToolInboxAction  = "act_on_inbox_item"
)

func (h *MCPHandler) SetEngagementService(service *engagement.Service) { h.engagement = service }

func mcpInboxTools() []mcpOperationDefinition {
	text := func() map[string]any { return map[string]any{"type": "string"} }
	boolean := func() map[string]any { return map[string]any{"type": "boolean"} }
	filters := func() map[string]any {
		return map[string]any{"workspace_id": text(), "kind": map[string]any{"type": "string", "enum": []string{"comment", "mention", "review"}}, "platform": text(), "account_id": text(), "publication_id": text(), "archived": boolean()}
	}
	tool := func(name, title, description string, properties map[string]any, required []string, mode mcpOperationMode) mcpOperationDefinition {
		return mcpOperationDescriptor(map[string]any{"name": name, "title": title, "description": description,
			"inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}, mode, false, name == mcpToolInboxRefresh || name == mcpToolInboxAction)
	}
	list := filters()
	list["unread_only"], list["cursor"] = boolean(), text()
	list["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 100}
	return []mcpOperationDefinition{
		tool(mcpToolInboxList, "Read Inbox", "Read stored comments, incoming mentions and Google Business reviews. No provider calls. Continue with next_cursor. Sync health distinguishes comments, mentions and reviews.", list, []string{"workspace_id"}, mcpOperationQuery),
		tool(mcpToolInboxGet, "Read Inbox item", "Read one stored item and available parent context. Incoming activity can exist without an SocialFlow.ai post. Review owner replies include moderation state and durable operation receipts.", map[string]any{"workspace_id": text(), "item_id": text()}, []string{"workspace_id", "item_id"}, mcpOperationQuery),
		tool(mcpToolInboxRefresh, "Refresh Inbox", "Queue durable comment, mention and review collection for enabled accounts. Returns jobs; inspect stored sync health after jobs complete. Does not send replies.", map[string]any{"workspace_id": text()}, []string{"workspace_id"}, mcpOperationExecute),
		tool(mcpToolInboxState, "Update Inbox state", "Set shared Workspace read or archive state for the selected items.", map[string]any{"workspace_id": text(), "ids": map[string]any{"type": "array", "items": text(), "minItems": 1, "maxItems": 100}, "read": boolean(), "archived": boolean()}, []string{"workspace_id", "ids"}, mcpOperationExecute),
		tool(mcpToolInboxRead, "Mark matching Inbox items read", "Mark all matching unread inbound items read across every page, using the same filters as list_inbox.", filters(), []string{"workspace_id"}, mcpOperationExecute),
		tool(mcpToolInboxAction, "Act on Inbox item", "Queue a supported action on a stored item. Use can_* capabilities. For reviews, reply replaces the single owner reply, at most 4096 UTF-8 bytes; delete_reply removes only the owner reply. Submission acceptance does not mean moderation approval. Reuse client_operation_id only for the same intent; inspect the returned job before another review replacement.", map[string]any{"workspace_id": text(), "item_id": text(), "action": map[string]any{"type": "string", "enum": []string{"reply", "delete_reply", "hide", "delete", "like", "unlike"}}, "message": text(), "client_operation_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 128}}, []string{"workspace_id", "item_id", "action", "client_operation_id"}, mcpOperationExecute),
	}
}

type mcpInboxInput struct {
	WorkspaceID       string   `json:"workspace_id"`
	ItemID            string   `json:"item_id"`
	Kind              string   `json:"kind"`
	Platform          string   `json:"platform"`
	AccountID         string   `json:"account_id"`
	PublicationID     string   `json:"publication_id"`
	Archived          *bool    `json:"archived"`
	UnreadOnly        bool     `json:"unread_only"`
	Limit             int      `json:"limit"`
	Cursor            string   `json:"cursor"`
	IDs               []string `json:"ids"`
	Read              *bool    `json:"read"`
	Action            string   `json:"action"`
	Message           string   `json:"message"`
	ClientOperationID string   `json:"client_operation_id"`
}

func (h *MCPHandler) callInboxTool(ctx context.Context, userID, name string, args map[string]any) (any, *mcpError) {
	var input mcpInboxInput
	if err := decodeMCPArguments(args, &input); err != nil || strings.TrimSpace(input.WorkspaceID) == "" || input.Limit < 0 || input.Limit > 100 || (input.Kind != "" && input.Kind != "comment" && input.Kind != "mention" && input.Kind != "review") {
		return nil, &mcpError{Code: -32602, Message: "invalid Inbox arguments"}
	}
	if err := h.ensureWorkspaceAccess(ctx, userID, input.WorkspaceID); err != nil {
		return nil, err
	}
	if h.engagement == nil {
		return nil, &mcpError{Code: -32603, Message: "Inbox is unavailable"}
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	body, err := h.executeInboxTool(ctx, name, input)
	if err != nil {
		return nil, &mcpError{Code: -32602, Message: err.Error()}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, &mcpError{Code: -32603, Message: "could not encode Inbox result"}
	}
	return map[string]any{"content": []mcpContent{{Type: "text", Text: string(encoded)}}, "structuredContent": body}, nil
}

func (h *MCPHandler) executeInboxTool(ctx context.Context, name string, input mcpInboxInput) (any, error) {
	handler := NewEngagementMessagingHandler(h.auth, nil, h.engagement)
	archived := input.Archived != nil && *input.Archived
	switch name {
	case mcpToolInboxList:
		out, err := handler.listEngagement(ctx, &ListEngagementInput{WorkspaceID: input.WorkspaceID, Kind: input.Kind, Platform: input.Platform, AccountID: input.AccountID, PublicationID: input.PublicationID, Archived: archived, UnreadOnly: input.UnreadOnly, Limit: input.Limit, Cursor: input.Cursor})
		if err != nil {
			return nil, err
		}
		return out.Body, nil
	case mcpToolInboxGet:
		out, err := h.engagement.GetEngagement(ctx, engagementActor(ctx), input.WorkspaceID, input.ItemID)
		if err != nil {
			return nil, engagementHTTPError(err, "could not read Inbox item")
		}
		return out, nil
	case mcpToolInboxRefresh:
		request := &RefreshCapabilitiesInput{}
		request.Body.WorkspaceID = input.WorkspaceID
		out, err := handler.refreshEngagement(ctx, request)
		if err != nil {
			return nil, err
		}
		return out.Body, nil
	case mcpToolInboxState:
		if len(input.IDs) == 0 || len(input.IDs) > 100 {
			return nil, huma.Error400BadRequest("select 1 to 100 items")
		}
		request := &SetEngagementStateInput{}
		request.Body.WorkspaceID, request.Body.IDs, request.Body.Read, request.Body.Archived = input.WorkspaceID, input.IDs, input.Read, input.Archived
		_, err := handler.setEngagementState(ctx, request)
		return map[string]any{"updated": err == nil}, err
	case mcpToolInboxRead:
		request := &MarkEngagementReadInput{}
		request.Body.WorkspaceID, request.Body.Kind, request.Body.Platform, request.Body.AccountID, request.Body.PublicationID, request.Body.Archived = input.WorkspaceID, input.Kind, input.Platform, input.AccountID, input.PublicationID, archived
		out, err := handler.markEngagementRead(ctx, request)
		if err != nil {
			return nil, err
		}
		return out.Body, nil
	default:
		request := &EngagementActionInput{ItemID: input.ItemID}
		request.Body.WorkspaceID, request.Body.Action, request.Body.Message, request.Body.ClientOperationID = input.WorkspaceID, input.Action, input.Message, input.ClientOperationID
		out, err := handler.queueEngagementAction(ctx, request)
		if err != nil {
			return nil, err
		}
		return out.Body, nil
	}
}
