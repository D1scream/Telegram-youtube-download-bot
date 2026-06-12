package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"telegram-bot/internal/usecases/messaging"
)

type mockMessageSender struct {
	err    error
	chatID int64
	text   string
}

func (m *mockMessageSender) SendToChat(_ context.Context, chatID int64, message string) error {
	m.chatID = chatID
	m.text = message
	return m.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSendHandler(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		senderErr      error
		expectedStatus int
		expectedOK     bool
		expectedChatID int64
		expectedText   string
	}{
		{
			name:           "успешная отправка",
			body:           `{"chat_id":-100,"text":"Привет"}`,
			expectedStatus: http.StatusOK,
			expectedOK:     true,
			expectedChatID: -100,
			expectedText:   "Привет",
		},
		{
			name:           "пустой chat_id",
			body:           `{"text":"Привет"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "пустой text",
			body:           `{"chat_id":-100,"text":"   "}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "ошибка Telegram",
			body:           `{"chat_id":-100,"text":"Привет"}`,
			senderErr:      errors.New("telegram error"),
			expectedStatus: http.StatusBadGateway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &mockMessageSender{err: tt.senderErr}
			service := messaging.NewSendMessageService(sender, testLogger())
			handler := NewSendHandler(service, testLogger())
			req := httptest.NewRequest(http.MethodPost, "/send", bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Fatalf("Ожидался статус %d, получено %d", tt.expectedStatus, rec.Code)
			}

			var resp apiResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("не удалось разобрать ответ: %v", err)
			}

			if resp.OK != tt.expectedOK {
				t.Errorf("Ожидался ok=%v, получено %v", tt.expectedOK, resp.OK)
			}

			if tt.expectedText != "" && sender.text != tt.expectedText {
				t.Errorf("Ожидался текст %q, получено %q", tt.expectedText, sender.text)
			}
			if tt.expectedChatID != 0 && sender.chatID != tt.expectedChatID {
				t.Errorf("Ожидался chat_id %d, получено %d", tt.expectedChatID, sender.chatID)
			}
		})
	}
}
