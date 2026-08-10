// Command reasonix-plugin-example is a reference Reasonix plugin: a minimal MCP stdio
// server speaking newline-delimited JSON-RPC 2.0 on stdin/stdout. It exists to
// document the contract end-to-end (the protocol the internal/plugin client
// drives) and to give users a working example to copy.
//
// Wire it up in reasonix.toml:
//
//	[[plugins]]
//	name    = "example"
//	command = "reasonix-plugin-example"
//
// Then reasonix surfaces its tools as "mcp__example__echo" / "mcp__example__wordcount",
// its prompt as the "/mcp__example__review" slash command, and its resource as
// the "@example:doc://style-guide" reference.
//
// Protocol, one JSON object per line:
//   - initialize                 → {protocolVersion, capabilities, serverInfo}
//   - notifications/initialized  (notification, no id) → ignored
//   - tools/list                 → {tools: [{name, description, inputSchema, annotations}]}
//   - tools/call {name, arguments} → {content: [{type:"text", text}], isError}
//   - prompts/list               → {prompts: [{name, description, arguments}]}
//   - prompts/get {name, arguments} → {messages: [{role, content:{type,text}}]}
//   - resources/list             → {resources: [{uri, name, description, mimeType}]}
//   - resources/read {uri}       → {contents: [{uri, mimeType, text}]}
//
// Logs go to stderr (reasonix forwards plugin stderr to the terminal); stdout is
// reserved for JSON-RPC so it must never carry stray prose.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"unicode/utf8"
)

// version is overridable via -ldflags "-X main.version=...". Reported in
// initialize's serverInfo so reasonix (and humans) can see which build is running.
var version = "dev"

func main() {
	log.SetPrefix("reasonix-plugin-example: ")
	log.SetFlags(0)
	if err := serve(os.Stdin, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// JSON-RPC framing

type request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"` // nil ⇒ notification (no reply); echoed back verbatim otherwise
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	protocolVersion    = "2024-11-05"
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// serve runs the read-dispatch-reply loop until stdin closes (reasonix closed the
// pipe / is shutting down). Each line is one JSON-RPC message.
func serve(in *os.File, out *os.File) error {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	defer w.Flush()

	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if rerr := handleLine(line, w); rerr != nil {
				return rerr
			}
			if ferr := w.Flush(); ferr != nil {
				return ferr
			}
		}
		if err != nil {
			return nil // EOF or pipe closed: clean shutdown
		}
	}
}

func handleLine(line []byte, w *bufio.Writer) error {
	line = trimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		log.Printf("skipping unparseable line: %v", err)
		return nil
	}
	if req.ID == nil {
		return nil // notification (e.g. notifications/initialized): no reply
	}

	resp := response{JSONRPC: "2.0", ID: *req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"prompts":   map[string]any{},
				"resources": map[string]any{},
			},
			"serverInfo": map[string]any{"name": "reasonix-plugin-example", "version": version},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolList()}
	case "tools/call":
		resp.Result, resp.Error = callTool(req.Params)
	case "prompts/list":
		resp.Result = map[string]any{"prompts": promptList()}
	case "prompts/get":
		resp.Result, resp.Error = getPrompt(req.Params)
	case "resources/list":
		resp.Result = map[string]any{"resources": resourceList()}
	case "resources/read":
		resp.Result, resp.Error = readResource(req.Params)
	default:
		resp.Error = &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// tools

// toolDef is one exposed tool: its advertised metadata plus the handler. run
// returns the text result, or an error which becomes an isError tool result the
// model can read and adapt to.
type toolDef struct {
	name        string
	description string
	schema      map[string]any
	readOnly    bool
	run         func(args map[string]any) (string, error)
}

// tools is the registry. Both demo tools are read-only and declare it via the
// readOnlyHint annotation, so reasonix runs them in parallel batches and (with the
// permission layer) auto-allows them without prompting.
var tools = []toolDef{
	{
		name:        "echo",
		description: "Echo the given text back. The simplest possible proof the plugin round-trip works.",
		readOnly:    true,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string", "description": "Text to echo back"},
			},
			"required": []string{"text"},
		},
		run: func(args map[string]any) (string, error) {
			text, ok := args["text"].(string)
			if !ok {
				return "", fmt.Errorf("argument 'text' must be a string")
			}
			return text, nil
		},
	},
	{
		name:        "wordcount",
		description: "Count the lines, words, and bytes of the given text (like wc).",
		readOnly:    true,
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string", "description": "Text to measure"},
			},
			"required": []string{"text"},
		},
		run: func(args map[string]any) (string, error) {
			text, ok := args["text"].(string)
			if !ok {
				return "", fmt.Errorf("argument 'text' must be a string")
			}
			return fmt.Sprintf("lines: %d, words: %d, bytes: %d, runes: %d",
				countLines(text), len(strings.Fields(text)), len(text), utf8.RuneCountInString(text)), nil
		},
	},
}

func toolList() []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":        t.name,
			"description": t.description,
			"inputSchema": t.schema,
			"annotations": map[string]any{
				"readOnlyHint": t.readOnly,
				"title":        t.name,
			},
		})
	}
	return out
}

// callTool dispatches a tools/call. A handler error is reported as an isError
// content result (in-band, so the model sees it) rather than a JSON-RPC error;
// JSON-RPC errors are reserved for protocol-level faults (bad params, unknown
// tool).
func callTool(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	for _, t := range tools {
		if t.name != p.Name {
			continue
		}
		text, err := t.run(p.Arguments)
		if err != nil {
			return textResult(err.Error(), true), nil
		}
		return textResult(text, false), nil
	}
	return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
}

func textResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// prompts

// promptList advertises the server's prompts. They surface in reasonix as
// /mcp__example__<name> slash commands.
func promptList() []map[string]any {
	return []map[string]any{{
		"name":        "review",
		"description": "Draft a focused code-review request for a file.",
		"arguments": []map[string]any{
			{"name": "path", "description": "File to review", "required": true},
		},
	}}
}

// getPrompt renders a prompt into MCP messages. The returned text becomes the
// next user turn in reasonix, so it's phrased as an instruction to the model.
func getPrompt(params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	if p.Name != "review" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown prompt: " + p.Name}
	}
	path := p.Arguments["path"]
	if path == "" {
		path = "the current file"
	}
	text := fmt.Sprintf("Please review %s. Read it, then list any correctness bugs and risky patterns with file:line references, most important first.", path)
	return map[string]any{
		"description": "Code review request",
		"messages": []map[string]any{
			{"role": "user", "content": map[string]any{"type": "text", "text": text}},
		},
	}, nil
}

// resources

// resourceContents is the demo resource store, keyed by uri.
var resourceContents = map[string]string{
	"doc://style-guide": "Project style: tabs for indentation; comments explain why, not what; keep functions short.",
}

func resourceList() []map[string]any {
	return []map[string]any{{
		"uri":         "doc://style-guide",
		"name":        "Style guide",
		"description": "The project's coding style notes.",
		"mimeType":    "text/plain",
	}}
}

func readResource(params json.RawMessage) (any, *rpcError) {
	var p struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
	}
	text, ok := resourceContents[p.URI]
	if !ok {
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown resource: " + p.URI}
	}
	return map[string]any{
		"contents": []map[string]any{
			{"uri": p.URI, "mimeType": "text/plain", "text": text},
		},
	}, nil
}

// countLines counts newline-separated lines, counting a final unterminated line.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// trimSpace trims leading/trailing ASCII whitespace without pulling in bytes.
func trimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
