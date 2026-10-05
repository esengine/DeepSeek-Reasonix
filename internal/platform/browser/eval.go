package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const maxEvalResult = 64 << 10
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
	if err := t.call(ctx, "Runtime.evaluate", map[string]any{
		"expression": expression, "awaitPromise": true, "returnByValue": true, "userGesture": true, "uniqueContextId": contextID,
	}, &result); err != nil {
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
		value = value[:maxEvalResult] + "\n… result truncated"
	}
	return value, t.info(true), nil
}
