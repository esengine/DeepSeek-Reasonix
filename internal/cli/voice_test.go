package cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
)

func TestWavFromPCMHeader(t *testing.T) {
	pcm := make([]byte, 3200) // 100ms at 16kHz mono s16le
	got := wavFromPCM(pcm)

	if len(got) != 44+len(pcm) {
		t.Fatalf("length = %d, want %d", len(got), 44+len(pcm))
	}
	if string(got[0:4]) != "RIFF" || string(got[8:12]) != "WAVE" {
		t.Fatalf("bad magic: %q / %q", got[0:4], got[8:12])
	}
	if string(got[12:16]) != "fmt " || string(got[36:40]) != "data" {
		t.Fatalf("bad chunk ids: %q / %q", got[12:16], got[36:40])
	}

	// The two size fields are what a decoder trusts; getting them wrong is the
	// exact failure mode that streaming an in-progress WAV would cause.
	if riff := binary.LittleEndian.Uint32(got[4:8]); riff != uint32(36+len(pcm)) {
		t.Errorf("RIFF size = %d, want %d", riff, 36+len(pcm))
	}
	if data := binary.LittleEndian.Uint32(got[40:44]); data != uint32(len(pcm)) {
		t.Errorf("data size = %d, want %d", data, len(pcm))
	}

	if ch := binary.LittleEndian.Uint16(got[22:24]); ch != voiceChannels {
		t.Errorf("channels = %d, want %d", ch, voiceChannels)
	}
	if sr := binary.LittleEndian.Uint32(got[24:28]); sr != voiceSampleRate {
		t.Errorf("sample rate = %d, want %d", sr, voiceSampleRate)
	}
	if bits := binary.LittleEndian.Uint16(got[34:36]); bits != voiceBitsPerSam {
		t.Errorf("bits = %d, want %d", bits, voiceBitsPerSam)
	}
	if !bytes.Equal(got[44:], pcm) {
		t.Error("payload not preserved")
	}
}

// TestTranscribeRequestShape pins what actually goes on the wire, against a
// stub standing in for any OpenAI-compatible provider.
func TestTranscribeRequestShape(t *testing.T) {
	t.Setenv("VOICE_TEST_KEY", "sk-secret")

	var (
		gotAuth   string
		gotHeader string
		gotFields = map[string]string{}
		gotFile   []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHeader = r.Header.Get("X-Gateway")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		for k, v := range r.MultipartForm.Value {
			gotFields[k] = v[0]
		}
		if fh := r.MultipartForm.File["file"]; len(fh) == 1 {
			f, _ := fh[0].Open()
			gotFile, _ = io.ReadAll(f)
			f.Close()
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"text":"hello there"}`)
	}))
	defer srv.Close()

	cfg := config.VoiceConfig{
		URL:         srv.URL,
		Model:       "whisper-1",
		APIKeyEnv:   "VOICE_TEST_KEY",
		Headers:     map[string]string{"X-Gateway": "acme", "Content-Type": "text/plain"},
		Language:    "en",
		Prompt:      "Reasonix, kubectl",
		Temperature: 0.2,
	}.WithDefaults()

	text, err := transcribe(context.Background(), cfg, make([]byte, 3200))
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if text != "hello there" {
		t.Errorf("text = %q", text)
	}
	if gotAuth != "Bearer sk-secret" {
		t.Errorf("Authorization = %q, want bearer token", gotAuth)
	}
	if gotHeader != "acme" {
		t.Errorf("X-Gateway = %q, want custom header forwarded", gotHeader)
	}
	for k, want := range map[string]string{
		"model": "whisper-1", "response_format": "json",
		"language": "en", "prompt": "Reasonix, kubectl", "temperature": "0.2",
	} {
		if gotFields[k] != want {
			t.Errorf("field %s = %q, want %q", k, gotFields[k], want)
		}
	}
	// A custom header must never be able to break the multipart encoding.
	if len(gotFile) != 44+3200 {
		t.Errorf("uploaded file = %d bytes, want a well-formed WAV", len(gotFile))
	}
}

// A configured-but-unset credential must fail by name, not as a bare 401.
func TestTranscribeMissingCredential(t *testing.T) {
	cfg := config.VoiceConfig{URL: "http://127.0.0.1:1", APIKeyEnv: "VOICE_ABSENT_KEY"}.WithDefaults()
	_, err := transcribe(context.Background(), cfg, make([]byte, 3200))
	if err == nil || !strings.Contains(err.Error(), "VOICE_ABSENT_KEY") {
		t.Fatalf("err = %v, want it to name the missing credential", err)
	}
}

// No URL must be a clear config error, not an attempt to dial something.
func TestTranscribeRequiresURL(t *testing.T) {
	if _, err := transcribe(context.Background(), config.VoiceConfig{}.WithDefaults(), make([]byte, 3200)); err == nil {
		t.Fatal("want an error when url is unset")
	}
	if err := (config.VoiceConfig{}).WithDefaults().Validate(); err == nil {
		t.Fatal("Validate should reject an empty url")
	}
}

func TestVoiceConfigDefaults(t *testing.T) {
	v := config.VoiceConfig{}.WithDefaults()
	// URL intentionally has NO default — see ErrVoiceURLUnset.
	if v.URL != "" {
		t.Errorf("url should have no default, got %q", v.URL)
	}
	if v.Model == "" {
		t.Fatalf("defaults incomplete: %+v", v)
	}
	if got := v.PollInterval(); got != 500*time.Millisecond {
		t.Errorf("PollInterval = %v, want 500ms", got)
	}
	// A too-fast cadence must be floored, not honoured — it would burn GPU for
	// partials arriving faster than anyone can read.
	fast := config.VoiceConfig{PollMS: 10}.WithDefaults()
	if fast.PollInterval() < 200*time.Millisecond {
		t.Errorf("PollInterval floor not applied: %v", fast.PollInterval())
	}
	if none := (config.VoiceConfig{MaxSeconds: -1}).WithDefaults(); none.MaxDuration() <= 0 {
		t.Error("MaxDuration should fall back to a positive default")
	}
}

func TestSnapshotTailWindow(t *testing.T) {
	s := &voiceSession{pcm: make([]byte, voiceBytesPerSec*10)} // 10s
	for i := range s.pcm {
		s.pcm[i] = byte(i)
	}

	full, total := s.snapshot(0)
	if len(full) != voiceBytesPerSec*10 || total != voiceBytesPerSec*10 {
		t.Fatalf("full snapshot = %d (total %d)", len(full), total)
	}

	// Partials must be clamped to the window, and must keep the NEWEST audio —
	// taking the head would freeze the transcript at the start of the recording.
	tail, total := s.snapshot(3 * time.Second)
	if want := voiceBytesPerSec * 3; len(tail) != want {
		t.Fatalf("tail snapshot = %d, want %d", len(tail), want)
	}
	if total != voiceBytesPerSec*10 {
		t.Errorf("total = %d, want full buffer length", total)
	}
	if !bytes.Equal(tail, s.pcm[len(s.pcm)-voiceBytesPerSec*3:]) {
		t.Error("tail window did not take the most recent audio")
	}
}

func TestSingleFlightClaim(t *testing.T) {
	s := &voiceSession{}
	if !s.tryClaim() {
		t.Fatal("first claim should succeed")
	}
	if s.tryClaim() {
		t.Fatal("second claim must fail while one is in flight")
	}
	s.release()
	if !s.tryClaim() {
		t.Fatal("claim should succeed after release")
	}
}

// TestTranscribeLive exercises the real client against a real ASR endpoint.
// Opt-in: set REASONIX_VOICE_PCM to a raw s16le/16kHz/mono file, and optionally
// REASONIX_VOICE_URL and REASONIX_VOICE_WANT (a word expected in the transcript).
func TestTranscribeLive(t *testing.T) {
	pcmPath := os.Getenv("REASONIX_VOICE_PCM")
	if pcmPath == "" {
		t.Skip("set REASONIX_VOICE_PCM to run the live ASR test")
	}
	pcm, err := os.ReadFile(pcmPath)
	if err != nil {
		t.Fatalf("read pcm: %v", err)
	}

	cfg := config.VoiceConfig{URL: os.Getenv("REASONIX_VOICE_URL")}.WithDefaults()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	audioSec := float64(len(pcm)) / float64(voiceBytesPerSec)

	// Four passes: the first pays connection setup and any server cold path, the
	// rest are what the poll loop actually experiences. The steady-state number
	// is the one that has to fit inside the poll interval.
	var text string
	for i := 0; i < 4; i++ {
		start := time.Now()
		out, err := transcribe(ctx, cfg, pcm)
		if err != nil {
			t.Fatalf("transcribe (pass %d): %v", i, err)
		}
		elapsed := time.Since(start)
		label := "warm"
		if i == 0 {
			label = "cold"
		}
		t.Logf("pass %d (%s): %.1fs audio -> %v", i, label, audioSec, elapsed.Round(time.Millisecond))
		if i > 0 && elapsed > cfg.PollInterval() {
			t.Errorf("warm pass %d took %v, exceeds the %v poll interval — partials would fall behind",
				i, elapsed.Round(time.Millisecond), cfg.PollInterval())
		}
		text = out
	}
	t.Logf("transcript: %q", text)

	if strings.TrimSpace(text) == "" {
		t.Fatal("empty transcript")
	}
	if want := os.Getenv("REASONIX_VOICE_WANT"); want != "" {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Errorf("transcript %q does not contain %q", text, want)
		}
	}
}

// The space bar stringifies as "space", not " " — Key.String() skips the space
// text and falls through to Keystroke(). Getting this wrong makes push-to-talk
// silently inert, so pin it against the real bubbletea types.
func TestVoiceTalkKeyMatchesSpaceBar(t *testing.T) {
	press := tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	if got := press.String(); got != voiceTalkKey {
		t.Fatalf("space press String() = %q, but voiceTalkKey = %q", got, voiceTalkKey)
	}
	release := tea.KeyReleaseMsg{Code: tea.KeySpace, Text: " "}
	if got := release.String(); got != voiceTalkKey {
		t.Fatalf("space release String() = %q, but voiceTalkKey = %q", got, voiceTalkKey)
	}
}

// A held key auto-repeats as further press events. Those must not restart the
// recording, or a hold would capture nothing.
func TestPushToTalkIgnoresAutoRepeat(t *testing.T) {
	m := &chatTUI{voicePTT: true}
	repeat := tea.KeyPressMsg{Code: tea.KeySpace, Text: " ", IsRepeat: true}

	cmd, handled := m.voiceKeyIntercept(repeat)
	if !handled {
		t.Fatal("a repeat of the talk key must be consumed, not passed to the composer")
	}
	if cmd != nil {
		t.Error("a repeat must not start a new capture")
	}
	if m.voice != nil {
		t.Error("a repeat must not open a recording session")
	}
}

// Without terminal support for key releases there is no way to end a hold, so
// /voice must never claim push-to-talk.
func TestPushToTalkRequiresTerminalSupport(t *testing.T) {
	m := &chatTUI{voicePTTSupported: false}
	if m.voicePTT {
		t.Fatal("push-to-talk must start disarmed")
	}
	// Release events cannot arrive in this mode; if one somehow does, ignore it.
	if _, handled := m.voiceKeyRelease(tea.KeyReleaseMsg{Code: tea.KeySpace, Text: " "}); handled {
		t.Error("release must not be claimed when push-to-talk is not armed")
	}
}

// A terminal can answer the enhancement query claiming release reporting and
// then never send one, which would arm a hold nothing can end. Claiming support
// is not enough: a release has to have actually arrived.
func TestPushToTalkNeedsObservedRelease(t *testing.T) {
	claimsButNeverDelivers := &chatTUI{voicePTTSupported: true, voiceSawKeyRelease: false}
	if claimsButNeverDelivers.pttPossible() {
		t.Error("push-to-talk must not arm on the terminal's claim alone")
	}

	honest := &chatTUI{voicePTTSupported: true, voiceSawKeyRelease: true}
	if !honest.pttPossible() {
		t.Error("push-to-talk must arm once a release has actually been seen")
	}

	// Once the watchdog has caught a stuck hold, /voice must not re-arm into it.
	burned := &chatTUI{voicePTTSupported: true, voiceSawKeyRelease: true, voicePTTBroken: true}
	if burned.pttPossible() {
		t.Error("push-to-talk must stay down after a stuck hold")
	}
}

// The watchdog: a hold that runs past voicePTTStuckHold with no release of the
// talk key ever seen means the release is not coming. Commit what was said,
// stand down, and do not re-arm.
func TestPushToTalkWatchdogEndsStuckHold(t *testing.T) {
	m := &chatTUI{voicePTT: true, voicePTTSupported: true, voiceSawKeyRelease: true,
		pendingCommit: &[]string{}}
	s := &voiceSession{seq: 1, cfg: config.VoiceConfig{}.WithDefaults()}
	// Enough buffered PCM to look like a hold past the watchdog threshold.
	s.pcm = make([]byte, int(voicePTTStuckHold.Seconds())*voiceBytesPerSec)
	m.voice = s
	m.voiceSeq = 1

	if _, ok := m.handleVoiceMsg(voiceTickMsg{seq: 1}); !ok {
		t.Fatal("tick must be handled as a voice message")
	}
	if m.voice != nil {
		t.Error("watchdog must end the recording")
	}
	if m.voicePTT {
		t.Error("watchdog must leave push-to-talk mode")
	}
	if !m.voicePTTBroken {
		t.Error("watchdog must latch so /voice does not re-arm into the same trap")
	}
}

// A working terminal must not be punished for a long hold. Once a release of
// the talk key has been seen, the watchdog is retired.
func TestPushToTalkWatchdogSpareLongHoldOnGoodTerminal(t *testing.T) {
	m := &chatTUI{voicePTT: true, voicePTTSupported: true,
		voiceSawKeyRelease: true, voiceSawTalkRelease: true, pendingCommit: &[]string{}}
	s := &voiceSession{seq: 1, cfg: config.VoiceConfig{}.WithDefaults()}
	s.pcm = make([]byte, int(voicePTTStuckHold.Seconds()+30)*voiceBytesPerSec)
	m.voice = s
	m.voiceSeq = 1

	m.handleVoiceMsg(voiceTickMsg{seq: 1})
	if m.voice == nil {
		t.Error("a long hold must survive once releases are proven to arrive")
	}
	if m.voicePTTBroken {
		t.Error("a working terminal must not be marked broken")
	}
}

// Releasing the talk key is what proves the terminal honours releases.
func TestTalkKeyReleaseRecordsEvidence(t *testing.T) {
	m := &chatTUI{voicePTT: true}
	if m.voiceSawTalkRelease {
		t.Fatal("evidence must start empty")
	}
	m.voiceKeyRelease(tea.KeyReleaseMsg{Code: tea.KeySpace, Text: " "})
	if !m.voiceSawTalkRelease {
		t.Error("a release of the talk key must be recorded as evidence")
	}
}

// Keys other than the talk key still reach the composer while armed, so typing
// is not swallowed by voice mode.
func TestPushToTalkPassesThroughOtherKeys(t *testing.T) {
	m := &chatTUI{voicePTT: true}
	for _, k := range []tea.KeyPressMsg{
		{Code: 'a', Text: "a"},
		{Code: 'z', Text: "z"},
	} {
		if _, handled := m.voiceKeyIntercept(k); handled {
			t.Errorf("key %q must pass through to the composer", k.String())
		}
	}
}

// Enter must submit the turn, not consume the keystroke: releasing SPACE has
// already ended the capture, so Enter has nothing left to stop. Voice mode stays
// armed so the next hold dictates the next message.
func TestPushToTalkEnterSubmitsAndStaysArmed(t *testing.T) {
	m := &chatTUI{voicePTT: true} // not recording: the key is up

	cmd, handled := m.voiceKeyIntercept(tea.KeyPressMsg{Code: tea.KeyEnter})
	if handled {
		t.Fatal("Enter must fall through so the turn is submitted normally")
	}
	if cmd != nil {
		t.Error("Enter should not produce a voice command when idle")
	}
	if !m.voicePTT {
		t.Error("voice mode must stay armed after sending, for the next hold")
	}
}

// Enter pressed before the key comes up must land the transcript first, or the
// turn would be sent without the words that were just spoken.
func TestPushToTalkEnterMidHoldCommitsFirst(t *testing.T) {
	// notice() writes through pendingCommit, which a real model always has.
	var commit []string
	m := &chatTUI{
		voicePTT:      true,
		voice:         &voiceSession{seq: 1, prefix: "before "},
		pendingCommit: &commit,
	}

	_, handled := m.voiceKeyIntercept(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("Enter during an active hold must be swallowed, not submitted")
	}
	if m.voice != nil {
		t.Error("the in-flight capture should have been stopped")
	}
	if !m.voicePTT {
		t.Error("voice mode must remain armed")
	}
}

// A modal surface owns the keyboard; push-to-talk must not eat its keys.
func TestPushToTalkYieldsToModals(t *testing.T) {
	m := &chatTUI{voicePTT: true, clearConfirm: &clearConfirm{confirm: 1}}
	if _, handled := m.voiceKeyIntercept(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}); handled {
		t.Error("SPACE must reach the modal, not start a recording")
	}
	if m.voice != nil {
		t.Error("no capture should start while a modal is up")
	}
}

// While voice mode is armed the user can still type, and prose is full of
// spaces. A short tap must be a space, not the start of a recording — otherwise
// composing text means fighting the recorder at every word boundary.
func TestPushToTalkShortTapTypesSpace(t *testing.T) {
	m := &chatTUI{voicePTT: true, voicePTTCfg: config.VoiceConfig{}.WithDefaults(),
		input: textarea.New(), pendingCommit: &[]string{}}
	m.input.SetValue("hello")

	cmd, handled := m.voiceKeyIntercept(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if !handled {
		t.Fatal("the talk key must be consumed while armed")
	}
	if cmd == nil {
		t.Fatal("press must schedule the hold threshold")
	}
	if m.voice != nil {
		t.Fatal("the mic must NOT open on the keystroke itself")
	}
	if !m.voicePTTHolding {
		t.Error("press must record that a hold is in progress")
	}
	// The space must land immediately. Deferring it to key-up reorders fast
	// typing: the next letter is processed first.
	if got := m.input.Value(); got != "hello " {
		t.Fatalf("composer after press = %q, want the space typed straight away", got)
	}

	// Released before the threshold: that was typing.
	if _, handled := m.voiceKeyRelease(tea.KeyReleaseMsg{Code: tea.KeySpace, Text: " "}); !handled {
		t.Fatal("release must be consumed")
	}
	if m.voice != nil {
		t.Error("a short tap must not leave a recording behind")
	}
	if m.voicePTTHolding {
		t.Error("hold state must be cleared on release")
	}
	if got := m.input.Value(); got != "hello " {
		t.Errorf("composer = %q, want exactly one space and no duplicate", got)
	}
	if !m.voicePTT {
		t.Error("a tap must not leave voice mode")
	}
}

// A repeat arriving during the pre-threshold window must not restart the timer,
// or a long hold would never promote. Auto-repeat is reported with IsRepeat
// false on every terminal measured, so the holding flag is the real guard.
func TestPushToTalkHoldSurvivesAutoRepeat(t *testing.T) {
	m := &chatTUI{voicePTT: true, voicePTTCfg: config.VoiceConfig{}.WithDefaults(),
		input: textarea.New(), pendingCommit: &[]string{}}

	m.voiceKeyIntercept(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	first := m.voiceHoldSeq

	for range 5 {
		if _, handled := m.voiceKeyIntercept(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}); !handled {
			t.Fatal("repeats must stay consumed")
		}
	}
	if m.voiceHoldSeq != first {
		t.Errorf("hold seq moved to %d; repeats must not restart the threshold", m.voiceHoldSeq)
	}
}

// A threshold timer from an earlier tap must not open the mic later.
func TestPushToTalkStaleHoldTimerIgnored(t *testing.T) {
	m := &chatTUI{voicePTT: true, voicePTTHolding: true, voiceHoldSeq: 7,
		voicePTTCfg: config.VoiceConfig{}.WithDefaults(),
		input:       textarea.New(), pendingCommit: &[]string{}}

	if _, ok := m.handleVoiceMsg(voicePTTHoldMsg{seq: 3}); !ok {
		t.Fatal("the hold message must be consumed as a voice message")
	}
	if m.voice != nil {
		t.Error("a stale hold timer must not open the mic")
	}

	// Nor may one fire after the key was already released.
	m.voicePTTHolding = false
	m.handleVoiceMsg(voicePTTHoldMsg{seq: 7})
	if m.voice != nil {
		t.Error("a hold timer must not open the mic after release")
	}
}

// When a hold does turn into dictation, the space the press typed has to be
// taken back out — otherwise every hold would leave a stray space in front of
// the transcript.
func TestPushToTalkPromotionRetractsSpace(t *testing.T) {
	m := &chatTUI{voicePTT: true, voicePTTCfg: config.VoiceConfig{}.WithDefaults(),
		input: textarea.New(), pendingCommit: &[]string{}}
	m.input.SetValue("note:")

	m.voiceKeyIntercept(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := m.input.Value(); got != "note: " {
		t.Fatalf("composer after press = %q", got)
	}

	// Recorder is stubbed out: this exercises the retraction, not the mic.
	m.voicePTTCfg.RecordCmd = []string{"true"}
	m.handleVoiceMsg(voicePTTHoldMsg{seq: m.voiceHoldSeq})

	if got := m.input.Value(); got != "note:" {
		t.Errorf("composer = %q, want the provisional space retracted", got)
	}
	if m.voice == nil {
		t.Error("promotion must open a capture session")
	}
}
