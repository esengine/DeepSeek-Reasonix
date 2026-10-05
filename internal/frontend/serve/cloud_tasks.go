package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"unicode/utf8"
)

const (
	cloudHistoryLimit = 24
	cloudTextBudget   = 28 << 10
)

var (
	ErrCloudTaskNotFound = errors.New("remote task not found")
	ErrCloudTaskReadOnly = errors.New("remote task is read-only")
	ErrCloudInputRefused = errors.New("remote input is not allowed")
)

type CloudTask struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Running  bool   `json:"running"`
	ReadOnly bool   `json:"readOnly,omitempty"`
}

type CloudMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Reasoning string `json:"reasoning,omitempty"`
	Images    int    `json:"images,omitempty"`
	ModelRef  string `json:"modelRef,omitempty"`
}

type CloudTaskSnapshot struct {
	Task     CloudTask      `json:"task"`
	Messages []CloudMessage `json:"messages"`
}

// CloudTasks is the deliberately small task surface exported to an
// account-authenticated cloud controller. SSH-backed panes stay on their own
// host and are never chained through this machine.
func (h *Hub) CloudTasks(context.Context) (any, error) {
	runtimes := h.Runtimes()
	tasks := make([]CloudTask, 0, len(runtimes))
	for _, rt := range runtimes {
		if !rt.Local() || rt.Server == nil {
			continue
		}
		view := rt.view()
		tasks = append(tasks, CloudTask{
			ID: view.ID, Name: view.Name, ReadOnly: view.ReadOnly,
			Running: rt.Server.Controller().RuntimeStatus().Running,
		})
	}
	return tasks, nil
}

func (h *Hub) CloudTask(_ context.Context, id string) (any, error) {
	rt := h.Get(strings.TrimSpace(id))
	if rt == nil || !rt.Local() || rt.Server == nil {
		return nil, ErrCloudTaskNotFound
	}
	view := rt.view()
	snapshot := CloudTaskSnapshot{Task: CloudTask{
		ID: view.ID, Name: view.Name, ReadOnly: view.ReadOnly,
		Running: rt.Server.Controller().RuntimeStatus().Running,
	}}
	history := historyMessages(rt.Server.Controller().History())
	if len(history) > cloudHistoryLimit {
		history = history[len(history)-cloudHistoryLimit:]
	}
	budget := cloudTextBudget
	for _, message := range history {
		content := cloudBoundText(message.Content, &budget)
		reasoning := cloudBoundText(message.Reasoning, &budget)
		snapshot.Messages = append(snapshot.Messages, CloudMessage{
			Role: message.Role, Content: content, Reasoning: reasoning,
			Images: message.Images, ModelRef: message.ModelRef,
		})
		if budget == 0 {
			break
		}
	}
	return snapshot, nil
}

func cloudBoundText(value string, budget *int) string {
	if *budget <= 0 {
		return ""
	}
	bytes := []byte(value)
	if len(bytes) <= *budget {
		*budget -= len(bytes)
		return value
	}
	bytes = bytes[:*budget]
	for len(bytes) > 0 && !utf8.Valid(bytes) {
		bytes = bytes[:len(bytes)-1]
	}
	*budget = 0
	return string(bytes) + "…"
}

func (h *Hub) CloudSubmit(ctx context.Context, id, input, deviceID string, ordinal int) error {
	rt := h.Get(strings.TrimSpace(id))
	if rt == nil || !rt.Local() || rt.Server == nil {
		return ErrCloudTaskNotFound
	}
	if !rt.writable() {
		return ErrCloudTaskReadOnly
	}
	input = strings.TrimSpace(input)
	if input == "" || len(input) > 32<<10 || strings.HasPrefix(input, "!") || strings.HasPrefix(input, "/") {
		return ErrCloudInputRefused
	}
	body, err := json.Marshal(map[string]string{"input": input})
	if err != nil {
		return err
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/submit", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withDeviceReach(req.Context(), deviceID, ordinal))
	rec := httptest.NewRecorder()
	rt.Server.submit(rec, req)
	if rec.Code >= 200 && rec.Code < 300 {
		return nil
	}
	var response struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &response)
	if response.Error.Message != "" {
		return errors.New(response.Error.Message)
	}
	return errors.New(http.StatusText(rec.Code))
}
