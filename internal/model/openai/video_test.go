package openai

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

type videoServer struct {
	mu      sync.Mutex
	uploads []uploadSeen
	chats   []map[string]any
	// failChatOnce makes the first chat request answer 404, as a vendor does for
	// a file it no longer holds.
	failChatOnce bool
	srv          *httptest.Server
	filesBytes   int
}

func (v *videoServer) receivedBytes() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.filesBytes
}

type uploadSeen struct {
	Purpose string
	Name    string
	Body    []byte
}

func newVideoServer(t *testing.T) *videoServer {
	t.Helper()
	v := &videoServer{}
	v.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.mu.Lock()
		defer v.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/files" && r.Method == http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			v.filesBytes += len(raw)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f, h, _ := r.FormFile("file")
			body, _ := io.ReadAll(f)
			v.uploads = append(v.uploads, uploadSeen{Purpose: r.FormValue("purpose"), Name: h.Filename, Body: body})
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "file-" + string(rune('0'+len(v.uploads)))})
		case r.URL.Path == "/v1/chat/completions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			v.chats = append(v.chats, body)
			if v.failChatOnce {
				v.failChatOnce = false
				http.Error(w, `{"error":{"message":"file not found"}}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(v.srv.Close)
	return v
}

func (v *videoServer) provider(t *testing.T, protocol string) provider.Provider {
	t.Helper()
	p, err := New(provider.Config{
		Name: "gw", BaseURL: v.srv.URL + "/v1", Model: "vid-model", APIKey: "k",
		Extra: map[string]any{"video_protocol": protocol},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func writeVideo(t *testing.T, content string) provider.VideoRef {
	t.Helper()
	path := filepath.Join(testenv.TempDir(t), "clip.mp4")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	ref := ungrantedVideo(path, content, sum)
	provider.GrantVideo(ref)
	return ref
}

func ungrantedVideo(path, content string, sum [32]byte) provider.VideoRef {
	return provider.VideoRef{Path: path, Name: "clip.mp4", SHA256: hex.EncodeToString(sum[:]), MIME: "video/mp4", Size: int64(len(content)), DurationMs: 3000}
}

func readRound(ref provider.VideoRef) []provider.Message {
	return []provider.Message{
		{Role: provider.RoleUser, Content: "watch it"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "c1", Name: "read_video", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "read_video", Content: "read_video: clip.mp4", Videos: []provider.VideoRef{ref}},
	}
}

func run(t *testing.T, p provider.Provider, msgs []provider.Message) error {
	t.Helper()
	ch, err := p.Stream(t.Context(), provider.Request{Messages: msgs})
	if err != nil {
		return err
	}
	for range ch {
	}
	return nil
}

// the last user message of a request, as the JSON the vendor received.
func lastUserParts(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	msgs := body["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "user" {
		t.Fatalf("the video must follow the tool results in a user message, got %v", last["role"])
	}
	var out []map[string]any
	for _, p := range last["content"].([]any) {
		out = append(out, p.(map[string]any))
	}
	return out
}

func videoURLOf(parts []map[string]any) string {
	for _, p := range parts {
		if p["type"] == "video_url" {
			return p["video_url"].(map[string]any)["url"].(string)
		}
	}
	return ""
}

func TestInlineProtocolSendsTheFileAsADataURLAfterTheToolResults(t *testing.T) {
	v := newVideoServer(t)
	ref := writeVideo(t, "FAKE-MP4-BYTES")
	if err := run(t, v.provider(t, "inline_data_url"), readRound(ref)); err != nil {
		t.Fatal(err)
	}
	want := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte("FAKE-MP4-BYTES"))
	if got := videoURLOf(lastUserParts(t, v.chats[0])); got != want {
		t.Fatalf("video_url = %q, want %q", got, want)
	}
	if len(v.uploads) != 0 {
		t.Fatal("the inline protocol must not upload anything")
	}
}

func TestMoonshotFileProtocolUploadsOnceAndReferencesTheID(t *testing.T) {
	v := newVideoServer(t)
	ref := writeVideo(t, "FAKE-MP4-BYTES")
	p := v.provider(t, "moonshot_file")
	for range 2 {
		if err := run(t, p, readRound(ref)); err != nil {
			t.Fatal(err)
		}
	}
	if len(v.uploads) != 1 || v.uploads[0].Purpose != "video" || string(v.uploads[0].Body) != "FAKE-MP4-BYTES" {
		t.Fatalf("uploads = %+v, want exactly one purpose=video upload of the file", v.uploads)
	}
	for i, chat := range v.chats {
		if got := videoURLOf(lastUserParts(t, chat)); got != "ms://file-1" {
			t.Fatalf("request %d video_url = %q, want ms://file-1 (the cached id)", i, got)
		}
	}
}

// A vendor that no longer holds the file answers 404 to the request that names
// it. The cached id is dropped and the file is sent again, once.
func TestMoonshotFileProtocolSendsAgainWhenTheVendorForgotTheFile(t *testing.T) {
	v := newVideoServer(t)
	ref := writeVideo(t, "FAKE-MP4-BYTES")
	p := v.provider(t, "moonshot_file")
	if err := run(t, p, readRound(ref)); err != nil {
		t.Fatal(err)
	}
	v.mu.Lock()
	v.failChatOnce = true
	v.mu.Unlock()
	if err := run(t, p, readRound(ref)); err != nil {
		t.Fatalf("the request must recover from a forgotten file: %v", err)
	}
	if len(v.uploads) != 2 {
		t.Fatalf("uploads = %d, want 2 (the original and the re-send)", len(v.uploads))
	}
	last := v.chats[len(v.chats)-1]
	if got := videoURLOf(lastUserParts(t, last)); got != "ms://file-2" {
		t.Fatalf("the retry named %q, want the new id ms://file-2", got)
	}
}

func TestAChangedFileIsNotSentUnderTheOldName(t *testing.T) {
	v := newVideoServer(t)
	ref := writeVideo(t, "FAKE-MP4-BYTES")
	if err := os.WriteFile(ref.Path, []byte("SOMETHING ELSE ENTIRELY"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(t, v.provider(t, "inline_data_url"), readRound(ref)); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(v.chats[0])
	if strings.Contains(string(body), "video_url") || !strings.Contains(string(body), "video.file_changed") {
		t.Fatalf("a changed file must be reported by code and not sent: %s", body)
	}
}

func TestAMissingFileIsReportedByCode(t *testing.T) {
	v := newVideoServer(t)
	ref := writeVideo(t, "FAKE-MP4-BYTES")
	_ = os.Remove(ref.Path)
	if err := run(t, v.provider(t, "inline_data_url"), readRound(ref)); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(v.chats[0])
	if strings.Contains(string(body), "video_url") || !strings.Contains(string(body), "video.file_missing") {
		t.Fatalf("a missing file must be reported by code: %s", body)
	}
}

func TestWithoutAProtocolTheModelIsToldItWasNotSent(t *testing.T) {
	v := newVideoServer(t)
	ref := writeVideo(t, "FAKE-MP4-BYTES")
	if err := run(t, v.provider(t, ""), readRound(ref)); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(v.chats[0])
	if strings.Contains(string(body), "video_url") || !strings.Contains(string(body), "cannot take video natively") {
		t.Fatalf("an endpoint with no video protocol must say so to the model: %s", body)
	}
}

// The file is opened once and everything after that reads the opened file: a
// path swapped for another file in the gap sends none of the other file's bytes.
func swapAfterOpen(t *testing.T, ref provider.VideoRef, other string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not let a file that is open be replaced, so the swap cannot happen; the system guarantees what this test checks")
	}
	elsewhere := filepath.Join(testenv.TempDir(t), "other.mp4")
	if err := os.WriteFile(elsewhere, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	afterVideoOpen = func(path string) {
		if err := os.Rename(elsewhere, path); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { afterVideoOpen = func(string) {} })
}

func TestInlineSendsTheOpenedFileNotWhatThePathBecomes(t *testing.T) {
	v := newVideoServer(t)
	ref := writeVideo(t, "FAKE-MP4-BYTES")
	swapAfterOpen(t, ref, "SECRET-OF-ANOTHER-FILE")
	if err := run(t, v.provider(t, "inline_data_url"), readRound(ref)); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(v.chats[0])
	other := base64.StdEncoding.EncodeToString([]byte("SECRET-OF-ANOTHER-FILE"))
	if strings.Contains(string(body), other) || strings.Contains(string(body), "SECRET-OF-ANOTHER-FILE") {
		t.Fatalf("bytes of the file the path became were sent: %s", body)
	}
	want := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte("FAKE-MP4-BYTES"))
	if got := videoURLOf(lastUserParts(t, v.chats[0])); got != want {
		t.Fatalf("the opened file was not what was sent: %q", got)
	}
}

func TestUploadSendsTheOpenedFileNotWhatThePathBecomes(t *testing.T) {
	v := newVideoServer(t)
	ref := writeVideo(t, "FAKE-MP4-BYTES")
	swapAfterOpen(t, ref, "SECRET-OF-ANOTHER-FILE")
	if err := run(t, v.provider(t, "moonshot_file"), readRound(ref)); err != nil {
		t.Fatal(err)
	}
	if len(v.uploads) != 1 || string(v.uploads[0].Body) != "FAKE-MP4-BYTES" {
		t.Fatalf("uploads = %+v, want the opened file's bytes and nothing of the other file", v.uploads)
	}
}

// An upload whose bytes stop matching what the tool saw is not completed, so the
// vendor never holds a file that is not the one the model was told about.
func TestUploadIsAbortedWhenTheBytesStopMatchingMidStream(t *testing.T) {
	v := newVideoServer(t)
	ref := writeVideo(t, "FAKE-MP4-BYTES")
	ref.SHA256 = strings.Repeat("0", 64)
	provider.GrantVideo(ref)
	if err := run(t, v.provider(t, "moonshot_file"), readRound(ref)); err != nil {
		t.Fatal(err)
	}
	if len(v.uploads) != 0 {
		t.Fatalf("a file that does not match its hash was uploaded: %+v", v.uploads)
	}
	body, _ := json.Marshal(v.chats[0])
	if !strings.Contains(string(body), "video.file_changed") {
		t.Fatalf("the model was not told by code: %s", body)
	}
}

// The path may already be another file when it is opened. Nothing of it leaves.
func swapBeforeOpen(t *testing.T, other string) {
	t.Helper()
	elsewhere := filepath.Join(testenv.TempDir(t), "other.mp4")
	if err := os.WriteFile(elsewhere, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	beforeVideoOpen = func(path string) {
		if err := os.Rename(elsewhere, path); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { beforeVideoOpen = func(string) {} })
}

func TestInlineSendsNothingOfAFileThePathBecameBeforeItWasOpened(t *testing.T) {
	v := newVideoServer(t)
	ref := writeVideo(t, "FAKE-MP4-BYTES")
	swapBeforeOpen(t, "SECRET-OF-ANOTHER-FILE")
	if err := run(t, v.provider(t, "inline_data_url"), readRound(ref)); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(v.chats[0])
	if strings.Contains(string(body), "SECRET-OF-ANOTHER-FILE") || strings.Contains(string(body), base64.StdEncoding.EncodeToString([]byte("SECRET-OF-ANOTHER-FILE"))) ||
		strings.Contains(string(body), "video_url") || !strings.Contains(string(body), "video.file_changed") {
		t.Fatalf("the swapped file was sent, or the model was not told by code: %s", body)
	}
}

func TestUploadSendsNotAByteOfAFileThePathBecameBeforeItWasOpened(t *testing.T) {
	v := newVideoServer(t)
	ref := writeVideo(t, "FAKE-MP4-BYTES")
	swapBeforeOpen(t, "SECRET-OF-ANOTHER-FILE")
	if err := run(t, v.provider(t, "moonshot_file"), readRound(ref)); err != nil {
		t.Fatal(err)
	}
	if v.receivedBytes() != 0 {
		t.Fatalf("the vendor received %d bytes of the file the path became", v.receivedBytes())
	}
	body, _ := json.Marshal(v.chats[0])
	if !strings.Contains(string(body), "video.file_changed") {
		t.Fatalf("the model was not told by code: %s", body)
	}
}

func TestAReferenceNoToolGrantedSendsNotAByteOfItsFile(t *testing.T) {
	outside := filepath.Join(testenv.TempDir(t), "secret.mp4")
	content := "NOT-A-VIDEO-BUT-READABLE-SECRET"
	if err := os.WriteFile(outside, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	ref := ungrantedVideo(outside, content, sha256.Sum256([]byte(content)))
	for _, protocol := range []string{"inline_data_url", "moonshot_file"} {
		v := newVideoServer(t)
		if err := run(t, v.provider(t, protocol), readRound(ref)); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(v.chats[0])
		if strings.Contains(string(body), "video_url") || !strings.Contains(string(body), "video.file_unavailable") {
			t.Fatalf("%s: an ungranted reference must be reported by code and not sent: %s", protocol, body)
		}
		if v.receivedBytes() != 0 || strings.Contains(string(body), base64.StdEncoding.EncodeToString([]byte(content))) {
			t.Fatalf("%s: bytes of a file no tool handed out left the process", protocol)
		}
	}
}

func TestAGrantedHashIsOpenedAtTheGrantedPathNotTheOneTheReferenceNames(t *testing.T) {
	v := newVideoServer(t)
	ref := writeVideo(t, "GRANTED-BYTES")
	other := filepath.Join(testenv.TempDir(t), "other.mp4")
	if err := os.WriteFile(other, []byte("OTHER"), 0o600); err != nil {
		t.Fatal(err)
	}
	ref.Path = other
	if err := run(t, v.provider(t, "inline_data_url"), readRound(ref)); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(v.chats[0])
	if !strings.Contains(string(body), base64.StdEncoding.EncodeToString([]byte("GRANTED-BYTES"))) {
		t.Fatalf("the granted file must be the one sent: %s", body)
	}
}

// https://docs.bigmodel.cn/cn/guide/models/vlm/glm-4.6v (read 2026-10-17): the
// video is {"type":"video_url","video_url":{"url":"https://..."}} in chat/completions.
func TestZhipuSendsAGrantedAddressAsAVideoURLPart(t *testing.T) {
	const addr = "https://cdn.example.com/clip.mp4"
	provider.GrantVideoAddress(addr)
	v := newVideoServer(t)
	if err := run(t, v.provider(t, "zhipu_url"), readRound(provider.VideoRef{URL: addr, Name: "clip.mp4"})); err != nil {
		t.Fatal(err)
	}
	if got := videoURLOf(lastUserParts(t, v.chats[0])); got != addr {
		t.Fatalf("video_url = %q, want %q", got, addr)
	}
	if len(v.uploads) != 0 {
		t.Fatal("an address is never uploaded")
	}
}

func bodyText(v *videoServer) string {
	raw, _ := json.Marshal(v.chats[0])
	return string(raw)
}

func TestZhipuNeverSendsAnAddressNoToolGranted(t *testing.T) {
	v := newVideoServer(t)
	if err := run(t, v.provider(t, "zhipu_url"), readRound(provider.VideoRef{URL: "https://cdn.example.com/forged.mp4", Name: "forged.mp4"})); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bodyText(v), "video_url") || !strings.Contains(bodyText(v), provider.VideoURLUnavailable) {
		t.Fatalf("a forged reference reached the wire: %s", bodyText(v))
	}
}

func TestZhipuHasNoWireForALocalFile(t *testing.T) {
	v := newVideoServer(t)
	if err := run(t, v.provider(t, "zhipu_url"), readRound(writeVideo(t, "FAKE-MP4-BYTES"))); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bodyText(v), "video_url") || !strings.Contains(bodyText(v), provider.VideoAddressOnly) || len(v.uploads) != 0 {
		t.Fatalf("request = %s", bodyText(v))
	}
}

func TestAnAddressIsNotSentOnAProtocolThatDocumentsNone(t *testing.T) {
	const addr = "https://cdn.example.com/clip2.mp4"
	provider.GrantVideoAddress(addr)
	v := newVideoServer(t)
	if err := run(t, v.provider(t, "inline_data_url"), readRound(provider.VideoRef{URL: addr, Name: "clip2.mp4"})); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bodyText(v), "video_url") || !strings.Contains(bodyText(v), provider.VideoURLUnsupported) {
		t.Fatalf("request = %s", bodyText(v))
	}
}
