package sidecar

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/rpcwire"
	"reasonix/internal/pluginpkg"
)

// Re-exec fake sidecar (the standard Go helper-process pattern): the test
// binary re-executes itself with REASONIX_FAKE_SIDECAR=1 and speaks the real
// Extension Protocol v1 over stdin/stdout. Behavior is steered through env:
//
//		REASONIX_FAKE_SIDECAR=1            enable the helper
//		REASONIX_FAKE_INIT_RESULT          raw JSON InitializeResult to answer with
//		REASONIX_FAKE_MODE                 comma-separated behavior flags:
//		                                   early_request | early_notify |
//		                                   ignore_shutdown | stall_intercept |
//		                                   block_intercept | stderr_flood |
//		                                   content_roundtrip | content_echo_ref |
//		                                   provider_stream | crash_after_init |
//	                                  wedge_after_init
const (
	fakeEnvEnable     = "REASONIX_FAKE_SIDECAR"
	fakeEnvInitResult = "REASONIX_FAKE_INIT_RESULT"
	fakeEnvMode       = "REASONIX_FAKE_MODE"
)

// TestFakeSidecarHelperProcess is the re-exec entry point. It skips in the
// parent test run and only acts as the sidecar when the env marker is set.
func TestFakeSidecarHelperProcess(t *testing.T) {
	if os.Getenv(fakeEnvEnable) != "1" {
		t.Skip("fake sidecar helper process")
	}
	runFakeSidecar(os.Stdin, os.Stdout)
	os.Exit(0)
}

type fakeFrame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
}

// fakeExternalizedField mirrors the wire envelope descriptor the fake parses
// from intercept params (and quotes back in content_echo_ref mode).
type fakeExternalizedField struct {
	JSONPointer string `json:"jsonPointer"`
	ContentRef  string `json:"contentRef"`
	TotalBytes  int64  `json:"totalBytes"`
	SHA256      string `json:"sha256"`
}

func fakeModes() map[string]bool {
	out := map[string]bool{}
	for mode := range strings.SplitSeq(os.Getenv(fakeEnvMode), ",") {
		if mode = strings.TrimSpace(mode); mode != "" {
			out[mode] = true
		}
	}
	return out
}

func fakeInitResult() json.RawMessage {
	if raw := strings.TrimSpace(os.Getenv(fakeEnvInitResult)); raw != "" {
		return json.RawMessage(raw)
	}
	return json.RawMessage(`{"protocolVersion":"1","name":"fake-sidecar","version":"1.0.0","stateSchemaVersion":0}`)
}

func runFakeSidecar(stdin io.Reader, stdout io.Writer) {
	modes := fakeModes()
	out := bufio.NewWriter(stdout)
	write := func(format string, args ...any) {
		fmt.Fprintf(out, format+"\n", args...)
		_ = out.Flush()
	}
	if modes["stderr_flood"] {
		// Push well past the 16 KiB tail so only the end survives, then leave
		// a credential-looking line at the very end — it must be retained by
		// the ring and redacted before surfacing.
		for i := range 32 * 1024 {
			fmt.Fprintf(os.Stderr, "flood line %d padding padding padding\n", i)
		}
		fmt.Fprintln(os.Stderr, "boot failed: api_key=sk-abcdef1234567890SECRETKEY is invalid")
	}

	respond := func(id json.RawMessage, result json.RawMessage) {
		write(`{"jsonrpc":"2.0","id":%s,"result":%s}`, string(id), string(result))
	}

	// readContentRef pages one host-side content ref through host/content/read
	// the way a real extension would, answering any interleaved host requests
	// with the default ruling while it waits for each chunk.
	readSeq := 88000
	readContentRef := func(in *bufio.Reader, ref string) []byte {
		var out []byte
		var offset int64
		for {
			readSeq++
			id := readSeq
			params, _ := json.Marshal(map[string]any{"contentRef": ref, "offset": offset})
			write(`{"jsonrpc":"2.0","id":%d,"method":"host/content/read","params":%s}`, id, string(params))
			for {
				line, err := in.ReadBytes('\n')
				if err != nil {
					return out
				}
				if len(line) == 0 {
					continue
				}
				var frame fakeFrame
				if json.Unmarshal(line, &frame) != nil {
					continue
				}
				if frame.Method != "" {
					// Interleaved host request while the read pages: answer with
					// the defaults so the host never wedges waiting on us.
					switch frame.Method {
					case string(protocol.MethodExtensionShutdown):
						respond(frame.ID, json.RawMessage(`{"accepted":true}`))
					case string(protocol.MethodExtensionIntercept):
						respond(frame.ID, json.RawMessage(`{"decision":"continue"}`))
					default:
						respond(frame.ID, json.RawMessage(`{}`))
					}
					continue
				}
				if string(frame.ID) != fmt.Sprintf("%d", id) {
					continue
				}
				var chunk struct {
					DataBase64 string `json:"dataBase64"`
					NextOffset *int64 `json:"nextOffset"`
				}
				if json.Unmarshal(frame.Result, &chunk) != nil {
					return out
				}
				data, err := base64.StdEncoding.DecodeString(chunk.DataBase64)
				if err != nil {
					return out
				}
				out = append(out, data...)
				if chunk.NextOffset == nil {
					return out
				}
				offset = *chunk.NextOffset
				break
			}
		}
	}

	in := bufio.NewReader(stdin)
	for {
		line, err := in.ReadBytes('\n')
		if len(line) > 0 {
			var frame fakeFrame
			if json.Unmarshal(line, &frame) == nil && frame.Method != "" {
				switch frame.Method {
				case string(protocol.MethodExtensionInitialize):
					if modes["early_request"] {
						write(`{"jsonrpc":"2.0","id":77001,"method":"host/content/read","params":{"contentRef":"content_nope","offset":0}}`)
					}
					if modes["early_notify"] {
						write(`{"jsonrpc":"2.0","method":"extension/provider/stream/chunk","params":{"streamId":"s1","seq":1,"chunk":{"type":"text","text":"hi"}}}`)
					}
					respond(frame.ID, fakeInitResult())
				case string(protocol.MethodExtensionInitialized):
					// notification; nothing to do — except in crash_after_init
					// mode, where exiting here ends the connection while the
					// host sees a ready handshake: an unexpected EOF (crash).
					if modes["crash_after_init"] {
						return
					}
					if modes["wedge_after_init"] {
						// Stay alive but never read stdin again: the host's
						// writes fill the pipe and must hit the write-stall
						// bound, not hang. The sleep keeps a timer pending so
						// the runtime's deadlock detector leaves us alive.
						for {
							time.Sleep(time.Hour)
						}
					}
				case string(protocol.MethodExtensionIntercept):
					switch {
					case modes["stall_intercept"]:
						fmt.Fprintln(os.Stderr, "intercept-stalled")
						// never answer: the host-side timeout or a crash ends it
					case modes["block_intercept"]:
						respond(frame.ID, json.RawMessage(`{"decision":"block","reason":"fake block"}`))
					case modes["content_roundtrip"] || modes["content_echo_ref"]:
						var params struct {
							Payload      json.RawMessage         `json:"payload"`
							Externalized []fakeExternalizedField `json:"externalized"`
						}
						_ = json.Unmarshal(frame.Params, &params)
						var payload []byte
						if len(params.Externalized) > 0 {
							payload = readContentRef(in, params.Externalized[0].ContentRef)
						} else {
							payload = append([]byte(nil), params.Payload...)
						}
						if modes["content_echo_ref"] && len(params.Externalized) > 0 {
							// Hand the same host-held content back as the ruling:
							// the replacement stays a ref the host must resolve.
							descriptor := params.Externalized[0]
							descriptor.JSONPointer = "/replacement"
							raw, _ := json.Marshal(descriptor)
							respond(frame.ID, json.RawMessage(fmt.Sprintf(`{"decision":"replace","replacement":null,"externalized":[%s]}`, string(raw))))
							break
						}
						// Prove the read: the replacement text carries the
						// reassembled payload's digest, padded past the 64 KiB
						// threshold so it exercises a large inline replacement.
						sum := sha256.Sum256(payload)
						text := fmt.Sprintf("read %d bytes sha256:%s ", len(payload), hex.EncodeToString(sum[:]))
						text += strings.Repeat("y", protocol.ExternalizeFieldBytes+4096)
						replacement, _ := json.Marshal(map[string]string{"text": text})
						respond(frame.ID, json.RawMessage(fmt.Sprintf(`{"decision":"replace","replacement":%s}`, string(replacement))))
					default:
						respond(frame.ID, json.RawMessage(`{"decision":"continue"}`))
					}
				case string(protocol.MethodExtensionShutdown):
					if modes["ignore_shutdown"] {
						// Stay alive without answering; the host must kill us
						// inside its bounded close.
						continue
					}
					respond(frame.ID, json.RawMessage(`{"accepted":true}`))
					return
				case string(protocol.MethodExtensionProviderCatalog):
					respond(frame.ID, json.RawMessage(`{"providers":[]}`))
				case string(protocol.MethodExtensionProviderStreamOpen):
					respond(frame.ID, json.RawMessage(`{"accepted":true}`))
					if modes["provider_stream"] {
						var params struct {
							StreamID string `json:"streamId"`
						}
						_ = json.Unmarshal(frame.Params, &params)
						write(`{"jsonrpc":"2.0","method":"extension/provider/stream/chunk","params":{"streamId":%q,"seq":1,"chunk":{"type":"text","text":"wired"}}}`, params.StreamID)
						write(`{"jsonrpc":"2.0","method":"extension/provider/stream/end","params":{"streamId":%q,"lastSeq":1}}`, params.StreamID)
					}
				case string(protocol.MethodExtensionProviderStreamCancel):
					respond(frame.ID, json.RawMessage(`{"cancelled":true}`))
				case string(protocol.MethodExtensionUIAction):
					// Echo the invoked action id so the host-side test proves
					// the routing; the message carries a credential-looking
					// token to exercise hub redaction end to end.
					var params struct {
						ActionID string `json:"actionId"`
					}
					_ = json.Unmarshal(frame.Params, &params)
					message, _ := json.Marshal("ran " + params.ActionID + " with api_key=sk-abcdef1234567890SECRETKEY")
					respond(frame.ID, json.RawMessage(`{"accepted":true,"message":`+string(message)+`}`))
				case string(protocol.MethodExtensionUISubmit):
					respond(frame.ID, json.RawMessage(`{"accepted":true}`))
				default:
					respond(frame.ID, json.RawMessage(`{}`))
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// fakeSidecarRuntime builds the exec-form runtime spec pointing at the
// re-executed test binary.
func fakeSidecarRuntime(t testing.TB, configure func(rt *pluginpkg.RuntimeSpec)) *pluginpkg.RuntimeSpec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	rt := &pluginpkg.RuntimeSpec{
		Command: exe,
		Args:    []string{"-test.run=^TestFakeSidecarHelperProcess$"},
		Env:     map[string]string{fakeEnvEnable: "1"},
	}
	if configure != nil {
		configure(rt)
	}
	return rt
}

// fakeSidecarPackage builds the installed-state entry + package for one fake
// sidecar. The package root is an empty temp dir: the runtime command is the
// test binary itself, so nothing needs to exist on disk.
func fakeSidecarPackage(t testing.TB, name string, configure func(rt *pluginpkg.RuntimeSpec)) (pluginpkg.Package, pluginpkg.InstalledPlugin) {
	t.Helper()
	rt := fakeSidecarRuntime(t, configure)
	pkg := pluginpkg.Package{
		Root:         t.TempDir(),
		ManifestKind: "reasonix",
		Manifest: pluginpkg.Manifest{
			Name:    name,
			Version: "1.0.0",
			Runtime: rt,
		},
	}
	installed := pluginpkg.InstalledPlugin{Name: name, Version: "1.0.0", Enabled: true, Root: pkg.Root}
	return pkg, installed
}

func testSessionContext() protocol.SessionContext {
	return protocol.SessionContext{SessionID: "sess-test", WorkspaceRoot: "/ws", Generation: 1}
}

// startFakeClient starts a fake sidecar client and registers its bounded
// shutdown.
func startFakeClient(t testing.TB, configure func(rt *pluginpkg.RuntimeSpec), opts func(*ClientOptions)) *Client {
	t.Helper()
	pkg, installed := fakeSidecarPackage(t, "fakeplugin", configure)
	clientOpts := ClientOptions{Package: pkg, Installed: installed, Session: testSessionContext()}
	if opts != nil {
		opts(&clientOpts)
	}
	client, err := StartClient(context.Background(), clientOpts)
	if err != nil {
		t.Fatalf("StartClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// waitFor polls cond until it holds or the deadline expires.
func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// protocolReason extracts the frozen protocol error reason from err, whether
// it travels as a local *protocol.ProtocolError or as the wire-shaped
// *rpcwire.RPCError handlers return.
func protocolReason(t *testing.T, err error) protocol.ErrorReason {
	t.Helper()
	var protocolErr *protocol.ProtocolError
	if errors.As(err, &protocolErr) {
		return protocolErr.Reason
	}
	var rpcErr *rpcwire.RPCError
	if errors.As(err, &rpcErr) {
		var data protocol.ProtocolErrorData
		raw, _ := json.Marshal(rpcErr.Data)
		if json.Unmarshal(raw, &data) == nil && data.Reason != "" {
			return data.Reason
		}
	}
	t.Fatalf("error %v carries no protocol reason", err)
	return ""
}
