# Voice dictation (`/voice`)

`/voice` dictates into the composer. Audio is captured locally and sent to an
OpenAI-compatible `POST /v1/audio/transcriptions` endpoint, so it works with a
local server or a hosted provider — the difference is config, not code.

Type `/voice` (alias `/dictate`) to start.

**Push-to-talk** (terminals that report key *release* events): **hold SPACE** to
talk, release to stop. Each hold appends to the composer, so you can dictate in
bursts. **Enter sends the message** and voice mode stays armed, so the next hold
dictates the next turn — hands stay off the keyboard for a whole conversation.
**Esc** leaves voice mode.

A short tap of SPACE still types an ordinary space, and it appears the moment the
key goes down, so typing keeps its natural ordering. The key only becomes the
talk key once it is held for `ptt_hold_ms` (1s by default); when a hold does turn
into dictation, that provisional space is retracted. You can therefore keep
typing while voice mode is armed instead of fighting the recorder at every word
boundary. All other keys always reach the composer.

**Toggle** (everywhere else): recording starts immediately and runs until
**Enter** accepts or **Esc** discards.

The mode is chosen automatically, and `/voice` tells you which one you got —
including when push-to-talk was unavailable, so the keys never change behaviour
silently. Set `no_push_to_talk = true` to force the toggle anyway.

### Which terminals deliver key releases

Push-to-talk needs the Kitty keyboard protocol *with event types*. A claim of
support is not the same as support: a terminal can answer the capability query
and then never send a release, which would arm a hold that nothing can end.
`/voice` therefore waits until it has actually seen a release before arming, and
if a hold somehow still runs away it is stopped after 45 seconds — the transcript
is kept and dictation drops to toggle for the rest of the session.

| Terminal | Push-to-talk |
|---|---|
| Kitty | yes (measured) |
| Ghostty | yes (measured) |
| WezTerm | no — toggle (measured; with `enable_kitty_keyboard = true` it reports event-type support but still sends no releases) |
| xterm, Terminal.app, and most classic terminals | no — toggle |

Other terminals vary; `/voice` detects rather than assumes.

## Setup

Add a `[voice]` block to your config. There is no default endpoint — you have to
say where to send audio.

### Hosted (OpenAI, Groq, Fireworks, ...)

```toml
[voice]
enabled     = true
url         = "https://api.openai.com/v1/audio/transcriptions"
model       = "whisper-1"
api_key_env = "OPENAI_API_KEY"
```

Groq is the same shape with a different host and model:

```toml
[voice]
enabled     = true
url         = "https://api.groq.com/openai/v1/audio/transcriptions"
model       = "whisper-large-v3-turbo"
api_key_env = "GROQ_API_KEY"
```

### Local (Speaches, LocalAI, vLLM, any faster-whisper wrapper)

```toml
[voice]
enabled = true
url     = "http://127.0.0.1:8000/v1/audio/transcriptions"
model   = "Systran/faster-whisper-large-v3"
```

No `api_key_env` means no `Authorization` header is sent.

## Options

| Key | Default | Notes |
|---|---|---|
| `enabled` | `false` | Master switch. |
| `url` | *(none)* | Required. Full transcription endpoint URL. |
| `model` | `whisper-1` | Local servers usually accept and ignore this. |
| `api_key_env` | *(none)* | Credential name for `Authorization: Bearer`. Resolved through the same store as provider keys — the secret never goes in the config file. |
| `headers` | *(none)* | Extra headers for gateways with their own auth scheme. |
| `language` | *(auto)* | ISO-639-1 hint such as `"en"`. Setting it is faster and steadier on short partials than auto-detect. |
| `prompt` | *(none)* | Biases decoding toward words the model would otherwise mangle — project names, CLI flags, people. Worth setting. |
| `temperature` | `0` | Passed through when non-zero. |
| `poll_ms` | `500` | How often the growing buffer is re-sent. Floored at 200. |
| `max_seconds` | `300` | A recording stops itself at this length. |
| `ptt_hold_ms` | `1000` | How long SPACE must be held before it counts as talking. Shorter taps type a space. |
| `no_push_to_talk` | `false` | Force the toggle even where hold-to-talk is supported. |
| `device` | *(default)* | Capture device for the built-in recorder (e.g. ALSA `hw:1,0`). |
| `record_cmd` | *(platform)* | Full override of the capture command. |

## Requirements

A capture tool must be on `PATH`:

- **Linux** — `arecord` (`alsa-utils`)
- **macOS** — `sox`
- **Other / custom** — set `record_cmd`

`record_cmd` must emit **raw signed 16-bit little-endian mono PCM at 16 kHz** on
stdout. For example, with ffmpeg on Linux:

```toml
record_cmd = ["ffmpeg", "-f", "alsa", "-i", "default",
              "-f", "s16le", "-ar", "16000", "-ac", "1", "-"]
```

## How it works, and why

While recording, the client re-sends the **whole accumulated buffer** each tick
and replaces the composer text with the response, rather than stitching
fragments together. That keeps the client trivial and lets the model revise
earlier words as context arrives — `"Out of voice"` becomes
`"Add a voice command"` once the sentence continues.

This is affordable because Whisper-family encoders always pad to a fixed 30s
window: transcribing 1s and 30s cost about the same. Measured on one consumer
GPU running faster-whisper large-v3-turbo, end to end over HTTP:

| Buffer | 1s | 5s | 10s | 20s | 30s |
|---|---|---|---|---|---|
| Latency | 0.128s | 0.138s | 0.150s | 0.188s | 0.297s |

That flatness is a property of the encoder, not of ASR generally — an
autoregressive speech model measured 0.43s → 4.35s across the same range on the
same hardware. Two guards keep the client honest on any backend:

- **Partials are clamped to a 30s tail window**, so per-request cost stays
  bounded no matter how long you talk. The final pass sends the whole recording.
- **Single-flight**: at most one request is in the air. A tick arriving while one
  is pending is dropped, never queued, so a slow endpoint cannot build a backlog.
  Responses carry the buffer length they were built from and are discarded if a
  newer partial already rendered.

A throwaway request is sent when recording starts, because the first call to a
cold endpoint is markedly slower than the rest (measured 569ms cold vs ~150ms
warm on the same audio).

## Troubleshooting

**"set url under [voice]"** — no endpoint configured. See Setup.

**"`<NAME>` is not set"** — `api_key_env` names a credential that has no value.
This is reported before the request rather than surfacing as a bare `401`.

**Names come out wrong** — expected for words the model has never seen. Set
`prompt` to a short list of them.

**Recorder not found** — install `alsa-utils` (Linux) or `sox` (macOS), or set
`record_cmd`.

**Partials feel behind** — your endpoint is slower than `poll_ms`. Raise
`poll_ms`, use a smaller/faster model, or move off a distant hosted endpoint.

**A hold never stops, or SPACE does nothing.** Your terminal is not delivering
key-release events. `/voice` should have said so and used the toggle; if it did
arm and then got stuck, it stands down after 45 seconds and keeps what you said.
Set `no_push_to_talk = true`, or use a terminal from the table above.

**Typing a space starts a recording.** Raise `ptt_hold_ms`. Because the space is
typed on the press and only retracted if the hold promotes, raising it costs
typing nothing — it only means holding longer before dictation begins. Note that
audio is captured from the promotion, not from the press, so a very long
threshold means waiting that long before you start speaking.
