package feedback

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/openpost/backend/internal/discordwebhook"
)

type DiscordDestination struct {
	webhookURL string
	client     *http.Client
}

type discordWebhookPayload struct {
	Username string         `json:"username,omitempty"`
	Embeds   []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

func (d *DiscordDestination) Deliver(ctx context.Context, report Report) error {
	payload := discordWebhookPayload{
		Username: "SocialFlow.ai feedback",
		Embeds: []discordEmbed{{
			Title:       strings.ToUpper(report.Category[:1]) + report.Category[1:] + " report",
			Description: report.Message,
			Fields: []discordEmbedField{
				{Name: "SocialFlow.ai version", Value: report.AppVersion, Inline: true},
				{Name: "User", Value: report.UserID, Inline: true},
				{Name: "Received", Value: report.CreatedAt, Inline: true},
			},
		}},
	}
	if report.Diagnostics != nil {
		summary, _ := json.Marshal(report.Diagnostics)
		value := string(summary)
		if len(value) > 1000 {
			value = value[:1000] + "…"
		}
		payload.Embeds[0].Fields = append(payload.Embeds[0].Fields, discordEmbedField{
			Name:  "Approved diagnostics",
			Value: "```json\n" + value + "\n```",
		})
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return errors.New("failed to encode feedback destination payload")
	}

	if report.Screenshot == nil {
		return discordwebhook.SendBody(ctx, d.client, d.webhookURL, bytes.NewReader(payloadJSON), "application/json")
	}

	screenshot, err := base64.StdEncoding.DecodeString(report.Screenshot.Data)
	if err != nil {
		return errors.New("invalid feedback screenshot")
	}
	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	if err := writer.WriteField("payload_json", string(payloadJSON)); err != nil {
		return errors.New("failed to encode feedback destination payload")
	}
	extension := "png"
	if report.Screenshot.MIMEType == "image/jpeg" {
		extension = "jpg"
	}
	part, err := writer.CreateFormFile("files[0]", "openpost-feedback."+extension)
	if err != nil {
		return errors.New("failed to encode feedback screenshot")
	}
	if _, err := part.Write(screenshot); err != nil {
		return errors.New("failed to encode feedback screenshot")
	}
	if err := writer.Close(); err != nil {
		return errors.New("failed to finish feedback payload")
	}
	// Multipart uploads keep their own body shape; the POST, bounded drain,
	// and status check still go through the shared sender.
	return discordwebhook.SendBody(ctx, d.client, d.webhookURL, &multipartBody, writer.FormDataContentType())
}
