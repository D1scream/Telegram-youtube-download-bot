package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"telegram-bot/internal/usecases/messaging"
)

type MessageService interface {
	SendTo(ctx context.Context, chatID int64, text string) error
}

type SendHandler struct {
	service MessageService
	logger  *slog.Logger
}

func NewSendHandler(service MessageService, logger *slog.Logger) *SendHandler {
	return &SendHandler{
		service: service,
		logger:  logger.With("component", "send_handler"),
	}
}

type sendRequest struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}

func (h *SendHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResponse{Error: "некорректный JSON"})
		return
	}

	if err := h.service.SendTo(r.Context(), req.ChatID, req.Text); err != nil {
		switch {
		case errors.Is(err, messaging.ErrEmptyMessage), errors.Is(err, messaging.ErrInvalidChatID):
			writeJSON(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
			return
		}

		h.logger.ErrorContext(r.Context(), "Не удалось отправить сообщение", "operation", "send", "err", err)
		writeJSON(w, http.StatusBadGateway, apiResponse{Error: "не удалось отправить сообщение"})
		return
	}

	writeJSON(w, http.StatusOK, apiResponse{OK: true})
}
