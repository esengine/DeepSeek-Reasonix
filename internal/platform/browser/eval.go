package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const maxEvalResult = 64 << 10

// evalTimeout bounds one script: the evaluation awaits the script's promise, and
// a page can leave one pending forever, which would hold the tool call for good.
var evalTimeout = 30 * time.Second

const maxEvalScript = 256 << 10

// Evaluate runs JavaScript in the main world of a tab and returns its value.
func (s *Session) Evaluate(ctx context.Context, tabID, script, approvedOrigin string) (string, TabInfo, error) {
	t, err := s.tab(tabID)
	if err != nil {
		return "", TabInfo{}, err
	}
	if err := t.checkCurrentURL(); err != nil {
		return "", TabInfo{}, err
	}
	if strings.TrimSpace(script) == "" {
		return "", TabInfo{}, fail(CodeBadStep, "script is empty")
	}
	if len(script) > maxEvalScript {
		return "", TabInfo{}, fail(CodeBadStep, "script is %d bytes; maximum is %d", len(script), maxEvalScript)
	}
	t.mu.Lock()
	current := OriginOf(t.url)
	contextID := t.runtime.contexts[t.mainFrame]
	t.mu.Unlock()
	if current == "" || approvedOrigin == "" || current != approvedOrigin {
		return "", t.info(true), fail(CodeOriginChanged, "script was approved for %q but the tab is now at %q; approve a new call for this page", approvedOrigin, current)
	}
	if contextID == "" {
		return "", t.info(true), fail(CodeOriginChanged, "the approved page document is no longer available; read the page and approve a new call")
	}
	quoted, _ := json.Marshal(script)
	expression := fmt.Sprintf(`(async()=>{try{const v=await (0,eval)(%s);let s;try{s=JSON.stringify(v)}catch(_){s=String(v)}if(s===undefined)s=String(v);return s.length>%d?s.slice(0,%d)+"\n… result truncated":s}catch(e){throw new Error(String(e).slice(0,%d))}})()`, quoted, maxEvalResult, maxEvalResult, maxEvalResult)
	var result struct {
		Result struct {
			Type                string          `json:"type"`
			Description         string          `json:"description"`
			UnserializableValue string          `json:"unserializableValue"`
			Value               json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	callCtx, stop := context.WithTimeout(ctx, evalTimeout)
	defer stop()
	if err := t.call(callCtx, "Runtime.evaluate", map[string]any{
		"expression": expression, "awaitPromise": true, "returnByValue": true, "userGesture": true, "uniqueContextId": contextID,
	}, &result); err != nil {
		if callCtx.Err() != nil && ctx.Err() == nil {
			t.terminateScript()
			return "", t.info(true), fail(CodeScriptTimeout, "the script did not finish within %s; a promise it awaits may never settle. Read the page again, and if the work needs longer, run it in steps and poll for the result in a later call", evalTimeout)
		}
		t.mu.Lock()
		currentContext := t.runtime.contexts[t.mainFrame]
		t.mu.Unlock()
		if currentContext != contextID {
			return "", t.info(true), fail(CodeOriginChanged, "the approved page document changed before the script could run; read the page and approve a new call")
		}
		return "", TabInfo{}, engineFailure(err)
	}
	if result.ExceptionDetails.Text != "" || result.ExceptionDetails.Exception.Description != "" {
		detail := result.ExceptionDetails.Exception.Description
		if detail == "" {
			detail = result.ExceptionDetails.Text
		}
		return "", t.info(true), fail(CodeScriptFailed, "%s", clip(detail, maxEvalResult))
	}
	var decoded string
	decodedOK := json.Unmarshal(result.Result.Value, &decoded) == nil
	value := decoded
	if result.Result.UnserializableValue != "" {
		value = result.Result.UnserializableValue
	} else if !decodedOK {
		value = result.Result.Description
		if value == "" {
			value = result.Result.Type
		}
	}
	if len(value) > maxEvalResult {
		// The page caps the result in UTF-16 units, so a result of non-ASCII text
		// can still be longer than maxEvalResult bytes; cut it before the
		// character that crosses the limit, not inside it.
		cut := maxEvalResult
		for cut > 0 && !utf8.RuneStart(value[cut]) {
			cut--
		}
		value = value[:cut] + "\n… result truncated"
	}
	return value, t.info(true), nil
}

// terminateScript stops what the timed-out script is still running. It is best
// effort: the failure being reported does not depend on the page answering.
func (t *tab) terminateScript() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = t.call(ctx, "Runtime.terminateExecution", map[string]any{}, nil)
}
