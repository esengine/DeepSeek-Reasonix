package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestStdioSession(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":"start","method":"initialize","params":{"protocolVersion":"2025-03-26"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"ping"}
`
	var out bytes.Buffer
	if err := serve(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&out)
	var initialized struct {
		ID     string `json:"id"`
		Result struct {
			ProtocolVersion string                     `json:"protocolVersion"`
			Capabilities    map[string]json.RawMessage `json:"capabilities"`
		} `json:"result"`
	}
	if err := decoder.Decode(&initialized); err != nil || initialized.ID != "start" || initialized.Result.ProtocolVersion != "2025-03-26" {
		t.Fatalf("initialize = %+v, err=%v", initialized, err)
	}
	if _, ok := initialized.Result.Capabilities["tools"]; !ok {
		t.Fatal("server did not advertise tools")
	}
	var listed struct {
		ID     int `json:"id"`
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Annotations struct {
					ReadOnly bool `json:"readOnlyHint"`
				} `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := decoder.Decode(&listed); err != nil || listed.ID != 2 || len(listed.Result.Tools) != 1 || listed.Result.Tools[0].Name != "count_lines" || !listed.Result.Tools[0].Annotations.ReadOnly {
		t.Fatalf("tools/list = %+v, err=%v", listed, err)
	}
	var ping response
	if err := decoder.Decode(&ping); err != nil || string(ping.ID) != "3" || ping.Error != nil {
		t.Fatalf("ping = %+v, err=%v", ping, err)
	}
	if decoder.More() {
		t.Fatal("notification wrote an extra response")
	}
}

func TestCountLinesThroughStdio(t *testing.T) {
	for _, tc := range []struct {
		text  string
		count int
	}{
		{"", 0}, {"one", 1}, {"one\n", 1}, {"\n", 1},
		{"one\n\n", 2}, {"一\r\n二\r\n", 2}, {"one\ntwo\nthree", 3},
	} {
		t.Run(fmt.Sprintf("%q", tc.text), func(t *testing.T) {
			args, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
				"name": "count_lines", "arguments": map[string]any{"text": tc.text},
			}})
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := serve(bytes.NewReader(append(args, '\n')), &out); err != nil {
				t.Fatal(err)
			}
			var result struct {
				Error  *rpcError `json:"error"`
				Result struct {
					Content []struct{ Type, Text string } `json:"content"`
				} `json:"result"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Error != nil || len(result.Result.Content) != 1 {
				t.Fatalf("response = %s, err=%v", out.String(), err)
			}
			if got := result.Result.Content[0]; got.Type != "text" || got.Text != fmt.Sprintf("Line count: %d", tc.count) {
				t.Fatalf("content = %+v", got)
			}
		})
	}
}

func TestStdioErrors(t *testing.T) {
	for _, tc := range []struct {
		input string
		code  int
	}{
		{`{`, -32700},
		{`[]`, -32600},
		{`{"id":1,"method":"ping"}`, -32600},
		{`{"jsonrpc":"2.0","id":1,"method":true}`, -32600},
		{`{"jsonrpc":"2.0","id":1,"method":"unknown"}`, -32601},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"other","arguments":{"text":"one"}}}`, -32602},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"count_lines","arguments":{}}}`, -32602},
		{`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"count_lines","arguments":{"text":7}}}`, -32602},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var out bytes.Buffer
			if err := serve(strings.NewReader(tc.input+"\n"), &out); err != nil {
				t.Fatal(err)
			}
			var resp response
			if err := json.Unmarshal(out.Bytes(), &resp); err != nil || resp.Error == nil || resp.Error.Code != tc.code || resp.Result != nil {
				t.Fatalf("response = %s, err=%v", out.String(), err)
			}
		})
	}
}
