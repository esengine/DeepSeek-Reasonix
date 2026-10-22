package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"reasonix/internal/contract/tool"
)

const CodeArtifactSource = "artifact.invalid_source"

func artifactSourceURL(ctx context.Context, ref, pointer string) (*url.URL, error) {
	refuse := func() (*url.URL, error) {
		return nil, tool.Refusal{Code: CodeArtifactSource, Message: "source must name a session tool-result and pointer must select an exact HTTP(S) URL in its version-1 result"}
	}
	if !tool.IsResultReference(ref) || !strings.HasPrefix(pointer, "/") {
		return refuse()
	}
	f, err := tool.OpenResult(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(tool.ResultRetainLimit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > tool.ResultRetainLimit {
		return refuse()
	}
	var envelope struct {
		Version int             `json:"version"`
		Result  json.RawMessage `json:"result"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Version != 1 {
		return refuse()
	}
	var result struct {
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(envelope.Result, &result) != nil || result.IsError {
		return refuse()
	}
	selected, err := selectArtifactField(envelope.Result, pointer)
	if err != nil {
		return refuse()
	}
	var raw string
	if json.Unmarshal(selected, &raw) != nil || len(raw) > 8192 || strings.TrimSpace(raw) != raw {
		return refuse()
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(raw, "\r\n\t ") {
		return refuse()
	}
	return u, nil
}

func selectArtifactField(raw json.RawMessage, pointer string) (json.RawMessage, error) {
	for field := range strings.SplitSeq(strings.TrimPrefix(pointer, "/"), "/") {
		for i := 0; i < len(field); i++ {
			if field[i] == '~' {
				if i+1 == len(field) || (field[i+1] != '0' && field[i+1] != '1') {
					return nil, fmt.Errorf("invalid JSON pointer")
				}
				i++
			}
		}
		field = strings.ReplaceAll(strings.ReplaceAll(field, "~1", "/"), "~0", "~")
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) == nil && object != nil {
			raw = object[field]
		} else {
			var array []json.RawMessage
			i, err := strconv.Atoi(field)
			if err != nil || strconv.Itoa(i) != field || json.Unmarshal(raw, &array) != nil || i < 0 || i >= len(array) {
				return nil, fmt.Errorf("JSON pointer not found")
			}
			raw = array[i]
		}
		if len(raw) == 0 {
			return nil, fmt.Errorf("JSON pointer not found")
		}
	}
	return raw, nil
}
