package server

import (
	"log/slog"
	"net/http"

	httptransport "telegram-bot/internal/transport/http"
)

type Dependencies struct {
	SendMessage  httptransport.MessageService
	SlskdWebhook http.Handler
	Logger       *slog.Logger
}

func New(deps Dependencies) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /send", httptransport.NewSendHandler(deps.SendMessage, deps.Logger))
	if deps.SlskdWebhook != nil {
		mux.Handle("POST /webhooks/slskd", deps.SlskdWebhook)
	}
	return mux
}
