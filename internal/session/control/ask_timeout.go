package control

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/base/i18n"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
)

// AskTimeoutSettings is the user's ask-timeout setting: how long an `ask`
// question waits before the host skips it. Seconds 0 means it waits.
type AskTimeoutSettings struct {
	Seconds int `json:"seconds"`
	// Invalid carries the code of a value the file holds that cannot be used;
	// Seconds then reads 0 because nothing is being applied.
	Invalid        string `json:"invalid,omitempty"`
	DefaultSeconds int    `json:"defaultSeconds"`
	MinSeconds     int    `json:"minSeconds"`
	MaxSeconds     int    `json:"maxSeconds"`
	Path           string `json:"path"`
}

func (c *Controller) AskTimeoutSettings() AskTimeoutSettings {
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	out := AskTimeoutSettings{
		Seconds:        cfg.Agent.AskTimeoutSeconds.Seconds,
		DefaultSeconds: config.DefaultAskTimeoutSeconds,
		MinSeconds:     config.MinAskTimeoutSeconds,
		MaxSeconds:     config.MaxAskTimeoutSeconds,
		Path:           path,
	}
	if config.ValidateAskTimeoutSetting(cfg.Agent.AskTimeoutSeconds) != nil {
		out.Seconds, out.Invalid = 0, "ask_timeout.out_of_range"
	}
	return out
}

// SaveAskTimeoutSettings persists the timeout. Each ask reads it as it begins
// to wait, so a question already open keeps the wait it started with.
func (c *Controller) SaveAskTimeoutSettings(seconds int) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	path := config.UserConfigPath()
	cfg := config.LoadForEdit(path)
	if err := cfg.SetAskTimeoutSeconds(seconds); err != nil {
		return err
	}
	return cfg.SaveTo(path)
}

// askOutcomeTimedOut is the receipt outcome of a question the host skipped
// because nobody answered within the user's ask-timeout setting.
const askOutcomeTimedOut = "timed_out"

// askSkipDelay is how long this question may go unanswered before the host
// skips it: zero for an external party's form, which is theirs to wait on, and
// for a setting of off or one that cannot be used (which is said once, loudly).
func (c *Controller) askSkipDelay(origin *event.AskOrigin) time.Duration {
	if origin != nil {
		return 0
	}
	after, err := config.LoadForEdit(config.UserConfigPath()).AskTimeoutChecked()
	if err != nil {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Code: event.NoticeCodeAskTimeoutInvalid,
			Text:   "ask_timeout_seconds in the user config is not valid, so questions wait as long as it takes until it is fixed",
			Detail: err.Error()})
		return 0
	}
	return max(after, 0)
}

// askWaitContext bounds an ask's wait by the user's ask timeout, on top of the
// approval timeout. The deadline's cause is the AskTimedOutError the asker
// returns, so a skip is told apart from a cancel or a headless deadline by
// identity.
func (c *Controller) askWaitContext(ctx context.Context, origin *event.AskOrigin) (context.Context, context.Context, context.CancelFunc) {
	return c.askWaitContextAfter(ctx, c.askSkipDelay(origin))
}

func (c *Controller) askWaitContextAfter(ctx context.Context, after time.Duration) (context.Context, context.Context, context.CancelFunc) {
	waitCtx, cancel := c.approval.waitContext(ctx)
	if after <= 0 {
		return waitCtx, waitCtx, cancel
	}
	skipCtx, cancelSkip := context.WithTimeoutCause(waitCtx, after, &agent.AskTimedOutError{After: after})
	return waitCtx, skipCtx, func() { cancelSkip(); cancel() }
}

var errAskEngaged = errors.New("the user took over this question")

// EngageQuestion disables automatic skipping for this pending question only.
// It neither supplies an answer nor changes any approval or future question.
func (c *Controller) EngageQuestion(id string) bool {
	c.approval.mu.Lock()
	p, ok := c.approval.asks[id]
	if !ok || p.queued || p.origin != nil {
		c.approval.mu.Unlock()
		return false
	}
	first := false
	select {
	case <-p.engaged:
	default:
		close(p.engaged)
		first = true
	}
	c.approval.mu.Unlock()
	if first {
		// Every window showing the question is told it will not be skipped; one
		// holding only the first announcement would count down to nothing.
		c.sink.Emit(event.Event{Kind: event.AskEngaged, Ask: event.Ask{ID: id}})
	}
	return true
}

func (c *Controller) waitForAsk(ctx context.Context, id string, reply <-chan []event.AskAnswer, after time.Duration) ([]event.AskAnswer, error) {
	base, waitCtx, cancel := c.askWaitContextAfter(ctx, after)
	defer cancel()
	c.approval.mu.Lock()
	engaged := c.approval.asks[id].engaged
	c.approval.mu.Unlock()
	for {
		select {
		case ans := <-reply:
			c.closeBarrier(id, barrierResolved)
			return ans, nil
		case <-engaged:
			waitCtx, engaged = base, nil
		case <-waitCtx.Done():
			ans, err := c.endUnansweredAsk(waitCtx, id, reply)
			if errors.Is(err, errAskEngaged) {
				waitCtx, engaged = base, nil
				continue
			}
			return ans, err
		}
	}
}

func (a *approvalManager) resolveAskForTimeout(id string) (pendingAsk, bool, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.asks[id]
	if ok {
		select {
		case <-p.engaged:
			return p, true, true
		default:
		}
	}
	delete(a.asks, id)
	return p, ok, false
}

// endUnansweredAsk settles an ask whose wait ended without an answer. A skip
// by the ask timeout lets an answer that landed as the timer fired win;
// otherwise the card is settled on every frontend by a receipt naming the
// timeout, and the asker is told it was one. Any other end is a cancellation.
func (c *Controller) endUnansweredAsk(waitCtx context.Context, id string, reply <-chan []event.AskAnswer) ([]event.AskAnswer, error) {
	timedOut, skipped := errors.AsType[*agent.AskTimedOutError](context.Cause(waitCtx))
	if !skipped {
		select {
		case ans := <-reply:
			c.closeBarrier(id, barrierResolved)
			return ans, nil
		default:
		}
		c.approval.cancelAsk(id)
		c.closeBarrier(id, barrierCancelled)
		return nil, waitCtx.Err()
	}
	pending, open, engaged := c.approval.resolveAskForTimeout(id)
	if engaged {
		return nil, errAskEngaged
	}
	if !open {
		select {
		case ans := <-reply:
			c.closeBarrier(id, barrierResolved)
			return ans, nil
		case <-time.After(time.Second):
		}
	}
	c.closeBarrier(id, barrierTimedOut)
	if open {
		c.recordAskDecisionReceipt(id, pending, nil, askOutcomeTimedOut, nil)
	}
	return nil, timedOut
}

// askTimeoutNotice is /ask-timeout: bare, it reports the setting; with
// <seconds> or off it changes it, through the same save the settings page uses.
func (c *Controller) askTimeoutNotice(fields []string) {
	if len(fields) == 2 {
		seconds := 0
		if arg := strings.ToLower(fields[1]); arg != "off" {
			n, err := strconv.Atoi(arg)
			if err != nil {
				c.notice(fmt.Sprintf(i18n.M.AskTimeoutBadFmt, fields[1]) + "\n" + c.askTimeoutStatus())
				return
			}
			seconds = n
		}
		if err := c.SaveAskTimeoutSettings(seconds); err != nil {
			c.notice("ask-timeout: " + err.Error())
			return
		}
	}
	c.notice(c.askTimeoutStatus())
}

func (c *Controller) askTimeoutStatus() string {
	state := i18n.M.AskTimeoutOff
	if n := c.AskTimeoutSettings().Seconds; n > 0 {
		state = fmt.Sprintf("%d s", n)
	}
	return fmt.Sprintf(i18n.M.AskTimeoutFmt, state)
}

func askTimeoutArgItems(prior []string) []SlashItem {
	if len(prior) != 1 {
		return nil
	}
	return []SlashItem{
		{Label: "off", Insert: "off", Hint: i18n.M.ArgAskTimeoutOff},
		{Label: "300", Insert: "300", Hint: i18n.M.ArgAskTimeout300},
	}
}
