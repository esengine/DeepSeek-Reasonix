package config

import (
	"errors"
	"strings"
	"time"
)

// VoiceConfig configures "/voice" dictation. Audio is captured locally and sent
// to an OpenAI-compatible POST /v1/audio/transcriptions endpoint, so anything
// speaking that shape works — a local server (Speaches, LocalAI, vLLM, or any
// faster-whisper/whisper.cpp wrapper exposing the OpenAI route) or a hosted one
// (OpenAI, Groq, Fireworks, ...).
//
// Hosted, with a key:
//
//	[voice]
//	enabled     = true
//	url         = "https://api.openai.com/v1/audio/transcriptions"
//	model       = "whisper-1"
//	api_key_env = "OPENAI_API_KEY"
//
// Local, no auth:
//
//	[voice]
//	enabled = true
//	url     = "http://127.0.0.1:8000/v1/audio/transcriptions"
//	model   = "Systran/faster-whisper-large-v3"
//
// Optional: language (omit to auto-detect), prompt (biases spelling of names the
// model has never seen), temperature, poll_ms, max_seconds, device, record_cmd,
// and headers for gateways with their own auth scheme.
type VoiceConfig struct {
	Enabled bool   `toml:"enabled"`
	URL     string `toml:"url"`
	Model   string `toml:"model"`

	// APIKeyEnv names the credential holding the bearer token, resolved through
	// the same store as provider keys. Empty means an unauthenticated endpoint.
	APIKeyEnv string `toml:"api_key_env"`
	// Headers are extra HTTP headers for gateways that want something other than
	// Authorization: Bearer. Secrets belong in api_key_env, not here.
	Headers map[string]string `toml:"headers"`

	// Language is an ISO-639-1 hint ("en"). Empty auto-detects, which costs a
	// little latency and can drift on short partials — set it when you know it.
	Language string `toml:"language"`
	// Prompt biases decoding toward vocabulary the model would otherwise mangle
	// (project names, CLI flags, people). Whisper treats it as prior context.
	Prompt      string  `toml:"prompt"`
	Temperature float64 `toml:"temperature"`

	// Device is a platform capture device for the default recorder (an ALSA
	// name such as "hw:1,0" on Linux).
	Device string `toml:"device"`
	// RecordCmd overrides the capture command entirely. It must emit raw
	// signed 16-bit little-endian mono PCM at 16kHz on stdout.
	RecordCmd []string `toml:"record_cmd"`

	PollMS     int `toml:"poll_ms"`
	MaxSeconds int `toml:"max_seconds"`

	// PTTHoldMS is how long the talk key must be held before push-to-talk opens
	// the mic. Below it the keystroke is treated as an ordinary space, so text
	// can still be typed while voice mode is armed.
	PTTHoldMS int `toml:"ptt_hold_ms"`

	// NoPushToTalk forces the toggle behaviour even on terminals that report key
	// release events. Push-to-talk binds SPACE while dictating, which is worth
	// opting out of if that conflicts with how you use the composer.
	NoPushToTalk bool `toml:"no_push_to_talk"`
}

const (
	// defaultVoiceModel is the OpenAI transcription model name. Local servers
	// generally accept and ignore it, so it is a safe cross-provider default.
	defaultVoiceModel      = "whisper-1"
	defaultVoicePollMS     = 500
	defaultVoiceMaxSeconds = 300

	// defaultVoicePTTHoldMS is how long SPACE must be held before push-to-talk
	// treats it as dictation rather than as a typed space. Without a threshold,
	// composing text while voice mode is armed fights the recorder on every word
	// boundary. A space typed in prose is well under 150ms; a deliberate hold is
	// not, so this separates them without the user thinking about it.
	//
	// The space is typed on the press and retracted if the hold promotes, so
	// this threshold does not affect typing at all — it only sets how long the
	// key must be held before dictation starts. Raising it is therefore cheap
	// for typing but delays speech: audio is captured from the promotion, not
	// from the press.
	defaultVoicePTTHoldMS = 1000

	// minVoicePollMS floors the cadence. Below this the client spends more time
	// re-transcribing than listening, for partials arriving faster than anyone
	// reads them.
	minVoicePollMS = 200
)

// ErrVoiceURLUnset reports that /voice has no endpoint to talk to. There is
// deliberately no default URL: guessing localhost would fail confusingly, and
// baking in a vendor would be a poor default for self-hosted users.
var ErrVoiceURLUnset = errors.New("voice: set url under [voice] to an OpenAI-compatible /v1/audio/transcriptions endpoint")

// WithDefaults fills unset fields. Callers should use the returned copy.
func (v VoiceConfig) WithDefaults() VoiceConfig {
	v.URL = strings.TrimSpace(v.URL)
	v.Model = strings.TrimSpace(v.Model)
	if v.Model == "" {
		v.Model = defaultVoiceModel
	}
	if v.PollMS <= 0 {
		v.PollMS = defaultVoicePollMS
	}
	if v.PollMS < minVoicePollMS {
		v.PollMS = minVoicePollMS
	}
	if v.MaxSeconds <= 0 {
		v.MaxSeconds = defaultVoiceMaxSeconds
	}
	if v.PTTHoldMS <= 0 {
		v.PTTHoldMS = defaultVoicePTTHoldMS
	}
	return v
}

// Validate reports whether this config can be used to make a request.
func (v VoiceConfig) Validate() error {
	if strings.TrimSpace(v.URL) == "" {
		return ErrVoiceURLUnset
	}
	return nil
}

// PollInterval is how often the growing buffer is re-sent.
func (v VoiceConfig) PollInterval() time.Duration {
	ms := v.PollMS
	if ms < minVoicePollMS {
		ms = defaultVoicePollMS
	}
	return time.Duration(ms) * time.Millisecond
}

// MaxDuration caps a single recording so a forgotten session cannot grow without
// bound. Zero means uncapped.
func (v VoiceConfig) MaxDuration() time.Duration {
	if v.MaxSeconds <= 0 {
		return 0
	}
	return time.Duration(v.MaxSeconds) * time.Second
}

// AuthToken resolves the bearer token, if one is configured. The bool reports
// whether a token should be sent; a configured-but-unset credential returns
// false so the caller can surface a clear error instead of a bare 401.
func (v VoiceConfig) AuthToken() (string, bool) {
	name := strings.TrimSpace(v.APIKeyEnv)
	if name == "" {
		return "", false
	}
	res := ResolveCredential(name)
	if !res.Set || strings.TrimSpace(res.Value) == "" {
		return "", false
	}
	return res.Value, true
}

// PTTHoldDelay is how long the talk key must be held before dictation starts.
func (v VoiceConfig) PTTHoldDelay() time.Duration {
	return time.Duration(v.PTTHoldMS) * time.Millisecond
}
