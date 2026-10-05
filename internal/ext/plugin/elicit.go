package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"reasonix/internal/contract/tool"
)

const (
	elicitMethod = "elicitation/create"
	// elicitAttempts bounds how often one form is put back in front of the
	// person with the reason the last answer was refused.
	elicitAttempts = 3
	elicitMaxField = 32
	// The text caps keep a server's words from crowding the host's own warning
	// off the card; past them the form is refused, not trimmed.
	elicitMaxMessage = 2000
	elicitMaxLabel   = 200
	elicitMaxChoices = 64
	maxExactInteger  = 1 << 53
	boolYes          = "Yes"
	boolNo           = "No"
)

// errBadElicitation is a form this client cannot render: a nested or unknown
// field type, or a mode it never declared.
var errBadElicitation = errors.New("unsupported elicitation request")

func elicitCapability() map[string]any {
	return map[string]any{"form": map[string]any{}, "url": map[string]any{}}
}

type elicitProp struct {
	name      string
	title     string
	desc      string
	kind      string // string, number, integer, boolean
	values    []string
	labels    []string
	multi     bool
	required  bool
	format    string
	min, max  *float64
	minLength *int
	maxLength *int
	defaults  []string // the schema's default, as the person would give it
}

// parseElicitation reads a request's params into the form it describes.
func parseElicitation(params json.RawMessage) (message string, props []elicitProp, err error) {
	var p struct {
		Mode            string          `json:"mode"`
		Message         string          `json:"message"`
		RequestedSchema json.RawMessage `json:"requestedSchema"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", nil, fmt.Errorf("%w: %w", errBadElicitation, err)
	}
	if p.Mode != "" && p.Mode != "form" {
		return "", nil, fmt.Errorf("%w: mode %q", errBadElicitation, p.Mode)
	}
	if utf8.RuneCountInString(p.Message) > elicitMaxMessage {
		return "", nil, fmt.Errorf("%w: message too long", errBadElicitation)
	}
	var schema struct {
		Type       string          `json:"type"`
		Properties json.RawMessage `json:"properties"`
		Required   []string        `json:"required"`
	}
	if len(p.RequestedSchema) > 0 {
		if err := json.Unmarshal(p.RequestedSchema, &schema); err != nil {
			return "", nil, fmt.Errorf("%w: %w", errBadElicitation, err)
		}
	}
	names, raws, err := orderedObject(schema.Properties)
	if err != nil || len(names) > elicitMaxField {
		return "", nil, fmt.Errorf("%w: properties", errBadElicitation)
	}
	for i, name := range names {
		prop, err := parseElicitProp(name, raws[i])
		if err != nil {
			return "", nil, err
		}
		prop.required = slices.Contains(schema.Required, name)
		props = append(props, prop)
	}
	return p.Message, props, nil
}

func parseElicitProp(name string, raw json.RawMessage) (elicitProp, error) {
	var s struct {
		Type        string          `json:"type"`
		Title       string          `json:"title"`
		Description string          `json:"description"`
		Format      string          `json:"format"`
		Enum        []string        `json:"enum"`
		EnumNames   []string        `json:"enumNames"`
		OneOf       []titled        `json:"oneOf"`
		Default     json.RawMessage `json:"default"`
		Minimum     *float64        `json:"minimum"`
		Maximum     *float64        `json:"maximum"`
		MinLength   *int            `json:"minLength"`
		MaxLength   *int            `json:"maxLength"`
		Items       *struct {
			Enum  []string `json:"enum"`
			AnyOf []titled `json:"anyOf"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return elicitProp{}, fmt.Errorf("%w: field %q", errBadElicitation, name)
	}
	p := elicitProp{name: name, title: s.Title, desc: s.Description, kind: s.Type, format: s.Format,
		min: s.Minimum, max: s.Maximum, minLength: s.MinLength, maxLength: s.MaxLength}
	switch s.Type {
	case "string":
		p.values, p.labels = choices(s.Enum, s.EnumNames, s.OneOf)
	case "number", "integer":
	case "boolean":
		p.values, p.labels = []string{"true", "false"}, []string{boolYes, boolNo}
	case "array":
		if s.Items == nil {
			return elicitProp{}, fmt.Errorf("%w: field %q has no items", errBadElicitation, name)
		}
		p.kind, p.multi = "string", true
		p.values, p.labels = choices(s.Items.Enum, nil, s.Items.AnyOf)
		if len(p.values) == 0 {
			return elicitProp{}, fmt.Errorf("%w: field %q offers no choices", errBadElicitation, name)
		}
	default:
		return elicitProp{}, fmt.Errorf("%w: field %q has type %q", errBadElicitation, name, s.Type)
	}
	if !labelsFit(append([]string{name, s.Title}, p.labels...)) || utf8.RuneCountInString(s.Description) > elicitMaxLabel*4 ||
		len(p.labels) > elicitMaxChoices || len(slices.Compact(slices.Sorted(slices.Values(p.labels)))) != len(p.labels) {
		return elicitProp{}, fmt.Errorf("%w: field %q has choices or labels this form cannot show", errBadElicitation, name)
	}
	p.defaults = p.given(s.Default)
	return p, nil
}

type titled struct {
	Const string `json:"const"`
	Title string `json:"title"`
}

func choices(enum, names []string, titledOptions []titled) (values, labels []string) {
	if len(titledOptions) > 0 {
		for _, o := range titledOptions {
			values = append(values, o.Const)
			labels = append(labels, cmp(o.Title, o.Const))
		}
		return values, labels
	}
	for i, v := range enum {
		values = append(values, v)
		label := v
		if i < len(names) && names[i] != "" {
			label = names[i]
		}
		labels = append(labels, label)
	}
	return values, labels
}

// given renders a schema default the way the person would give it: a choice by
// its label, a number or text as typed. A default the field cannot hold is none.
func (p elicitProp) given(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var values []string
	switch x := v.(type) {
	case string:
		values = []string{x}
	case bool:
		values = []string{strconv.FormatBool(x)}
	case float64:
		return []string{strconv.FormatFloat(x, 'f', -1, 64)}
	case []any:
		for _, item := range x {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
	}
	if len(p.values) == 0 {
		return values
	}
	var labels []string
	for _, v := range values {
		if i := slices.Index(p.values, v); i >= 0 {
			labels = append(labels, p.labels[i])
		}
	}
	return labels
}

func labelsFit(labels []string) bool {
	for _, l := range labels {
		if utf8.RuneCountInString(l) > elicitMaxLabel {
			return false
		}
	}
	return true
}

func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// orderedObject returns an object's keys in the order the server wrote them,
// which is the order the person should meet the fields in.
func orderedObject(raw json.RawMessage) ([]string, []json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, nil, errBadElicitation
	}
	var names []string
	var values []json.RawMessage
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		name, _ := tok.(string)
		if slices.Contains(names, name) {
			return nil, nil, errBadElicitation
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, nil, err
		}
		names = append(names, name)
		values = append(values, v)
	}
	return names, values, nil
}

// elicit answers one elicitation/create with the MCP result it calls for.
// Nobody to ask, or a refusal, is "decline"; a stopped turn is "cancel".
func elicit(ctx context.Context, e tool.Elicitor, server string, params json.RawMessage) (map[string]any, error) {
	var mode struct {
		Mode string `json:"mode"`
	}
	if json.Unmarshal(params, &mode) == nil && mode.Mode == "url" {
		return elicitURL(ctx, e, server, params)
	}
	message, props, err := parseElicitation(params)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return map[string]any{"action": "decline"}, nil
	}
	req := tool.ElicitRequest{Source: server, Message: message}
	for _, p := range props {
		f := tool.ElicitField{Name: p.name, Title: cmp(p.title, p.name), Description: p.desc, Choices: p.labels, Multi: p.multi, Default: p.defaults}
		if f.Description == "" {
			f.Description = f.Title
		}
		req.Fields = append(req.Fields, f)
	}
	for range elicitAttempts {
		reply, err := e.Elicit(ctx, req)
		if err != nil {
			// A stopped turn or a prompt that timed out: nobody answered.
			return map[string]any{"action": "cancel"}, nil
		}
		if reply.Declined {
			return map[string]any{"action": "decline"}, nil
		}
		content, problems := elicitContent(props, reply.Values)
		if len(problems) == 0 {
			return map[string]any{"action": "accept", "content": content}, nil
		}
		req.Note = strings.Join(problems, "; ")
		req.Fields = slices.Clone(req.Fields)
		for i := range req.Fields {
			if given, ok := reply.Values[req.Fields[i].Name]; ok {
				req.Fields[i].Default = given
			}
		}
	}
	return map[string]any{"action": "cancel"}, nil
}

// elicitContent turns what the person gave into the typed values the schema
// asks for, and names every field that does not satisfy it.
func elicitContent(props []elicitProp, given map[string][]string) (map[string]any, []string) {
	content := map[string]any{}
	var problems []string
	for _, p := range props {
		raw := given[p.name]
		if len(raw) == 0 || (len(raw) == 1 && strings.TrimSpace(raw[0]) == "") {
			if p.required {
				problems = append(problems, fmt.Sprintf("%q is required", cmp(p.title, p.name)))
			}
			continue
		}
		v, err := p.value(raw)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%q: %s", cmp(p.title, p.name), err))
			continue
		}
		content[p.name] = v
	}
	return content, problems
}

func (p elicitProp) value(raw []string) (any, error) {
	if len(p.values) > 0 {
		picked := make([]string, 0, len(raw))
		for _, r := range raw {
			i := slices.Index(p.labels, r)
			if i < 0 {
				return nil, fmt.Errorf("%q is not one of the choices", r)
			}
			picked = append(picked, p.values[i])
		}
		switch {
		case p.multi:
			return picked, nil
		case p.kind == "boolean":
			return picked[0] == "true", nil
		}
		return picked[0], nil
	}
	s := strings.TrimSpace(raw[0])
	switch p.kind {
	case "integer":
		// Parsed exactly: through a float64, 2^53+1 arrives as a different number.
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v > maxExactInteger || v < -maxExactInteger {
			return nil, errors.New("not a whole number this form can carry")
		}
		if (p.min != nil && float64(v) < *p.min) || (p.max != nil && float64(v) > *p.max) {
			return nil, errors.New("out of range")
		}
		return v, nil
	case "number":
		n, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			return nil, errors.New("not a number")
		}
		if (p.min != nil && n < *p.min) || (p.max != nil && n > *p.max) {
			return nil, errors.New("out of range")
		}
		return n, nil
	}
	n := utf8.RuneCountInString(s)
	if (p.minLength != nil && n < *p.minLength) || (p.maxLength != nil && n > *p.maxLength) {
		return nil, errors.New("wrong length")
	}
	if !formatHolds(p.format, s) {
		return nil, fmt.Errorf("not a valid %s", p.format)
	}
	return s, nil
}

func formatHolds(format, s string) bool {
	switch format {
	case "email":
		a, err := mail.ParseAddress(s)
		return err == nil && a.Address == s
	case "uri":
		u, err := url.Parse(s)
		return err == nil && u.Scheme != ""
	case "date":
		_, err := time.Parse(time.DateOnly, s)
		return err == nil
	case "date-time":
		_, err := time.Parse(time.RFC3339, s)
		return err == nil
	}
	return true
}

// elicitRouter holds the tool calls in flight on a connection whose server
// requests arrive unattached to any call (stdio): a form goes to the most
// recent one, whose person is the one watching, and ends with that call. One
// form is open at a time; a server asking again meanwhile is declined.
type elicitRouter struct {
	mu    sync.Mutex
	calls []*elicitCall
	open  atomic.Bool
}

type elicitCall struct{ ctx context.Context }

func (r *elicitRouter) register(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	entry := &elicitCall{ctx: ctx}
	r.mu.Lock()
	r.calls = append(r.calls, entry)
	r.mu.Unlock()
	return func() {
		cancel()
		r.mu.Lock()
		r.calls = slices.DeleteFunc(r.calls, func(c *elicitCall) bool { return c == entry })
		r.mu.Unlock()
	}
}

// claim returns the most recent call with a person behind it, and release once
// its form is answered. No such call, or a form already open, leaves no
// elicitor: a refusal, answered at once.
func (r *elicitRouter) claim() (context.Context, tool.Elicitor, func()) {
	if !r.open.CompareAndSwap(false, true) {
		return context.Background(), nil, func() {}
	}
	release := func() { r.open.Store(false) }
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, call := range slices.Backward(r.calls) {
		if e, ok := tool.ElicitorFrom(call.ctx); ok {
			return call.ctx, e, release
		}
	}
	return context.Background(), nil, release
}

type elicitTransport interface {
	registerElicitCall(ctx context.Context) func()
}

// elicitationReply is the JSON-RPC response to one elicitation/create.
func elicitationReply(ctx context.Context, e tool.Elicitor, server string, id json.RawMessage, params json.RawMessage) any {
	type response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  any             `json:"result,omitempty"`
		Error   *rpcError       `json:"error,omitempty"`
	}
	out := response{JSONRPC: "2.0", ID: append(json.RawMessage(nil), id...)}
	result, err := elicit(ctx, e, server, params)
	switch {
	case errors.Is(err, errBadElicitation):
		out.Error = &rpcError{Code: -32602, Message: err.Error()}
	case err != nil:
		out.Error = &rpcError{Code: -32603, Message: "elicitation failed"}
	default:
		out.Result = result
	}
	return out
}
