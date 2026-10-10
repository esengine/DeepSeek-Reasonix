package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func main() {
	if err := serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		var req request
		resp := response{JSONRPC: "2.0", ID: json.RawMessage("null")}
		switch {
		case !json.Valid(scanner.Bytes()):
			resp.Error = &rpcError{-32700, "Invalid JSON request"}
		case json.Unmarshal(scanner.Bytes(), &req) != nil || req.JSONRPC != "2.0" || req.Method == "":
			resp.Error = &rpcError{-32600, "Invalid JSON-RPC request"}
		case len(req.ID) == 0:
			continue
		default:
			resp.ID = req.ID
			resp.Result, resp.Error = handle(req.Method, req.Params)
		}
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func handle(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2025-03-26",
			"serverInfo":      map[string]any{"name": "line-counter", "version": "0.1.0"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": []map[string]any{{
			"name":        "count_lines",
			"description": "Count newline-separated lines in supplied text; an empty string has zero lines and a final newline adds no line.",
			"inputSchema": map[string]any{
				"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}},
				"required": []string{"text"}, "additionalProperties": false,
			},
			"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
		}}}, nil
	case "tools/call":
		var call struct {
			Name      string                     `json:"name"`
			Arguments map[string]json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(params, &call); err != nil || call.Name != "count_lines" || len(call.Arguments) != 1 {
			return nil, &rpcError{-32602, "count_lines requires a string text argument"}
		}
		var value *string
		if err := json.Unmarshal(call.Arguments["text"], &value); err != nil || value == nil {
			return nil, &rpcError{-32602, "count_lines requires a string text argument"}
		}
		text := *value
		count := strings.Count(text, "\n")
		if text != "" && !strings.HasSuffix(text, "\n") {
			count++
		}
		return map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("Line count: %d", count)}}}, nil
	default:
		return nil, &rpcError{-32601, "Method not found"}
	}
}
