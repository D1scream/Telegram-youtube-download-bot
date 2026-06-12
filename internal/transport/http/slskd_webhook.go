package httptransport

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"telegram-bot/internal/usecases/music"
)

type DownloadWebhookService interface {
	HandleDownloadComplete(ctx context.Context, event music.DownloadCompleteEvent) error
}

type SlskdWebhookHandler struct {
	service DownloadWebhookService
	secret  string
	logger  *slog.Logger
}

func NewSlskdWebhookHandler(service DownloadWebhookService, secret string, logger *slog.Logger) *SlskdWebhookHandler {
	return &SlskdWebhookHandler{
		service: service,
		secret:  strings.TrimSpace(secret),
		logger:  logger.With("component", "slskd_webhook_handler"),
	}
}

type slskdWebhookPayload struct {
	Type           string           `json:"type"`
	LocalFilename  string           `json:"localFilename"`
	RemoteFilename string           `json:"remoteFilename"`
	Transfer       *slskdWebhookTransfer `json:"transfer"`
}

type slskdWebhookTransfer struct {
	Username  string `json:"username"`
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	State     string `json:"state"`
	Exception string `json:"exception"`
}

func (h *SlskdWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.secret != "" && r.Header.Get("X-Webhook-Secret") != h.secret {
		writeJSON(w, http.StatusUnauthorized, apiResponse{Error: "unauthorized"})
		return
	}

	var payload slskdWebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "некорректный JSON"})
		return
	}

	event := eventFromPayload(payload)
	state := transferState(payload.Transfer)
	h.logger.InfoContext(r.Context(), "Webhook slskd",
		"operation", "download_webhook",
		"type", payload.Type,
		"username", event.Username,
		"filename", event.Filename,
		"state", state,
	)

	if payload.Type != "DownloadFileComplete" {
		writeJSON(w, http.StatusOK, apiResponse{OK: true})
		return
	}

	if event.Username == "" || event.Filename == "" {
		h.logger.WarnContext(r.Context(), "Webhook DownloadFileComplete без нужных полей",
			"operation", "download_webhook",
			"payload_type", payload.Type,
			"state", state,
		)
		writeJSON(w, http.StatusOK, apiResponse{OK: true})
		return
	}

	if state != "" && !strings.Contains(state, "Succeeded") {
		h.logger.ErrorContext(r.Context(), "Скачивание не удалось",
			"operation", "download_webhook",
			"username", event.Username,
			"filename", event.Filename,
			"size", event.Size,
			"state", state,
			"exception", transferException(payload.Transfer),
		)
		writeJSON(w, http.StatusOK, apiResponse{OK: true})
		return
	}

	go func() {
		if err := h.service.HandleDownloadComplete(context.Background(), event); err != nil {
			h.logger.ErrorContext(context.Background(), "Ошибка обработки webhook скачивания",
				"operation", "download_webhook",
				"err", err,
			)
		}
	}()

	writeJSON(w, http.StatusOK, apiResponse{OK: true})
}

func eventFromPayload(payload slskdWebhookPayload) music.DownloadCompleteEvent {
	event := music.DownloadCompleteEvent{
		LocalFilename:  payload.LocalFilename,
		RemoteFilename: payload.RemoteFilename,
	}
	if payload.Transfer != nil {
		event.Username = payload.Transfer.Username
		event.Filename = payload.Transfer.Filename
		event.Size = payload.Transfer.Size
	}
	if event.Filename == "" {
		event.Filename = payload.RemoteFilename
	}
	return event
}

func transferState(transfer *slskdWebhookTransfer) string {
	if transfer == nil {
		return ""
	}
	return transfer.State
}

func transferException(transfer *slskdWebhookTransfer) string {
	if transfer == nil {
		return ""
	}
	return transfer.Exception
}
