package builtinmcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	stdtime "time"
)

const protocolVersion = "2024-11-05"

// RunCommand runs hidden built-in MCP subcommands shared by the CLI and desktop
// binary. It intentionally writes only MCP JSON-RPC frames to stdout.
func RunCommand(args []string, in io.Reader, out io.Writer, errOut io.Writer, version string) int {
	if len(args) != 1 || args[0] != TimeName {
		fmt.Fprintln(errOut, "usage: reasonix builtin-mcp time")
		return 2
	}
	if serveErr := ServeTimeMCP(in, out, version); serveErr != nil {
		fmt.Fprintln(errOut, serveErr)
		return 1
	}
	return 0
}

// ServeTimeMCP serves Reasonix's dependency-free built-in time MCP over stdio.
func ServeTimeMCP(in io.Reader, out io.Writer, version string) error {
	r := bufio.NewReader(in)
	w := bufio.NewWriter(out)
	defer w.Flush()

	for {
		line, err := r.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			if handleErr := handleTimeLine(line, w, version); handleErr != nil {
				return handleErr
			}
			if flushErr := w.Flush(); flushErr != nil {
				return flushErr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

type timeRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params"`
}

type timeResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *timeRPCError   `json:"error,omitempty"`
}

type timeRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func handleTimeLine(line []byte, w *bufio.Writer, version string) error {
	var req timeRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return nil
	}
	if req.ID == nil {
		return nil
	}

	resp := timeResponse{JSONRPC: "2.0", ID: *req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{"name": "reasonix-time", "version": version},
		}
	case "tools/list":
		resp.Result = map[string]any{"tools": timeTools()}
	case "tools/call":
		resp.Result, resp.Error = callTimeTool(req.Params)
	default:
		resp.Error = &timeRPCError{Code: -32601, Message: "method not found: " + req.Method}
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func timeTools() []map[string]any {
	return []map[string]any{
		{
			"name":        "get_current_time",
			"description": "Get the current time in a specific IANA timezone, or the system timezone when omitted.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"timezone": map[string]any{"type": "string", "description": "IANA timezone name, for example America/New_York"},
				},
			},
			"annotations": map[string]any{"readOnlyHint": true, "title": "get_current_time"},
		},
		{
			"name":        "convert_time",
			"description": "Convert a 24-hour HH:MM time between two IANA timezones using today's date in the source timezone.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"source_timezone": map[string]any{"type": "string", "description": "Source IANA timezone name"},
					"time":            map[string]any{"type": "string", "description": "Time in 24-hour HH:MM format"},
					"target_timezone": map[string]any{"type": "string", "description": "Target IANA timezone name"},
				},
				"required": []string{"source_timezone", "time", "target_timezone"},
			},
			"annotations": map[string]any{"readOnlyHint": true, "title": "convert_time"},
		},
	}
}

func callTimeTool(params json.RawMessage) (any, *timeRPCError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &timeRPCError{Code: -32602, Message: "invalid params: " + err.Error()}
	}
	var (
		text string
		err  error
	)
	switch p.Name {
	case "get_current_time":
		text, err = getCurrentTime(p.Arguments)
	case "convert_time":
		text, err = convertTime(p.Arguments)
	default:
		return nil, &timeRPCError{Code: -32602, Message: "unknown tool: " + p.Name}
	}
	if err != nil {
		return timeTextResult(err.Error(), true), nil
	}
	return timeTextResult(text, false), nil
}

func getCurrentTime(args map[string]any) (string, error) {
	loc, name, err := loadLocation(optionalString(args, "timezone"))
	if err != nil {
		return "", err
	}
	now := stdtime.Now().In(loc)
	return marshalTimeResult(map[string]any{
		"timezone": name,
		"datetime": now.Format(stdtime.RFC3339),
		"is_dst":   now.IsDST(),
	})
}

func convertTime(args map[string]any) (string, error) {
	sourceName := requiredString(args, "source_timezone")
	targetName := requiredString(args, "target_timezone")
	rawTime := requiredString(args, "time")
	if sourceName == "" || targetName == "" || rawTime == "" {
		return "", fmt.Errorf("source_timezone, time, and target_timezone are required")
	}
	sourceLoc, _, err := loadLocation(sourceName)
	if err != nil {
		return "", err
	}
	targetLoc, _, err := loadLocation(targetName)
	if err != nil {
		return "", err
	}
	hour, minute, err := parseHHMM(rawTime)
	if err != nil {
		return "", err
	}
	today := stdtime.Now().In(sourceLoc)
	source := stdtime.Date(today.Year(), today.Month(), today.Day(), hour, minute, 0, 0, sourceLoc)
	target := source.In(targetLoc)
	_, sourceOffset := source.Zone()
	_, targetOffset := target.Zone()
	return marshalTimeResult(map[string]any{
		"source": map[string]any{
			"timezone": sourceName,
			"datetime": source.Format(stdtime.RFC3339),
			"is_dst":   source.IsDST(),
		},
		"target": map[string]any{
			"timezone": targetName,
			"datetime": target.Format(stdtime.RFC3339),
			"is_dst":   target.IsDST(),
		},
		"time_difference": fmt.Sprintf("%+.1fh", float64(targetOffset-sourceOffset)/3600),
	})
}

func loadLocation(name string) (*stdtime.Location, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return stdtime.Local, stdtime.Local.String(), nil
	}
	loc, err := stdtime.LoadLocation(name)
	if err != nil {
		return nil, "", fmt.Errorf("unknown timezone %q", name)
	}
	return loc, name, nil
}

func parseHHMM(raw string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("time must use HH:MM format")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("hour must be between 00 and 23")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("minute must be between 00 and 59")
	}
	return hour, minute, nil
}

func optionalString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func requiredString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func marshalTimeResult(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func timeTextResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}
