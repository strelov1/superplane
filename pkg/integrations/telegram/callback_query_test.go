package telegram

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/superplanehq/superplane/pkg/core"
	"github.com/superplanehq/superplane/test/support/contexts"
)

// A click on a question nobody waits for any more must still be answered,
// or Telegram leaves the button spinning and the click looks lost.
func Test__HandleCallbackQuery__StaleClickIsAnswered(t *testing.T) {
	var calls []string
	withDefaultTransport(t, func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		calls = append(calls, req.URL.Path+" "+string(body))
		return jsonResponse(http.StatusOK, `{"ok": true, "result": true}`), nil
	})

	rec := httptest.NewRecorder()
	(&Telegram{}).handleCallbackQuery(core.HTTPRequestContext{
		Logger:      logrus.NewEntry(logrus.New()),
		Response:    rec,
		Integration: &contexts.IntegrationContext{Configuration: map[string]any{"botToken": "test-token"}},
	}, Update{CallbackQuery: &CallbackQuery{
		ID:      "q1",
		Data:    "yes",
		Message: &CallbackMessage{TelegramMessage: TelegramMessage{MessageID: 7, Chat: Chat{ID: 42}}},
	}})

	assert.Equal(t, http.StatusOK, rec.Code)
	if assert.Len(t, calls, 2) {
		assert.Contains(t, calls[0], "/answerCallbackQuery")
		assert.Contains(t, calls[0], `"text":"This question is no longer waiting for an answer"`)
		assert.Contains(t, calls[1], "/editMessageReplyMarkup")
		assert.Contains(t, calls[1], `"message_id":7`)
	}
}

func Test__CallbackMessage__ButtonLabel(t *testing.T) {
	m := &CallbackMessage{}
	assert.Equal(t, "yes", m.buttonLabel("yes"))

	m.ReplyMarkup = &struct {
		InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
	}{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "Да", CallbackData: "yes"}, {Text: "Нет", CallbackData: "no"}}}}
	assert.Equal(t, "Нет", m.buttonLabel("no"))
}
