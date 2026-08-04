package cli

// Live voice dictation for the composer ("/voice").
//
// Model: record continuously into an in-memory PCM buffer and, every poll
// interval, POST the accumulated audio to an OpenAI-compatible
// /v1/audio/transcriptions endpoint. Each response is the transcript of
// everything spoken so far, so the composer is *replaced* rather than appended
// to. Enter commits the text, Esc discards it.
//
// Why re-sending the whole buffer is affordable: Whisper-family encoders always
// pad to a fixed 30s window, so transcribing 1s and transcribing 30s cost about
// the same. On one consumer GPU running faster-whisper large-v3-turbo that
// measured 0.128s at a 1s buffer and 0.297s at 30s — flat enough to hold a 500ms
// cadence comfortably.
//
// That flatness is a property of the encoder, not of ASR in general. An
// autoregressive speech model measured 0.43s -> 4.35s across the same range on
// the same hardware, which would fall behind the cadence after a couple of
// seconds. Partials are therefore clamped to a tail window (see partialWindow)
// so cost stays bounded on any backend, and the poll interval is floored.

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

const (
	voiceSampleRate  = 16000 // Hz — what the ASR endpoint expects
	voiceChannels    = 1
	voiceBitsPerSam  = 16
	voiceBytesPerSec = voiceSampleRate * voiceChannels * (voiceBitsPerSam / 8)

	// partialWindow bounds how much audio a *partial* request carries. Past a
	// Whisper encoder window the flat-cost property is gone and latency starts
	// tracking length, which would let partials fall behind the poll cadence on
	// a long dictation. The final request always sends the complete buffer.
	partialWindow = 30 * time.Second

	// voicePTTStuckHold bounds a push-to-talk hold that no release ever ends.
	// A terminal can advertise key-release reporting and then never deliver it,
	// which leaves a hold recording with nothing able to stop it. Past this the
	// transcript is committed and push-to-talk stands down for the session.
	// Only ever consulted before the first successful release, so a genuinely
	// long hold on a working terminal is unaffected.
	voicePTTStuckHold = 45 * time.Second

	// voiceTalkKey is the push-to-talk key as bubbletea stringifies it. The
	// space bar renders as "space", NOT " ": Key.String() explicitly skips
	// the space text and falls through to Keystroke(). Comparing against a
	// literal space here would silently never match.
	voiceTalkKey = "space"
)

// voicePTTHoldMsg fires once the talk key has been held long enough to count as
// dictation rather than as a typed space. seq scopes it to one hold so a stale
// timer from an earlier tap cannot open the mic.
type voicePTTHoldMsg struct{ seq int }

// voiceTickMsg drives one poll of the growing buffer. seq scopes it to a single
// recording so a tick from an old session cannot resurrect a finished one.
type voiceTickMsg struct{ seq int }

// voicePartialMsg carries an interim transcript. samples records how much audio
// the request was built from, so a response that overtakes a newer one can be
// dropped instead of rewinding the composer.
type voicePartialMsg struct {
	seq     int
	samples int
	text    string
	err     error
}

// voiceFinalMsg carries the transcript of the complete buffer, produced after
// the user stops recording.
type voiceFinalMsg struct {
	seq  int
	text string
	err  error
}

// voiceSession owns one recording: the capture subprocess, the PCM buffer it
// writes into, and the in-flight guard. Held by pointer on chatTUI because the
// bubbletea model is copied by value on every update.
type voiceSession struct {
	seq int

	mu       sync.Mutex
	pcm      []byte
	inflight bool
	recErr   error
	done     bool

	cmd    *exec.Cmd
	cancel context.CancelFunc

	// prefix is whatever was already in the composer when recording started, so
	// dictation appends to the user's typing instead of erasing it.
	prefix string

	cfg config.VoiceConfig
}

// startVoiceSession spawns the capture process and begins buffering PCM.
func startVoiceSession(seq int, cfg config.VoiceConfig, prefix string) (*voiceSession, error) {
	argv, err := voiceRecordArgv(cfg)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	// Keep stderr so a failed recorder can explain itself instead of just
	// producing silence.
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("%s: %w", argv[0], err)
	}

	s := &voiceSession{seq: seq, cmd: cmd, cancel: cancel, prefix: prefix, cfg: cfg}

	go func() {
		buf := make([]byte, 8192)
		for {
			n, readErr := stdout.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.pcm = append(s.pcm, buf[:n]...)
				s.mu.Unlock()
			}
			if readErr != nil {
				s.mu.Lock()
				if readErr != io.EOF && !s.done {
					msg := strings.TrimSpace(errBuf.String())
					if msg == "" {
						msg = readErr.Error()
					}
					s.recErr = fmt.Errorf("%s: %s", argv[0], msg)
				}
				s.mu.Unlock()
				break
			}
		}
		// Reap the recorder. Cancelling the context kills it but nothing
		// collects its exit status, so without this every hold leaves a zombie
		// behind for the lifetime of the session. Safe here and only here: the
		// read loop has finished, and Wait closes the pipe it was reading.
		_ = cmd.Wait()
	}()

	return s, nil
}

// stop halts capture. The buffered audio stays available for the final pass.
func (s *voiceSession) stop() {
	s.mu.Lock()
	s.done = true
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

// snapshot returns a copy of the buffered PCM. When window > 0 only the most
// recent window of audio is returned (partials); otherwise the whole buffer.
func (s *voiceSession) snapshot(window time.Duration) ([]byte, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pcm := s.pcm
	if window > 0 {
		maxBytes := int(window.Seconds()) * voiceBytesPerSec
		if len(pcm) > maxBytes {
			pcm = pcm[len(pcm)-maxBytes:]
		}
	}
	out := make([]byte, len(pcm))
	copy(out, pcm)
	return out, len(s.pcm)
}

func (s *voiceSession) duration() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Duration(len(s.pcm)) * time.Second / time.Duration(voiceBytesPerSec)
}

func (s *voiceSession) recordError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recErr
}

// tryClaim enforces single-flight: at most one transcription request is in the
// air at a time. A tick that arrives while one is pending is dropped rather
// than queued, so a slow response can never build a backlog.
func (s *voiceSession) tryClaim() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight {
		return false
	}
	s.inflight = true
	return true
}

func (s *voiceSession) release() {
	s.mu.Lock()
	s.inflight = false
	s.mu.Unlock()
}

// voiceRecordArgv picks a capture command: an explicit config override first,
// then the platform default. All variants must emit raw signed 16-bit
// little-endian mono PCM at 16kHz on stdout.
func voiceRecordArgv(cfg config.VoiceConfig) ([]string, error) {
	if len(cfg.RecordCmd) > 0 {
		return cfg.RecordCmd, nil
	}
	switch runtime.GOOS {
	case "linux":
		argv := []string{"arecord", "-q", "-t", "raw", "-f", "S16_LE", "-r", "16000", "-c", "1"}
		if strings.TrimSpace(cfg.Device) != "" {
			argv = append(argv, "-D", cfg.Device)
		}
		if _, err := exec.LookPath("arecord"); err != nil {
			return nil, fmt.Errorf("arecord not found (install alsa-utils, or set voice.record_cmd)")
		}
		return argv, nil
	case "darwin":
		if _, err := exec.LookPath("sox"); err != nil {
			return nil, fmt.Errorf("sox not found (brew install sox, or set voice.record_cmd)")
		}
		return []string{"sox", "-q", "-d", "-t", "raw", "-b", "16", "-e", "signed-integer",
			"-r", "16000", "-c", "1", "-"}, nil
	default:
		return nil, fmt.Errorf("no default recorder for %s — set voice.record_cmd", runtime.GOOS)
	}
}

// wavFromPCM wraps raw PCM in a 44-byte WAV header. The recorder streams
// headerless PCM on purpose: a WAV written by a still-running process carries
// wrong RIFF/data sizes until it closes, and this endpoint is fed a fresh
// buffer several times a second.
func wavFromPCM(pcm []byte) []byte {
	var b bytes.Buffer
	b.Grow(44 + len(pcm))
	byteRate := voiceSampleRate * voiceChannels * voiceBitsPerSam / 8
	blockAlign := voiceChannels * voiceBitsPerSam / 8

	// bytes.Buffer grows to fit and never fails a write, so these cannot error.
	put := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }

	b.WriteString("RIFF")
	put(uint32(36 + len(pcm)))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	put(uint32(16))
	put(uint16(1)) // PCM
	put(uint16(voiceChannels))
	put(uint32(voiceSampleRate))
	put(uint32(byteRate))
	put(uint16(blockAlign))
	put(uint16(voiceBitsPerSam))
	b.WriteString("data")
	put(uint32(len(pcm)))
	b.Write(pcm)
	return b.Bytes()
}

// transcribe POSTs one WAV to the ASR endpoint and returns the transcript.
// The request is plain OpenAI /v1/audio/transcriptions, so it works against a
// local server or a hosted provider with no code change — only config.
func transcribe(ctx context.Context, cfg config.VoiceConfig, pcm []byte) (string, error) {
	if len(pcm) == 0 {
		return "", nil
	}
	if err := cfg.Validate(); err != nil {
		return "", err
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", err
	}
	if _, err := part.Write(wavFromPCM(pcm)); err != nil {
		return "", err
	}
	_ = w.WriteField("model", cfg.Model)
	_ = w.WriteField("response_format", "json")
	if strings.TrimSpace(cfg.Language) != "" {
		_ = w.WriteField("language", cfg.Language)
	}
	if strings.TrimSpace(cfg.Prompt) != "" {
		_ = w.WriteField("prompt", cfg.Prompt)
	}
	if cfg.Temperature != 0 {
		_ = w.WriteField("temperature", strconv.FormatFloat(cfg.Temperature, 'f', -1, 64))
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	// Extra headers first so they cannot silently clobber Content-Type, and so a
	// gateway with a bespoke auth scheme is still expressible.
	for k, v := range cfg.Headers {
		if k = strings.TrimSpace(k); k != "" && !strings.EqualFold(k, "Content-Type") {
			req.Header.Set(k, v)
		}
	}
	if token, ok := cfg.AuthToken(); ok {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if name := strings.TrimSpace(cfg.APIKeyEnv); name != "" {
		// Fail with the cause rather than letting the endpoint answer 401.
		return "", fmt.Errorf("voice: %s is not set", name)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("asr %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var out struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("asr: bad response: %w", err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("asr: %s", out.Error)
	}
	return strings.TrimSpace(out.Text), nil
}

// ---- bubbletea plumbing -------------------------------------------------

func voiceTickCmd(seq int, every time.Duration) tea.Cmd {
	return tea.Tick(every, func(time.Time) tea.Msg { return voiceTickMsg{seq: seq} })
}

// voicePartialCmd transcribes the current tail window off the Update loop.
func voicePartialCmd(s *voiceSession) tea.Cmd {
	return func() tea.Msg {
		pcm, total := s.snapshot(partialWindow)
		defer s.release()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		text, err := transcribe(ctx, s.cfg, pcm)
		return voicePartialMsg{seq: s.seq, samples: total, text: text, err: err}
	}
}

// voiceFinalCmd transcribes the complete buffer once recording has stopped.
func voiceFinalCmd(s *voiceSession) tea.Cmd {
	return func() tea.Msg {
		pcm, _ := s.snapshot(0)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		text, err := transcribe(ctx, s.cfg, pcm)
		return voiceFinalMsg{seq: s.seq, text: text, err: err}
	}
}

// voiceWarmupCmd fires a throwaway request so the first real partial does not
// pay cold-path cost. Measured ~0.28-0.41s cold vs ~0.13s warm.
func voiceWarmupCmd(cfg config.VoiceConfig) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		silence := make([]byte, voiceBytesPerSec/10) // 100ms
		_, _ = transcribe(ctx, cfg, silence)
		return nil
	}
}

// ---- chatTUI integration ------------------------------------------------

// voiceConfig loads and validates the dictation config, reporting the reason to
// the transcript if it cannot be used.
func (m *chatTUI) voiceConfig() (config.VoiceConfig, bool) {
	cfg, err := config.Load()
	if err != nil {
		m.notice(fmt.Sprintf("%s: %v", i18n.M.VoiceFailed, err))
		return config.VoiceConfig{}, false
	}
	vc := cfg.Voice.WithDefaults()
	if !vc.Enabled {
		m.notice(i18n.M.VoiceDisabled)
		return config.VoiceConfig{}, false
	}
	// Catch a missing endpoint before opening the mic, so the failure names the
	// cause instead of arriving as a dead partial after the user has spoken.
	// Show the whole setup rather than just the missing key: "set url" leaves a
	// first-time user with no idea what a valid endpoint even looks like.
	if err := vc.Validate(); err != nil {
		m.notice(i18n.M.VoiceSetupHint)
		return config.VoiceConfig{}, false
	}
	return vc, true
}

// startVoice runs /voice. Where the terminal reports key release events it arms
// push-to-talk (hold SPACE to talk); elsewhere it falls back to a toggle, since
// a classic terminal only ever sends key-down and hold-to-talk is unrepresentable
// there. Invoking it again leaves whichever mode is active.
func (m *chatTUI) startVoice() tea.Cmd {
	if m.voicePTT {
		return m.exitVoicePTT(true)
	}
	if m.voice != nil {
		return m.stopVoice(true)
	}

	vc, ok := m.voiceConfig()
	if !ok {
		return nil
	}

	if !vc.NoPushToTalk && m.pttPossible() {
		m.voicePTT = true
		m.voicePTTCfg = vc
		m.notice(i18n.M.VoicePTTReady)
		// Warm the endpoint now so the first hold is not the slow one.
		return voiceWarmupCmd(vc)
	}

	// Say why the keys behave differently. Downgrading in silence is what makes
	// this look like a bug: the user holds SPACE, nothing ends the hold, and
	// there is nothing on screen to explain it.
	note := i18n.M.VoiceListening
	if !vc.NoPushToTalk {
		note = i18n.M.VoicePTTUnavailable
	}
	return m.beginVoiceCapture(vc, note)
}

// pttPossible reports whether push-to-talk can actually work here. The terminal
// must claim key-release reporting AND have delivered at least one release: a
// terminal that answers the enhancement query and then sends nothing would
// otherwise arm a hold that can never end.
func (m *chatTUI) pttPossible() bool {
	return m.voicePTTSupported && m.voiceSawKeyRelease && !m.voicePTTBroken
}

// beginVoiceCapture opens the mic and starts the poll loop.
func (m *chatTUI) beginVoiceCapture(vc config.VoiceConfig, note string) tea.Cmd {
	m.voiceSeq++
	// The prefix is taken fresh each time, so successive push-to-talk holds
	// accumulate instead of overwriting one another.
	s, err := startVoiceSession(m.voiceSeq, vc, m.input.Value())
	if err != nil {
		m.notice(fmt.Sprintf("%s: %v", i18n.M.VoiceFailed, err))
		return nil
	}
	m.voice = s
	m.voiceRendered = 0
	if note != "" {
		m.notice(note)
	}
	return tea.Batch(
		voiceWarmupCmd(vc),
		voiceTickCmd(s.seq, vc.PollInterval()),
	)
}

// exitVoicePTT leaves push-to-talk mode, committing or discarding any hold that
// is still in progress.
func (m *chatTUI) exitVoicePTT(commit bool) tea.Cmd {
	var cmd tea.Cmd
	if m.voice != nil {
		m.voicePrefix = m.voice.prefix
		cmd = m.stopVoice(commit)
	}
	m.voicePTT = false
	m.voicePTTHolding = false
	m.notice(i18n.M.VoicePTTDone)
	return cmd
}

// stopVoice ends capture. commit=true runs a final full-buffer pass and leaves
// the text in the composer; commit=false discards the recording.
func (m *chatTUI) stopVoice(commit bool) tea.Cmd {
	s := m.voice
	if s == nil {
		return nil
	}
	m.voice = nil
	s.stop()

	if !commit {
		m.input.SetValue(s.prefix)
		m.growInputToFit()
		m.notice(i18n.M.VoiceCancelled)
		return nil
	}
	if s.duration() <= 0 {
		m.notice(i18n.M.VoiceNoAudio)
		return nil
	}
	m.notice(i18n.M.VoiceTranscribing)
	return voiceFinalCmd(s)
}

// applyVoiceText renders a transcript into the composer, preserving whatever
// the user had already typed before recording started.
func (m *chatTUI) applyVoiceText(prefix, text string) {
	combined := text
	if prefix != "" && text != "" {
		combined = strings.TrimRight(prefix, " ") + " " + text
	} else if text == "" {
		combined = prefix
	}
	m.input.SetValue(combined)
	m.input.CursorEnd()
	m.growInputToFit()
}

// handleVoiceMsg processes the three voice messages. ok reports whether msg was
// a voice message at all.
func (m *chatTUI) handleVoiceMsg(msg tea.Msg) (cmd tea.Cmd, ok bool) {
	switch v := msg.(type) {
	case voicePTTHoldMsg:
		// Held past the threshold, so this is dictation. A stale seq means the
		// key was already released and this timer belongs to an earlier tap.
		if !m.voicePTT || !m.voicePTTHolding || v.seq != m.voiceHoldSeq || m.voice != nil {
			return nil, true
		}
		// This was dictation after all, so retract the space the press typed.
		// Done before capture starts so it is not carried into the prefix the
		// transcript appends to.
		if cur := m.input.Value(); strings.HasSuffix(cur, " ") {
			m.input.SetValue(strings.TrimSuffix(cur, " "))
			m.input.CursorEnd()
			m.growInputToFit()
		}
		return m.beginVoiceCapture(m.voicePTTCfg, ""), true

	case voiceTickMsg:
		s := m.voice
		// A tick from a superseded recording is inert.
		if s == nil || s.seq != v.seq {
			return nil, true
		}
		if err := s.recordError(); err != nil {
			m.stopVoice(false)
			m.notice(fmt.Sprintf("%s: %v", i18n.M.VoiceFailed, err))
			return nil, true
		}
		// A push-to-talk hold this long, before any release of the talk key has
		// ever been seen, means the release is not coming: the terminal claimed
		// the enhancement and does not honour it. Land what was said and drop to
		// toggle, rather than recording until max_seconds with no way to stop.
		if m.voicePTT && !m.voiceSawTalkRelease && s.duration() >= voicePTTStuckHold {
			cmd := m.stopVoice(true)
			m.voicePTT = false
			m.voicePTTBroken = true
			m.notice(i18n.M.VoicePTTStuck)
			return cmd, true
		}

		// Auto-stop rather than record forever if the user walks away.
		if max := s.cfg.MaxDuration(); max > 0 && s.duration() >= max {
			m.notice(i18n.M.VoiceMaxReached)
			return m.stopVoice(true), true
		}

		var cmds []tea.Cmd
		if s.tryClaim() {
			cmds = append(cmds, voicePartialCmd(s))
		}
		cmds = append(cmds, voiceTickCmd(s.seq, s.cfg.PollInterval()))
		return tea.Batch(cmds...), true

	case voicePartialMsg:
		s := m.voice
		if s == nil || s.seq != v.seq {
			return nil, true
		}
		if v.err != nil {
			// Partials are best-effort: a dropped one is not worth ending the
			// recording over, and the final pass still has the full audio.
			return nil, true
		}
		// Drop a response that lost a race with a newer one.
		if v.samples < m.voiceRendered {
			return nil, true
		}
		m.voiceRendered = v.samples
		m.applyVoiceText(s.prefix, v.text)
		return nil, true

	case voiceFinalMsg:
		if v.seq != m.voiceSeq {
			return nil, true
		}
		m.voiceRendered = 0
		if v.err != nil {
			m.notice(fmt.Sprintf("%s: %v", i18n.M.VoiceFailed, v.err))
			return nil, true
		}
		if strings.TrimSpace(v.text) == "" {
			m.notice(i18n.M.VoiceNoAudio)
			return nil, true
		}
		m.applyVoiceText(m.voicePrefix, v.text)
		m.notice(i18n.M.VoiceDone)
		return nil, true
	}
	return nil, false
}

// voiceKeyIntercept handles keys while dictation is active, before they reach
// the composer. In push-to-talk mode SPACE is the talk key: press starts a hold,
// release ends it, Enter leaves the mode, Esc discards.
func (m *chatTUI) voiceKeyIntercept(msg tea.KeyPressMsg) (cmd tea.Cmd, handled bool) {
	if !m.voicePTT && m.voice == nil {
		return nil, false
	}

	// Another surface owns the keyboard (approval prompt, picker, confirmation).
	// Push-to-talk must not swallow its keys.
	if m.voicePTT && m.voiceModalActive() {
		return nil, false
	}

	key := msg.String()

	// NOTE: the space bar stringifies as "space", not " " — Key.String()
	// deliberately excludes the space text and falls back to Keystroke().
	if m.voicePTT && key == voiceTalkKey {
		// A held key auto-repeats as further press events, roughly every 40ms.
		// Without this guard every repeat would tear down and restart the
		// recording, so the hold would capture nothing.
		//
		// Note IsRepeat is not sufficient on its own: on every terminal measured
		// (Ghostty, Kitty, WezTerm) auto-repeat arrives with IsRepeat false, so
		// in practice the active-session check is what filters them. IsRepeat is
		// kept because a terminal that does set it is then handled a keystroke
		// earlier, before a session exists.
		if msg.Key().IsRepeat || m.voice != nil || m.voicePTTHolding {
			return nil, true
		}
		// Type the space NOW rather than on release. Deferring it to key-up
		// reorders fast typing — the next letter is processed first, turning
		// "hello world" into "hellow orld". The space is removed again if the
		// hold turns out to be dictation, which is the rarer path.
		m.input.InsertString(" ")
		m.growInputToFit()

		// Do NOT open the mic on the keystroke. While voice mode is armed the
		// user is still able to type, and prose is full of spaces — starting a
		// recording on each one means fighting the recorder at every word
		// boundary. Only a deliberate hold is dictation, so start a timer and
		// decide when it fires.
		m.voicePTTHolding = true
		m.voiceHoldSeq++
		seq := m.voiceHoldSeq
		return tea.Tick(m.voicePTTCfg.PTTHoldDelay(), func(time.Time) tea.Msg {
			return voicePTTHoldMsg{seq: seq}
		}), true
	}

	switch key {
	case "enter":
		if m.voicePTT {
			// Releasing SPACE already ended the capture, so Enter has nothing to
			// stop: let it submit the turn normally and stay armed, so the next
			// hold dictates the next message without re-running /voice.
			//
			// The exception is Enter arriving while a hold is still in flight
			// (pressed before the key came up). Land that transcript first and
			// swallow this Enter, or the turn would be sent without the words.
			if m.voice != nil {
				m.voicePrefix = m.voice.prefix
				return m.stopVoice(true), true
			}
			return nil, false
		}
		m.voicePrefix = m.voice.prefix
		return m.stopVoice(true), true
	case "esc", "ctrl+c":
		if m.voicePTT {
			return m.exitVoicePTT(false), true
		}
		return m.stopVoice(false), true
	}
	return nil, false
}

// voiceModalActive reports whether a modal surface currently owns the keyboard.
func (m *chatTUI) voiceModalActive() bool {
	return m.pendingApproval != nil || m.rewind != nil || m.clearConfirm != nil
}

// voiceKeyRelease ends a push-to-talk hold. Only delivered by terminals that
// support the keyboard enhancement; elsewhere /voice never enters this mode.
func (m *chatTUI) voiceKeyRelease(msg tea.KeyReleaseMsg) (cmd tea.Cmd, handled bool) {
	if !m.voicePTT || msg.String() != voiceTalkKey {
		return nil, false
	}
	// Evidence that this terminal really does report releases of the talk key,
	// which retires the stuck-hold watchdog for the rest of the session.
	m.voiceSawTalkRelease = true

	// Let go before the hold threshold: that was typing, and the space was
	// already typed on the press. Nothing to do but stay armed.
	if m.voicePTTHolding && m.voice == nil {
		m.voicePTTHolding = false
		return nil, true
	}
	m.voicePTTHolding = false

	if m.voice == nil {
		return nil, true
	}
	m.voicePrefix = m.voice.prefix
	// Stay in push-to-talk mode: the user can hold again to keep dictating, and
	// each hold appends to what is already in the composer.
	return m.stopVoice(true), true
}
