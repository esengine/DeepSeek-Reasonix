package feedback

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type stub struct {
	mu       sync.Mutex
	posts    []wireSubmit
	tokens   []string
	gets     []http.Header
	postFn   func(n int, w http.ResponseWriter, r *http.Request) bool
	mineBody string
	mineCode int
	rows     []stubRow
}

type stubRow struct{ install, key, receipt, at string }

func stubToken(install string) string { return "tok-" + install }

var installPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

func stubRefuse(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"error":{"code":"`+code+`","message":"stub"}}`)
}

// admit applies the worker's submit rules in the worker's order: schema, then
// idempotent replay (which returns the token again), then the install token for
// an install that already has a report, then the per-install hourly limit.
func (s *stub) admit(w http.ResponseWriter, r *http.Request, b wireSubmit) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	env := b.Env
	if len(b.IdempotencyKey) < 8 || len(b.IdempotencyKey) > 64 || !installPattern.MatchString(b.InstallID) ||
		strings.TrimSpace(b.Body) == "" || len(b.Body) > 8192 || b.DisplayName == "" || len([]rune(b.DisplayName)) > 40 ||
		len([]rune(b.Contact)) > 120 || len(b.Attachments) > 3 || len([]rune(env.Locale)) > 20 || len([]rune(env.Version)) > 40 ||
		len([]rune(env.Commit)) > 40 || len([]rune(env.OSVersion)) > 80 || len([]rune(env.Arch)) > 20 || len([]rune(env.OS)) > 40 ||
		!slices.Contains([]string{"studio", "tui", "acp"}, env.Surface) || !slices.Contains([]Category{Bug, Idea, Question, Other}, b.Category) {
		stubRefuse(w, http.StatusBadRequest, "feedback.invalid")
		return true
	}
	known, count := false, 0
	for _, row := range s.rows {
		if row.install != b.InstallID {
			continue
		}
		known = true
		count++
		if row.key == b.IdempotencyKey {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"receipt":"`+row.receipt+`","status":"received","installToken":"`+stubToken(b.InstallID)+`","createdAt":"`+row.at+`"}`)
			return true
		}
	}
	if known && r.Header.Get("X-Install-Token") != stubToken(b.InstallID) {
		stubRefuse(w, http.StatusUnauthorized, "feedback.bad_token")
		return true
	}
	if count >= 3 {
		stubRefuse(w, http.StatusTooManyRequests, "feedback.rate_limited")
		return true
	}
	for _, a := range b.Attachments {
		raw, err := base64.StdEncoding.DecodeString(a.DataBase64)
		if err != nil || !stubClean(raw) {
			stubRefuse(w, http.StatusBadRequest, "feedback.image_metadata")
			return true
		}
	}
	receipt := "FB-7K3M-9QX2"
	if len(s.rows) > 0 {
		receipt = "FB-2H8P-4WD" + string(rune('0'+len(s.rows)%10))
	}
	at := "2026-09-30T08:0" + string(rune('0'+len(s.rows)%10)) + ":00Z"
	s.rows = append(s.rows, stubRow{b.InstallID, b.IdempotencyKey, receipt, at})
	w.WriteHeader(http.StatusCreated)
	_, _ = io.WriteString(w, `{"receipt":"`+receipt+`","status":"received","installToken":"`+stubToken(b.InstallID)+`","createdAt":"`+at+`"}`)
	return true
}

// stubClean is the worker's allow-list, written out on its own so the client's
// check is not what marks its own work.
func stubClean(data []byte) bool {
	if len(data) > 8 && string(data[1:4]) == "PNG" {
		allowed := []string{"IHDR", "PLTE", "IDAT", "IEND", "gAMA", "cHRM", "sRGB", "iCCP", "pHYs", "tRNS", "bKGD", "sBIT"}
		pos := 8
		for pos+12 <= len(data) {
			size := int(binary.BigEndian.Uint32(data[pos:]))
			kind := string(data[pos+4 : pos+8])
			if !slices.Contains(allowed, kind) {
				return false
			}
			pos += 12 + size
			if kind == "IEND" {
				return pos == len(data)
			}
		}
		return false
	}
	for pos := 2; pos+4 <= len(data) && data[pos] == 0xFF; {
		m := data[pos+1]
		if m == 0xDA {
			return true
		}
		if m != 0xE0 && m != 0xE2 && m != 0xEE && (m >= 0xE0 && m <= 0xEF || m == 0xFE) {
			return false
		}
		pos += 2 + int(binary.BigEndian.Uint16(data[pos+2:]))
	}
	return len(data) > 2 && data[0] == 0xFF
}

func (s *stub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/feedback", func(w http.ResponseWriter, r *http.Request) {
		var body wireSubmit
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.posts = append(s.posts, body)
		s.tokens = append(s.tokens, r.Header.Get("X-Install-Token"))
		n := len(s.posts)
		s.mu.Unlock()
		if s.postFn != nil && s.postFn(n, w, r) {
			return
		}
		s.admit(w, r, body)
	})
	mux.HandleFunc("GET /v1/feedback/mine", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.gets = append(s.gets, r.Header.Clone())
		s.mu.Unlock()
		if s.mineCode != 0 {
			w.WriteHeader(s.mineCode)
		}
		_, _ = io.WriteString(w, s.mineBody)
	})
	return mux
}

func setup(t *testing.T, st *stub) (*Service, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(st.handler())
	t.Cleanup(srv.Close)
	svc, err := New(Config{Home: t.TempDir(), Base: srv.URL, HTTP: srv.Client(), Backoff: []time.Duration{}})
	if err != nil {
		t.Fatal(err)
	}
	return svc, srv
}

func draft() Draft {
	return Draft{Category: Bug, Body: "Sidebar loses selection", DisplayName: "kim", Env: EnvContext{Surface: SurfaceStudio, ProviderKind: "deepseek"}}
}

func apiError(status int, code string) func(int, http.ResponseWriter, *http.Request) bool {
	return func(_ int, w http.ResponseWriter, _ *http.Request) bool {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"code":"`+code+`","message":"words that must never be matched"}}`)
		return true
	}
}

func TestSubmitSendsContractShapeAndKeepsReceiptPrivately(t *testing.T) {
	st := &stub{}
	svc, _ := setup(t, st)
	d := draft()
	d.Contact = " kim@example.com "
	got, err := svc.Submit(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if got.Receipt != "FB-7K3M-9QX2" || got.Status != StatusReceived || got.Redacted {
		t.Fatalf("receipt = %+v", got)
	}
	p := st.posts[0]
	if p.InstallID == "" || p.IdempotencyKey == "" || p.Category != Bug || p.Body != d.Body || p.DisplayName != "kim" || p.Contact != "kim@example.com" {
		t.Fatalf("payload = %+v", p)
	}
	if p.Env.Surface != "studio" || p.Env.OS != runtime.GOOS || p.Env.Arch != runtime.GOARCH || p.Env.ProviderKind != "deepseek" || p.Env.Channel == "" {
		t.Fatalf("env = %+v", p.Env)
	}
	raw, _ := json.Marshal(p)
	for _, forbidden := range []string{"apiKey", "baseUrl", "base_url", "api_key"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("payload carries %s: %s", forbidden, raw)
		}
	}
	file := filepath.Join(svc.store.path)
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %v", info.Mode().Perm())
	}
	loaded, _ := svc.store.load()
	if loaded.InstallToken != stubToken(st.posts[0].InstallID) || loaded.DisplayName != "kim" || len(loaded.Items) != 1 || loaded.Items[0].Receipt != got.Receipt {
		t.Fatalf("state = %+v", loaded)
	}
	if svc.DisplayName() != "kim" {
		t.Fatalf("display name = %q", svc.DisplayName())
	}
}

func TestSubmitRedactsLocallyBeforeAnythingLeaves(t *testing.T) {
	st := &stub{}
	svc, _ := setup(t, st)
	d := draft()
	d.Body = "crash at /Users/alice/proj, api_key=sk-abcdefghijklmnopqrstuvwxyz mail me a@b.co"
	got, err := svc.Submit(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	sent := st.posts[0].Body
	for _, leak := range []string{"alice", "sk-abcdef", "a@b.co"} {
		if strings.Contains(sent, leak) {
			t.Fatalf("%q reached the wire: %s", leak, sent)
		}
	}
	if !got.Redacted {
		t.Fatal("receipt did not say the text was masked")
	}
}

func TestInstallIdentityIsStableAcrossSubmits(t *testing.T) {
	st := &stub{}
	svc, _ := setup(t, st)
	for range 2 {
		if _, err := svc.Submit(context.Background(), draft()); err != nil {
			t.Fatal(err)
		}
	}
	if st.posts[0].InstallID != st.posts[1].InstallID {
		t.Fatal("install id changed between submits")
	}
	if st.posts[0].IdempotencyKey == st.posts[1].IdempotencyKey {
		t.Fatal("two reports shared an idempotency key")
	}
}

func TestRetryAfterAnUnansweredRequestReusesTheKey(t *testing.T) {
	st := &stub{postFn: func(n int, w http.ResponseWriter, _ *http.Request) bool {
		if n > 1 {
			return false
		}
		hj := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
		return true
	}}
	srv := httptest.NewServer(st.handler())
	defer srv.Close()
	svc, _ := New(Config{Home: t.TempDir(), Base: srv.URL, HTTP: srv.Client(), Backoff: []time.Duration{time.Millisecond}})
	if _, err := svc.Submit(context.Background(), draft()); err != nil {
		t.Fatal(err)
	}
	if len(st.posts) != 2 || st.posts[0].IdempotencyKey != st.posts[1].IdempotencyKey {
		t.Fatalf("posts = %d, keys differ or missing", len(st.posts))
	}
}

func TestCallerSuppliedKeyIsForwardedAndReplayIsAccepted(t *testing.T) {
	st := &stub{postFn: func(_ int, w http.ResponseWriter, _ *http.Request) bool {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"receipt":"FB-7K3M-9QX2","status":"recorded","installToken":"tok-x","createdAt":"2026-09-30T08:00:00Z"}`)
		return true
	}}
	svc, _ := setup(t, st)
	d := draft()
	d.IdempotencyKey = "11111111-2222-4333-8444-555555555555"
	got, err := svc.Submit(context.Background(), d)
	if err != nil || got.Status != StatusRecorded {
		t.Fatalf("replay: %+v %v", got, err)
	}
	if st.posts[0].IdempotencyKey != d.IdempotencyKey {
		t.Fatal("key was replaced")
	}
}

func TestServiceCodesBecomeSentinels(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   error
	}{
		{413, "feedback.too_large", ErrTooLarge},
		{429, "feedback.rate_limited", ErrRateLimited},
		{400, "feedback.invalid", ErrInvalid},
		{503, "feedback.disabled", ErrDisabled},
		{409, "feedback.duplicate", ErrDuplicate},
		{401, "feedback.bad_token", ErrBadToken},
		{503, "feedback.busy", ErrBusy},
		{400, "feedback.image_metadata", ErrImageMetadata},
	}
	for _, c := range cases {
		st := &stub{postFn: apiError(c.status, c.code)}
		svc, _ := setup(t, st)
		_, err := svc.Submit(context.Background(), draft())
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.code, err, c.want)
		}
		want := 1
		if errors.Is(c.want, ErrBadToken) {
			want = 2
		}
		if len(st.posts) != want {
			t.Errorf("%s: %d posts, want %d (a coded refusal is not retried as is)", c.code, len(st.posts), want)
		}
	}
}

func TestRateLimitCarriesRetryAfter(t *testing.T) {
	st := &stub{postFn: func(_ int, w http.ResponseWriter, _ *http.Request) bool {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":"feedback.rate_limited"}}`)
		return true
	}}
	svc, _ := setup(t, st)
	_, err := svc.Submit(context.Background(), draft())
	if RetryAfter(err) != 2*time.Minute {
		t.Fatalf("retry after = %v (%v)", RetryAfter(err), err)
	}
}

func TestUncodedGatewayFailureIsUnavailableNotAGuess(t *testing.T) {
	st := &stub{postFn: func(_ int, w http.ResponseWriter, _ *http.Request) bool {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "<html>bad gateway feedback.rate_limited</html>")
		return true
	}}
	svc, _ := setup(t, st)
	if _, err := svc.Submit(context.Background(), draft()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnreachableServiceIsOfflineAndKeepsNothing(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	svc, _ := New(Config{Home: t.TempDir(), Base: base, Backoff: []time.Duration{}})
	_, err := svc.Submit(context.Background(), draft())
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("err = %v", err)
	}
	if loaded, _ := svc.store.load(); len(loaded.Items) != 0 {
		t.Fatal("a failed submit left a receipt behind")
	}
}

func TestCancelledContextIsNotOffline(t *testing.T) {
	st := &stub{}
	svc, _ := setup(t, st)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.Submit(ctx, draft())
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrOffline) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientRefusesBeforeSending(t *testing.T) {
	st := &stub{}
	svc, _ := setup(t, st)
	long := draft()
	long.Body = strings.Repeat("a b ", 3000)
	cases := map[string]struct {
		mut    func(*Draft)
		field  string
		reason string
	}{
		"empty body":   {func(d *Draft) { d.Body = "  " }, FieldBody, ReasonEmpty},
		"long body":    {func(d *Draft) { d.Body = long.Body }, FieldBody, ReasonTooLong},
		"no name":      {func(d *Draft) { d.DisplayName = "" }, FieldDisplayName, ReasonEmpty},
		"long name":    {func(d *Draft) { d.DisplayName = strings.Repeat("名", 41) }, FieldDisplayName, ReasonTooLong},
		"long contact": {func(d *Draft) { d.Contact = strings.Repeat("x", 121) }, FieldContact, ReasonTooLong},
		"bad category": {func(d *Draft) { d.Category = "rant" }, FieldCategory, ReasonBadValue},
	}
	for name, c := range cases {
		d := draft()
		c.mut(&d)
		_, err := svc.Submit(context.Background(), d)
		var ie *InvalidError
		if !errors.Is(err, ErrInvalid) || !errors.As(err, &ie) || ie.Field != c.field || ie.Reason != c.reason {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(st.posts) != 0 {
		t.Fatal("an invalid draft reached the network")
	}
}

func TestNameOfExactlyTheLimitIsAccepted(t *testing.T) {
	st := &stub{}
	svc, _ := setup(t, st)
	d := draft()
	d.DisplayName = strings.Repeat("名", 40)
	if _, err := svc.Submit(context.Background(), d); err != nil {
		t.Fatal(err)
	}
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 90, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func noisePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	rng := rand.New(rand.NewSource(7))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	rng.Read(img.Pix)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegWithEXIF(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 32, 32)), nil); err != nil {
		t.Fatal(err)
	}
	raw := b.Bytes()
	exif := append([]byte("Exif\x00\x00"), []byte("GPS-52.5200N-13.4050E")...)
	seg := append([]byte{0xFF, 0xE1, byte((len(exif) + 2) >> 8), byte(len(exif) + 2)}, exif...)
	return append(append(append([]byte{}, raw[:2]...), seg...), raw[2:]...)
}

func TestNormalizeStripsMetadataAndKeepsFormat(t *testing.T) {
	in := jpegWithEXIF(t)
	if !bytes.Contains(in, []byte("GPS-52")) {
		t.Fatal("fixture lost its EXIF")
	}
	out, err := Normalize([]Image{{Name: `C:\tmp\holiday photo.JPEG`, Data: in}}, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out[0].Data, []byte("GPS-52")) || bytes.Contains(out[0].Data, []byte("Exif")) {
		t.Fatal("EXIF survived")
	}
	if out[0].ContentType != "image/jpeg" || out[0].Name != "holiday photo.jpg" {
		t.Fatalf("attachment = %s %s", out[0].ContentType, out[0].Name)
	}
}

func TestNormalizeDownscalesToTheBudget(t *testing.T) {
	big := noisePNG(t, 1500, 1500)
	if len(big) <= DefaultLimits.ImageBytes {
		t.Fatalf("fixture only %d bytes", len(big))
	}
	lim := DefaultLimits
	lim.UploadBytes = 64 << 20
	out, err := Normalize([]Image{{Name: "big.png", Data: big}}, lim)
	if err != nil {
		t.Fatal(err)
	}
	if len(out[0].Data) > lim.ImageBytes {
		t.Fatalf("still %d bytes", len(out[0].Data))
	}
	if _, _, err := image.Decode(bytes.NewReader(out[0].Data)); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeRefusals(t *testing.T) {
	var g bytes.Buffer
	_ = gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 4, 4), []color.Color{color.Black, color.White}), nil)
	small := pngBytes(t, 8, 8)
	renamed := Image{Name: "shot.png", Data: []byte("MZ this is an exe")}
	cases := map[string]struct {
		in     []Image
		reason string
	}{
		"gif":         {[]Image{{Name: "a.png", Data: g.Bytes()}}, ReasonFormat},
		"not image":   {[]Image{renamed}, ReasonFormat},
		"four":        {[]Image{{Data: small}, {Data: small}, {Data: small}, {Data: small}}, ReasonTooMany},
		"empty":       {[]Image{{Name: "x.png"}}, ReasonUndecodable},
		"truncated":   {[]Image{{Data: small[:len(small)/2]}}, ReasonUndecodable},
		"upload size": {[]Image{{Data: append(append([]byte{}, small...), make([]byte, DefaultLimits.UploadBytes)...)}}, ReasonTooLarge},
	}
	for name, c := range cases {
		_, err := Normalize(c.in, DefaultLimits)
		var ie *InvalidError
		if !errors.Is(err, ErrInvalid) || !errors.As(err, &ie) || ie.Field != FieldImages || ie.Reason != c.reason {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestSubmitCarriesNormalizedAttachments(t *testing.T) {
	st := &stub{}
	svc, _ := setup(t, st)
	d := draft()
	d.Images = []Image{{Name: "a.png", Data: pngBytes(t, 40, 40)}, {Name: "b.jpg", Data: jpegWithEXIF(t)}}
	if _, err := svc.Submit(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	att := st.posts[0].Attachments
	if len(att) != 2 || att[0].ContentType != "image/png" || att[1].ContentType != "image/jpeg" || att[0].DataBase64 == "" {
		t.Fatalf("attachments = %+v", att)
	}
}

func TestListMineBeforeAnySubmitAsksNobody(t *testing.T) {
	st := &stub{}
	svc, _ := setup(t, st)
	got, err := svc.ListMine(context.Background())
	if err != nil || got.Offline || len(got.Items) != 0 || got.Items == nil {
		t.Fatalf("mine = %+v %v", got, err)
	}
	if len(st.gets) != 0 {
		t.Fatal("asked the service with no identity")
	}
}

func TestListMineSendsIdentityAndMergesServerState(t *testing.T) {
	st := &stub{mineBody: `{"items":[{"receipt":"FB-7K3M-9QX2","category":"bug","titleSnippet":"Sidebar loses selection","status":"fixed","issueNumber":11350,"issueUrl":"https://github.com/esengine/DeepSeek-Reasonix/issues/11350","resolvedVersion":"v2.25.0","createdAt":"2026-09-30T08:00:00Z","updatedAt":"2026-10-02T08:00:00Z"}]}`}
	svc, _ := setup(t, st)
	if _, err := svc.Submit(context.Background(), draft()); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ListMine(context.Background())
	if err != nil || got.Offline || len(got.Items) != 1 {
		t.Fatalf("mine = %+v %v", got, err)
	}
	it := got.Items[0]
	if it.Status != StatusFixed || it.IssueNumber == nil || *it.IssueNumber != 11350 || it.ResolvedVersion != "v2.25.0" {
		t.Fatalf("item = %+v", it)
	}
	h := st.gets[0]
	if h.Get("X-Install-Id") != st.posts[0].InstallID || h.Get("X-Install-Token") != stubToken(st.posts[0].InstallID) {
		t.Fatalf("headers = %v", h)
	}
}

func TestListMineOfflineAnswersFromLocalReceipts(t *testing.T) {
	st := &stub{}
	svc, srv := setup(t, st)
	if _, err := svc.Submit(context.Background(), draft()); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	got, err := svc.ListMine(context.Background())
	if err != nil || !got.Offline || len(got.Items) != 1 || got.Items[0].Receipt != "FB-7K3M-9QX2" {
		t.Fatalf("mine = %+v %v", got, err)
	}
}

func TestBadTokenRetiresTheIdentityAndKeepsHistoryMarked(t *testing.T) {
	st := &stub{mineCode: 401, mineBody: `{"error":{"code":"feedback.bad_token"}}`}
	svc, _ := setup(t, st)
	if _, err := svc.Submit(context.Background(), draft()); err != nil {
		t.Fatal(err)
	}
	first := st.posts[0].InstallID
	got, err := svc.ListMine(context.Background())
	if err != nil || got.Offline || len(got.Items) != 1 || !got.Items[0].StatusUnavailable {
		t.Fatalf("mine = %+v %v", got, err)
	}
	if again, _ := svc.ListMine(context.Background()); len(st.gets) != 1 || len(again.Items) != 1 {
		t.Fatalf("a retired identity still asked the service (%d gets)", len(st.gets))
	}
	if _, err := svc.Submit(context.Background(), draft()); err != nil {
		t.Fatal(err)
	}
	if st.posts[1].InstallID == first || st.posts[1].InstallID == "" {
		t.Fatal("the next report reused the retired install id")
	}
	loaded, _ := svc.store.load()
	if len(loaded.Items) != 2 || loaded.Items[0].StatusUnavailable || !loaded.Items[1].StatusUnavailable {
		t.Fatalf("items after the new submit = %+v", loaded.Items)
	}
}

func TestEnvCarriesNoSecretsAndNamesTheSurface(t *testing.T) {
	env := CollectEnv(EnvContext{Surface: SurfaceTUI, Locale: "zh_CN.UTF-8", ProviderKind: "some-vendor"})
	if env.Surface != "tui" || env.Locale != "zh-CN" || env.ProviderKind != "other" {
		t.Fatalf("env = %+v", env)
	}
	raw, _ := json.Marshal(env)
	if strings.Contains(strings.ToLower(string(raw)), "key") || strings.Contains(string(raw), "http") {
		t.Fatalf("env leaks: %s", raw)
	}
}

func TestBaseComesFromEnvironmentWhenUnset(t *testing.T) {
	t.Setenv(baseEnv, "http://127.0.0.1:9/")
	svc, err := New(Config{Home: t.TempDir(), HTTP: http.DefaultClient})
	if err != nil || svc.base != "http://127.0.0.1:9" {
		t.Fatalf("base = %q %v", svc.base, err)
	}
}

func pngChunks(t *testing.T, data []byte) []string {
	t.Helper()
	var kinds []string
	for pos := 8; pos+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[pos:]))
		kinds = append(kinds, string(data[pos+4:pos+8]))
		pos += 12 + size
	}
	return kinds
}

func withPNGChunk(t *testing.T, src []byte, kind string, payload []byte) []byte {
	t.Helper()
	chunk := make([]byte, 8, 12+len(payload))
	binary.BigEndian.PutUint32(chunk, uint32(len(payload)))
	copy(chunk[4:], kind)
	chunk = append(chunk, payload...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	return append(append(append([]byte{}, src[:33]...), chunk...), src[33:]...)
}

func TestNormalizeRedrawsPNGsCarryingMetadata(t *testing.T) {
	plain := pngBytes(t, 16, 16)
	dirty := withPNGChunk(t, plain, "iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00<x:xmpmeta/>"))
	dirty = withPNGChunk(t, dirty, "eXIf", []byte("MM\x00*GPS"))
	dirty = withPNGChunk(t, dirty, "tEXt", []byte("Author\x00kim"))
	dirty = withPNGChunk(t, dirty, "tIME", make([]byte, 7))
	dirty = append(dirty, []byte("trailing-secret")...)
	if got := pngChunks(t, dirty); !slices.Contains(got, "iTXt") {
		t.Fatalf("fixture chunks = %v", got)
	}
	out, err := Normalize([]Image{{Name: "shot.png", Data: dirty}}, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range pngChunks(t, out[0].Data) {
		if kind != "IHDR" && kind != "IDAT" && kind != "IEND" {
			t.Fatalf("chunk %s survived the re-encode", kind)
		}
	}
	if bytes.Contains(out[0].Data, []byte("trailing-secret")) || bytes.Contains(out[0].Data, []byte("GPS")) {
		t.Fatal("hidden bytes survived")
	}
	if !metadataFree(out[0].Data, out[0].ContentType) {
		t.Fatal("self-check disagrees with the chunk walk")
	}
}

func TestJPEGSegmentsOtherThanPixelsAreGone(t *testing.T) {
	in := jpegWithEXIF(t)
	com := append([]byte{0xFF, 0xFE, 0x00, 0x08}, []byte("secret")...)
	in = append(append(append([]byte{}, in[:2]...), com...), in[2:]...)
	out, err := Normalize([]Image{{Name: "a.jpg", Data: in}}, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !jpegClean(out[0].Data) || bytes.Contains(out[0].Data, []byte("secret")) {
		t.Fatal("a comment or app segment survived")
	}
}

func TestSelfCheckRefusesWhatItCannotProveClean(t *testing.T) {
	if metadataFree(withPNGChunk(t, pngBytes(t, 4, 4), "tEXt", []byte("a\x00b")), "image/png") {
		t.Fatal("png with tEXt passed")
	}
	if metadataFree(append(pngBytes(t, 4, 4), 0), "image/png") {
		t.Fatal("png with trailing byte passed")
	}
	if metadataFree(jpegWithEXIF(t), "image/jpeg") {
		t.Fatal("jpeg with APP1 passed")
	}
	if metadataFree([]byte("nope"), "image/jpeg") || metadataFree(nil, "image/png") {
		t.Fatal("garbage passed")
	}
}

func TestWebKitStyleExportsComeOutOnTheAllowList(t *testing.T) {
	dirty := pngBytes(t, 24, 24)
	for _, c := range []struct {
		kind    string
		payload []byte
	}{
		{"sRGB", []byte{0}},
		{"iCCP", append([]byte("Display P3\x00\x00"), make([]byte, 16)...)},
		{"pHYs", make([]byte, 9)},
		{"eXIf", []byte("MM\x00*\x00\x00\x00\x08GPSLatitude")},
		{"iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00<x:xmpmeta/>")},
	} {
		dirty = withPNGChunk(t, dirty, c.kind, c.payload)
	}
	png, err := Normalize([]Image{{Name: "safari.png", Data: dirty}}, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !metadataFree(png[0].Data, png[0].ContentType) || bytes.Contains(png[0].Data, []byte("GPSLatitude")) {
		t.Fatal("a WebKit-style PNG kept a block")
	}

	in := jpegWithEXIF(t)
	iptc := append([]byte{0xFF, 0xED, 0x00, 0x0E}, []byte("Photoshop 3.0")...)
	icc := append([]byte{0xFF, 0xE2, 0x00, 0x0A}, []byte("ICC_PROF")...)
	in = append(append(append(append([]byte{}, in[:2]...), iptc...), icc...), in[2:]...)
	jpg, err := Normalize([]Image{{Name: "photo.jpg", Data: in}}, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !jpegClean(jpg[0].Data) || bytes.Contains(jpg[0].Data, []byte("Photoshop")) || bytes.Contains(jpg[0].Data, []byte("GPS-52")) {
		t.Fatal("a WebKit-style JPEG kept a segment")
	}
}

func TestASecondSubmitCarriesTheInstallToken(t *testing.T) {
	st := &stub{}
	svc, _ := setup(t, st)
	for range 2 {
		if _, err := svc.Submit(context.Background(), draft()); err != nil {
			t.Fatal(err)
		}
	}
	if st.tokens[0] != "" || st.tokens[1] != stubToken(st.posts[0].InstallID) {
		t.Fatalf("tokens sent = %q", st.tokens)
	}
}

// The worker records the report and the answer never arrives.
func lostFirstAnswer(st *stub) func(int, http.ResponseWriter, *http.Request) bool {
	return func(n int, w http.ResponseWriter, r *http.Request) bool {
		if n != 1 {
			return false
		}
		st.admit(httptest.NewRecorder(), r, st.posts[0])
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
		return true
	}
}

func TestARerunOfALostSendReusesItsKeyAndRecoversTheToken(t *testing.T) {
	st := &stub{}
	st.postFn = lostFirstAnswer(st)
	svc, _ := setup(t, st)
	if _, err := svc.Submit(context.Background(), draft()); !errors.Is(err, ErrOffline) {
		t.Fatalf("first send: %v", err)
	}
	got, err := svc.Submit(context.Background(), draft())
	if err != nil {
		t.Fatal(err)
	}
	if st.posts[1].IdempotencyKey != st.posts[0].IdempotencyKey || st.posts[1].InstallID != st.posts[0].InstallID {
		t.Fatal("the rerun did not reuse the unfinished send's key")
	}
	if len(st.rows) != 1 || got.Receipt != st.rows[0].receipt {
		t.Fatalf("the rerun filed a second report: %+v", st.rows)
	}
	if loaded, _ := svc.store.load(); loaded.InstallToken == "" || loaded.Pending != nil {
		t.Fatalf("state after recovery = %+v", loaded)
	}
	if _, err := svc.Submit(context.Background(), Draft{Category: Idea, Body: "another", DisplayName: "kim", Env: draft().Env}); err != nil {
		t.Fatal(err)
	}
	if len(st.rows) != 2 {
		t.Fatalf("rows = %d", len(st.rows))
	}
}

func TestADifferentTextGetsANewKey(t *testing.T) {
	st := &stub{}
	st.postFn = lostFirstAnswer(st)
	svc, _ := setup(t, st)
	_, _ = svc.Submit(context.Background(), draft())
	other := draft()
	other.Body = "something else entirely"
	if _, err := svc.Submit(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if st.posts[1].IdempotencyKey == st.posts[0].IdempotencyKey {
		t.Fatal("a different report reused the key")
	}
}

// The first answer is lost and the rerun cannot present the old key (a caller
// that mints its own): the worker now wants a token this machine never got.
func TestLostTokenOnSubmitRegistersANewIdentity(t *testing.T) {
	st := &stub{}
	st.postFn = lostFirstAnswer(st)
	svc, _ := setup(t, st)
	first := draft()
	first.IdempotencyKey = "aaaaaaaa-1"
	_, _ = svc.Submit(context.Background(), first)
	second := draft()
	second.IdempotencyKey = "bbbbbbbb-2"
	if _, err := svc.Submit(context.Background(), second); err != nil {
		t.Fatalf("submit after a lost token: %v", err)
	}
	if st.posts[1].InstallID == st.posts[2].InstallID || len(st.rows) != 2 {
		t.Fatalf("identity was not replaced (%d rows)", len(st.rows))
	}
	if loaded, _ := svc.store.load(); loaded.InstallToken != stubToken(st.posts[2].InstallID) {
		t.Fatalf("token = %q", loaded.InstallToken)
	}
}

func TestTransientSubmitResponseRetryIsBounded(t *testing.T) {
	for name, fn := range map[string]func(int, http.ResponseWriter, *http.Request) bool{
		"uncoded 502": func(_ int, w http.ResponseWriter, _ *http.Request) bool {
			w.WriteHeader(http.StatusBadGateway)
			return true
		},
		"coded 503": apiError(http.StatusServiceUnavailable, "feedback.disabled"),
		"429":       apiError(http.StatusTooManyRequests, "feedback.rate_limited"),
	} {
		st := &stub{postFn: fn}
		srv := httptest.NewServer(st.handler())
		svc, _ := New(Config{Home: t.TempDir(), Base: srv.URL, HTTP: srv.Client(), Backoff: []time.Duration{time.Millisecond, time.Millisecond}})
		_, _ = svc.Submit(context.Background(), draft())
		srv.Close()
		want := 1
		if name == "uncoded 502" {
			want = 2
		}
		if len(st.posts) != want {
			t.Errorf("%s: %d posts, want %d", name, len(st.posts), want)
		}
	}
}

func TestNicknameIsRedactedOnTheWireAndWhenRemembered(t *testing.T) {
	st := &stub{}
	svc, _ := setup(t, st)
	d := draft()
	d.DisplayName = "a@b.co"
	if _, err := svc.Submit(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if st.posts[0].DisplayName != "[redacted-email]" {
		t.Fatalf("nickname on the wire = %q", st.posts[0].DisplayName)
	}
	if err := svc.SetDisplayName("x@y.io"); err != nil || svc.DisplayName() != "[redacted-email]" {
		t.Fatalf("remembered = %q %v", svc.DisplayName(), err)
	}
}

func TestTheMetadataGateStopsWhatItCannotProveClean(t *testing.T) {
	old := clean
	clean = func([]byte, string) bool { return false }
	defer func() { clean = old }()
	_, err := Normalize([]Image{{Name: "a.png", Data: pngBytes(t, 8, 8)}}, DefaultLimits)
	if !errors.Is(err, ErrImageMetadata) {
		t.Fatalf("err = %v", err)
	}
}

func TestBaseURLPolicy(t *testing.T) {
	for _, bad := range []string{"http://example.com", "ftp://x", "https://u:p@example.com", "example.com", "ftp://127.0.0.1:9"} {
		if _, err := New(Config{Home: t.TempDir(), Base: bad, HTTP: http.DefaultClient}); err == nil {
			t.Errorf("base %q accepted", bad)
		}
	}
	for _, ok := range []string{"https://example.com", "http://127.0.0.1:9", "http://localhost:9", "http://[::1]:9"} {
		if _, err := New(Config{Home: t.TempDir(), Base: ok, HTTP: http.DefaultClient}); err != nil {
			t.Errorf("base %q refused: %v", ok, err)
		}
	}
	t.Setenv(baseEnv, "http://evil.example.com")
	svc, err := New(Config{Home: t.TempDir(), HTTP: http.DefaultClient})
	if err != nil || svc.base != defaultBase {
		t.Fatalf("an insecure environment override was honoured: %q %v", svc.base, err)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	var hit int
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit++ }))
	defer elsewhere.Close()
	moved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/v1/feedback", http.StatusTemporaryRedirect)
	}))
	defer moved.Close()
	svc, err := New(Config{Home: t.TempDir(), Base: moved.URL, Backoff: []time.Duration{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(context.Background(), draft()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if hit != 0 {
		t.Fatal("the body and install headers followed a redirect to another host")
	}
}

func TestEnvFieldsStayWithinTheSchema(t *testing.T) {
	env := CollectEnv(EnvContext{Surface: SurfaceStudio, Locale: strings.Repeat("x", 60)})
	if len([]rune(env.Locale)) != 20 {
		t.Fatalf("locale = %q", env.Locale)
	}
}

func exifJPEG(t *testing.T, w, h, orientation int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
			if x >= w/2 {
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, &jpeg.Options{Quality: 100})
	raw := b.Bytes()
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, byte(orientation), 0, 0, 0, 0, 0, 0}
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := append([]byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}, payload...)
	return append(append(append([]byte{}, raw[:2]...), seg...), raw[2:]...)
}

func TestExifOrientationIsAppliedBeforeMetadataIsDropped(t *testing.T) {
	out, err := Normalize([]Image{{Name: "p.jpg", Data: exifJPEG(t, 32, 16, 6)}}, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := image.Decode(bytes.NewReader(out[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	if b := got.Bounds(); b.Dx() != 16 || b.Dy() != 32 {
		t.Fatalf("orientation 6 left a %dx%d image", b.Dx(), b.Dy())
	}
	// Left half red, right half blue; rotated 90 degrees clockwise the red half is on top.
	if r, _, _, _ := got.At(8, 4).RGBA(); r>>8 < 200 {
		t.Fatal("the top of the rotated image is not the original left half")
	}
	same, _ := Normalize([]Image{{Name: "p.jpg", Data: exifJPEG(t, 32, 16, 1)}}, DefaultLimits)
	if g, _, _ := image.Decode(bytes.NewReader(same[0].Data)); g.Bounds().Dx() != 32 {
		t.Fatal("orientation 1 was rotated")
	}
}

func TestOversizedDecodeIsRefusedBeforeItAllocates(t *testing.T) {
	raw := pngBytes(t, 8, 8)
	binary.BigEndian.PutUint32(raw[16:], 7000)
	binary.BigEndian.PutUint32(raw[20:], 7000)
	binary.BigEndian.PutUint32(raw[29:], crc32.ChecksumIEEE(raw[12:29]))
	_, err := Normalize([]Image{{Name: "big.png", Data: raw}}, DefaultLimits)
	var ie *InvalidError
	if !errors.As(err, &ie) || ie.Reason != ReasonUndecodable {
		t.Fatalf("err = %v", err)
	}
}
