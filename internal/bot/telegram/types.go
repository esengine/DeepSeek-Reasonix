// Package telegram implements the Telegram Bot API adapter.
package telegram

import (
	"net/http"
	"time"
)

// Config configures a Telegram adapter.
type Config struct {
	ConnectionID string
	Token        string
	APIBaseURL   string
	HTTPClient   HTTPDoer
}

// HTTPDoer permits injecting an HTTP client in tests.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type apiResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description"`
	ErrorCode   int    `json:"error_code"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

type user struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type message struct {
	MessageID       int64    `json:"message_id"`
	From            *user    `json:"from"`
	Chat            chat     `json:"chat"`
	Text            string   `json:"text"`
	ReplyToMessage  *message `json:"reply_to_message"`
	MessageThreadID int64    `json:"message_thread_id"`
}

type update struct {
	UpdateID      int64    `json:"update_id"`
	Message       *message `json:"message"`
	EditedMessage *message `json:"edited_message"`
}

type apiError struct {
	code        int
	description string
	retryAfter  time.Duration
}

func (e *apiError) Error() string {
	if e.description != "" {
		return "telegram API error: " + e.description
	}
	return "telegram API error"
}
