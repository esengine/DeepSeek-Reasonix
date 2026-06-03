package approval

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

type CardAction struct {
	Action     string `json:"action"`
	ApprovalID string `json:"approval_id"`
}

type pendingApproval struct {
	replyCh       chan approvalResult
	chatID        string
	ctrl          *control.Controller
	timeout       time.Duration
	cardMessageID string
}

type pendingAsk struct {
	replyCh       chan []event.AskAnswer
	chatID        string
	ctrl          *control.Controller
	timeout       time.Duration
	answers       map[string][]string
	total         int
	cardMessageID string
}

type approvalResult struct {
	allow   bool
	session bool
	persist bool
}

type Handler struct {
	mu          sync.Mutex
	pending     map[string]*pendingApproval
	pendingAsks map[string]*pendingAsk
	ch          channeltypes.Channel
	ctrl        *control.Controller
	chatID      string
}

func NewHandler(ch channeltypes.Channel) *Handler {
	return &Handler{
		ch:          ch,
		pending:     map[string]*pendingApproval{},
		pendingAsks: map[string]*pendingAsk{},
	}
}

func (h *Handler) HandleApproval(ctx context.Context, ctrl *control.Controller, chatID string, ev event.Event, timeout time.Duration) (string, error) {
	approvalID := ev.Approval.ID

	h.mu.Lock()
	replyCh := make(chan approvalResult, 1)
	h.pending[approvalID] = &pendingApproval{
		replyCh: replyCh,
		chatID:  chatID,
		ctrl:    ctrl,
		timeout: timeout,
	}
	h.mu.Unlock()

	card := buildApprovalCard(approvalID, ev.Approval.Tool, ev.Approval.Subject)
	result, err := h.ch.Send(ctx, &channeltypes.SendInput{
		ChatID: chatID,
		Card:   card,
	})
	if err != nil {
		h.mu.Lock()
		delete(h.pending, approvalID)
		h.mu.Unlock()
		return "", fmt.Errorf("send approval card: %w", err)
	}
	cardMsgID := result.MessageID

	h.mu.Lock()
	if pa, ok := h.pending[approvalID]; ok {
		pa.cardMessageID = cardMsgID
	}
	h.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-replyCh:
		ctrl.Approve(approvalID, result.allow, result.session, result.persist)
		h.updateCard(ctx, chatID, approvalID, result)
		return cardMsgID, nil
	case <-timer.C:
		h.mu.Lock()
		delete(h.pending, approvalID)
		h.mu.Unlock()
		ctrl.Approve(approvalID, false, false, false)
		h.updateCardWithMessage(ctx, chatID, "Timed out — denied", "yellow")
		return cardMsgID, nil
	case <-ctx.Done():
		h.mu.Lock()
		delete(h.pending, approvalID)
		h.mu.Unlock()
		return cardMsgID, ctx.Err()
	}
}

func (h *Handler) HandleAsk(ctx context.Context, ctrl *control.Controller, chatID string, ev event.Event, timeout time.Duration) (string, error) {
	askID := ev.Ask.ID
	questions := ev.Ask.Questions

	h.mu.Lock()
	replyCh := make(chan []event.AskAnswer, 1)
	h.pendingAsks[askID] = &pendingAsk{
		replyCh: replyCh,
		chatID:  chatID,
		ctrl:    ctrl,
		timeout: timeout,
		answers: map[string][]string{},
		total:   len(questions),
	}
	h.mu.Unlock()

	var cardMsgID string
	for _, q := range questions {
		card := buildAskCard(askID, q)
		result, err := h.ch.Send(ctx, &channeltypes.SendInput{
			ChatID: chatID,
			Card:   card,
		})
		if err != nil {
			h.mu.Lock()
			delete(h.pendingAsks, askID)
			h.mu.Unlock()
			return "", fmt.Errorf("send ask card: %w", err)
		}
		cardMsgID = result.MessageID
	}

	h.mu.Lock()
	if pa, ok := h.pendingAsks[askID]; ok {
		pa.cardMessageID = cardMsgID
	}
	h.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case answers := <-replyCh:
		ctrl.AnswerQuestion(askID, answers)
		return cardMsgID, nil
	case <-timer.C:
		h.mu.Lock()
		delete(h.pendingAsks, askID)
		h.mu.Unlock()
		return cardMsgID, nil
	case <-ctx.Done():
		h.mu.Lock()
		delete(h.pendingAsks, askID)
		h.mu.Unlock()
		return cardMsgID, ctx.Err()
	}
}

func (h *Handler) OnCardAction(ctx context.Context, ev *channeltypes.CardActionEvent) error {
	action, _ := ev.Action.Value["action"].(string)
	slog.Info("lark card action received", "action", action, "value", ev.Action.Value)

	if approvalID, _ := ev.Action.Value["approval_id"].(string); approvalID != "" {
		slog.Info("lark approval card action", "approval_id", approvalID, "action", action)
		h.mu.Lock()
		pa, ok := h.pending[approvalID]
		if !ok {
			h.mu.Unlock()
			slog.Warn("lark approval card action: no pending approval", "approval_id", approvalID)
			return nil
		}
		delete(h.pending, approvalID)
		h.mu.Unlock()

		var result approvalResult
		switch action {
		case "allow":
			result = approvalResult{allow: true, session: false, persist: false}
		case "deny":
			result = approvalResult{allow: false, session: false, persist: false}
		case "always_allow":
			result = approvalResult{allow: true, session: true, persist: true}
		default:
			return nil
		}
		select {
		case pa.replyCh <- result:
		default:
		}
		return nil
	}

	if askID, _ := ev.Action.Value["ask_id"].(string); askID != "" {
		questionID, _ := ev.Action.Value["question_id"].(string)
		if questionID == "" || action == "" {
			return nil
		}

		h.mu.Lock()
		pa, ok := h.pendingAsks[askID]
		if !ok {
			h.mu.Unlock()
			return nil
		}
		pa.answers[questionID] = append(pa.answers[questionID], action)
		answered := len(pa.answers)
		total := pa.total
		h.mu.Unlock()

		if answered >= total {
			h.mu.Lock()
			if pa, ok := h.pendingAsks[askID]; ok {
				delete(h.pendingAsks, askID)
				var answers []event.AskAnswer
				for qID, selected := range pa.answers {
					answers = append(answers, event.AskAnswer{QuestionID: qID, Selected: selected})
				}
				h.mu.Unlock()
				select {
				case pa.replyCh <- answers:
				default:
				}
				return nil
			}
			h.mu.Unlock()
		}
		return nil
	}

	return nil
}

func (h *Handler) updateCard(ctx context.Context, chatID, approvalID string, result approvalResult) {
	var msg string
	var template string
	if result.allow {
		if result.persist {
			msg = "Always allowed"
			template = "green"
		} else {
			msg = "Approved"
			template = "green"
		}
	} else {
		msg = "Denied"
		template = "red"
	}
	h.updateCardWithMessage(ctx, chatID, msg, template)
}

func (h *Handler) updateCardWithMessage(ctx context.Context, chatID, msg, template string) {
	card := fmt.Sprintf(`{"header":{"title":{"tag":"plain_text","content":"Tool Approval"},"template":"%s"},"elements":[{"tag":"div","text":{"tag":"plain_text","content":"%s"}}]}`, template, msg)
	_, _ = h.ch.Send(ctx, &channeltypes.SendInput{
		ChatID: chatID,
		Card:   card,
	})
}

func buildAskCard(askID string, q event.AskQuestion) string {
	var sb strings.Builder
	sb.WriteString(`{"header":{"title":{"tag":"plain_text","content":"`)
	sb.WriteString(escapeJSON(q.Header))
	sb.WriteString(`"},"template":"blue"},"elements":[`)
	sb.WriteString(`{"tag":"div","text":{"tag":"lark_md","content":"`)
	sb.WriteString(escapeJSON(q.Prompt))
	sb.WriteString(`"}},{"tag":"action","actions":[`)

	for _, opt := range q.Options {
		label := opt.Label
		if opt.Description != "" {
			label = label + " - " + opt.Description
		}
		sb.WriteString(`{"tag":"button","text":{"tag":"plain_text","content":"`)
		sb.WriteString(escapeJSON(label))
		sb.WriteString(`"},"type":"default","value":{"action":"`)
		sb.WriteString(escapeJSON(opt.Label))
		sb.WriteString(`","ask_id":"`)
		sb.WriteString(askID)
		sb.WriteString(`","question_id":"`)
		sb.WriteString(q.ID)
		sb.WriteString(`"}},`)
	}

	s := sb.String()
	s = s[:len(s)-1]
	return s + `]}]}`
}

func buildApprovalCard(approvalID, toolName, subject string) string {
	var sb strings.Builder
	sb.WriteString(`{"header":{"title":{"tag":"plain_text","content":"Tool Approval"},"template":"blue"},"elements":[`)
	sb.WriteString(`{"tag":"div","text":{"tag":"lark_md","content":"**`)
	sb.WriteString(escapeJSON(toolName))
	sb.WriteString(`**\n`)
	sb.WriteString(escapeJSON(truncateSubject(subject, 200)))
	sb.WriteString(`"}},{"tag":"action","actions":[`)
	sb.WriteString(`{"tag":"button","text":{"tag":"plain_text","content":"Allow"},"type":"primary","value":{"action":"allow","approval_id":"`)
	sb.WriteString(approvalID)
	sb.WriteString(`"}},`)
	sb.WriteString(`{"tag":"button","text":{"tag":"plain_text","content":"Deny"},"type":"danger","value":{"action":"deny","approval_id":"`)
	sb.WriteString(approvalID)
	sb.WriteString(`"}},`)
	sb.WriteString(`{"tag":"button","text":{"tag":"plain_text","content":"Always Allow"},"type":"default","value":{"action":"always_allow","approval_id":"`)
	sb.WriteString(approvalID)
	sb.WriteString(`"}}`)
	sb.WriteString(`]}]}`)
	return sb.String()
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func truncateSubject(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func (h *Handler) HasPending(approvalID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.pending[approvalID]
	return ok
}
