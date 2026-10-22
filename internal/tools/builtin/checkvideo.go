package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessiontemp"
)

const (
	CheckVideoName           = "check_video"
	checkVideoDecodeTimeout  = 10 * time.Minute
	checkVideoDefaultDurTol  = 0.5
	checkVideoAspectTol      = 0.01
	checkVideoDiagnosticSize = 512
	// checkVideoTruncationTol is how far decoded time may fall short of the
	// declared duration before the tail counts as missing.
	checkVideoTruncationTol = 0.15
)

func init() { tool.RegisterConditional(videoCheck{}) }

// videoCheck is technical acceptance: it probes and fully decodes one immutable
// snapshot. It never judges content, motion or sound, and says so in the report.
type videoCheck struct {
	media       readVideo
	sessionTemp *sessiontemp.Manager
	timeout     time.Duration
}

// CheckVideo binds check_video to the same read boundary and sandbox read_video uses.
func (w Workspace) CheckVideo() tool.Tool {
	return videoCheck{media: w.readVideo(), sessionTemp: w.SessionTemp}
}

type videoCheckArgs struct {
	Path              string   `json:"path"`
	ExpectedSHA256    string   `json:"expected_sha256,omitempty"`
	Duration          *float64 `json:"duration,omitempty"`
	DurationTolerance *float64 `json:"duration_tolerance,omitempty"`
	Width             int      `json:"width,omitempty"`
	Height            int      `json:"height,omitempty"`
	AspectRatio       *float64 `json:"aspect_ratio,omitempty"`
	Audio             string   `json:"audio,omitempty"`
}

type videoCheckResult struct {
	Name     string `json:"name"`
	Expected string `json:"expected"`
	Observed string `json:"observed"`
	Pass     bool   `json:"pass"`
}

type videoObserved struct {
	Duration      float64 `json:"duration,omitempty"`
	Decoded       float64 `json:"decoded_duration,omitempty"`
	Width         int     `json:"width,omitempty"`
	Height        int     `json:"height,omitempty"`
	DisplayAspect float64 `json:"display_aspect,omitempty"`
	Rotation      int     `json:"rotation,omitempty"`
	VideoCodec    string  `json:"video_codec,omitempty"`
	AudioCodec    string  `json:"audio_codec,omitempty"`
	HasAudio      bool    `json:"has_audio"`
}

// videoCheckReport is version 1 of the acceptance record a manifest cites.
type videoCheckReport struct {
	Version       int                `json:"version"`
	Path          string             `json:"path"`
	SHA256        string             `json:"sha256"`
	Bytes         int64              `json:"bytes"`
	Observed      videoObserved      `json:"observed"`
	Checks        []videoCheckResult `json:"checks"`
	Technical     string             `json:"technical"`
	TechnicalPass bool               `json:"technical_pass"`
	Failures      []string           `json:"failures"`
	Unchecked     []string           `json:"unchecked"`
	Diagnostic    string             `json:"diagnostic,omitempty"`
}

// videoUncheckedScopes are what a decode cannot establish about a video.
var videoUncheckedScopes = []string{"content", "motion", "audio_and_sync"}

func (videoCheck) Name() string   { return CheckVideoName }
func (videoCheck) ReadOnly() bool { return true }

func (videoCheck) Description() string {
	return "Technical acceptance of a local video: full decode plus duration, size, aspect and audio checks. Does not judge content, motion or sound."
}

func (videoCheck) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["path"],"properties":{"path":{"type":"string"},"expected_sha256":{"type":"string"},"duration":{"type":"number","description":"seconds"},"duration_tolerance":{"type":"number","description":"seconds, default 0.5"},"width":{"type":"integer"},"height":{"type":"integer"},"aspect_ratio":{"type":"number","description":"display width/height"},"audio":{"type":"string","enum":["any","required","forbidden"]}}}`)
}

func (c videoCheck) RootsFor(args json.RawMessage) []tool.Root { return c.media.RootsFor(args) }

func (c videoCheck) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p videoCheckArgs
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", errors.New("path is required")
	}
	switch p.Audio {
	case "", "any", "required", "forbidden":
	default:
		return "", fmt.Errorf("audio must be any, required or forbidden")
	}
	rp := resolveReadablePath(c.media.workDir, p.Path, c.media.paths)
	if err := c.media.refuse(rp); err != nil {
		return "", err
	}
	demuxer, err := c.media.sniff(rp)
	if err != nil {
		return "", err
	}
	ffmpeg, ffprobe, err := c.media.programs()
	if err != nil {
		return "", err
	}
	dir, release, err := c.snapshotDir(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	snap, sum, size, err := c.snapshot(rp, dir)
	if err != nil {
		return "", err
	}
	report := videoCheckReport{Version: 1, Path: rp.DisplayPath, SHA256: sum, Bytes: size, Checks: []videoCheckResult{}, Failures: []string{}, Unchecked: videoUncheckedScopes}
	if p.ExpectedSHA256 != "" {
		report.check("sha256", strings.ToLower(p.ExpectedSHA256), sum, strings.EqualFold(p.ExpectedSHA256, sum))
	}
	observed, probeErr := c.probe(ctx, ffprobe, demuxer, snap, dir)
	if probeErr != nil {
		report.fail("probe", probeErr.Error())
		return report.finish(false)
	}
	report.Observed = observed
	if observed.Width == 0 {
		report.fail("no_video_stream", "")
	}
	incomplete := c.decode(ctx, ffmpeg, demuxer, snap, dir, &report)
	report.compare(p, report.Observed)
	return report.finish(incomplete)
}

func (c videoCheck) snapshotDir(ctx context.Context) (string, func(), error) {
	m := sessiontemp.FromContext(ctx)
	if m == nil {
		m = c.sessionTemp
	}
	if m != nil {
		lease, err := m.Acquire()
		if err != nil {
			return "", nil, fmt.Errorf("%w: %w", errSessionTemp, err)
		}
		dir, err := os.MkdirTemp(lease.Dir(), "check-video-")
		if err != nil {
			lease.Release()
			return "", nil, err
		}
		return dir, func() { _ = os.RemoveAll(dir); lease.Release() }, nil
	}
	dir, err := os.MkdirTemp("", "reasonix-check-video-")
	if err != nil {
		return "", nil, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// snapshot copies the confined file once, hashing what it copies, so probe,
// decode and hash all describe the same bytes.
func (c videoCheck) snapshot(rp ResolvedPath, dir string) (string, string, int64, error) {
	src, err := os.Open(rp.Path)
	if err != nil {
		return "", "", 0, fmt.Errorf("read %s: %w", rp.DisplayPath, err)
	}
	defer src.Close()
	if err := confineOpened(c.media.readRoots, src); err != nil {
		return "", "", 0, err
	}
	limit := c.media.maxBytes
	if limit <= 0 {
		limit = readVideoMaxBytes
	}
	path := filepath.Join(dir, "snapshot"+filepath.Ext(rp.Path))
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", "", 0, err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(src, limit+1))
	if err := errors.Join(copyErr, dst.Close()); err != nil {
		return "", "", 0, err
	}
	if n > limit {
		return "", "", 0, tool.Refusal{Code: CodeVideoTooLarge, Message: fmt.Sprintf("%s is larger than %d bytes", rp.DisplayPath, limit)}
	}
	return path, hex.EncodeToString(hash.Sum(nil)), n, nil
}

func (c videoCheck) decode(ctx context.Context, ffmpeg, demuxer, snap, dir string, report *videoCheckReport) (incomplete bool) {
	argv := append([]string{ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-protocol_whitelist", "file"}, demuxArgs(demuxer)...)
	argv = append(argv, "-i", snap, "-map", "0:v:0", "-map", "0:a?", "-f", "null", "-", "-progress", "pipe:1", "-nostats")
	timeout := c.timeout
	if timeout <= 0 {
		timeout = checkVideoDecodeTimeout
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, tail, err := c.media.execIn(dctx, argv, 1<<20, dir)
	switch {
	case dctx.Err() != nil:
		report.fail("decode_incomplete", "decode did not finish: "+dctx.Err().Error())
		return true
	case err != nil:
		report.fail("decode", strings.TrimSpace(err.Error()+" "+tail))
		return false
	}
	// A cut container ends early without a decode error; only the time actually
	// decoded shows the missing tail.
	if decoded, ok := decodedSeconds(out); ok {
		report.Observed.Decoded = decoded
		if declared := report.Observed.Duration; declared > 0 && decoded < declared-checkVideoTruncationTol {
			report.fail("truncated", fmt.Sprintf("decoded %.3fs of a declared %.3fs", decoded, declared))
		}
	}
	return false
}

func (r *videoCheckReport) check(name, expected, observed string, pass bool) {
	r.Checks = append(r.Checks, videoCheckResult{Name: name, Expected: expected, Observed: observed, Pass: pass})
	if !pass {
		r.Failures = append(r.Failures, name)
	}
}

func (r *videoCheckReport) fail(code, diagnostic string) {
	r.Failures = append(r.Failures, code)
	if diagnostic = strings.TrimSpace(diagnostic); diagnostic != "" && r.Diagnostic == "" {
		r.Diagnostic = diagnostic[:min(len(diagnostic), checkVideoDiagnosticSize)]
	}
}

func (r *videoCheckReport) compare(p videoCheckArgs, o videoObserved) {
	if p.Duration != nil {
		tol := checkVideoDefaultDurTol
		if p.DurationTolerance != nil && *p.DurationTolerance >= 0 {
			tol = *p.DurationTolerance
		}
		got := o.Duration
		if o.Decoded > 0 {
			got = o.Decoded
		}
		observed := "unknown"
		if got > 0 {
			observed = fmt.Sprintf("%.3f", got)
		}
		r.check("duration", fmt.Sprintf("%.3f±%.3f", *p.Duration, tol), observed, got > 0 && math.Abs(got-*p.Duration) <= tol)
	}
	if p.Width > 0 {
		r.check("width", fmt.Sprint(p.Width), fmt.Sprint(o.Width), o.Width == p.Width)
	}
	if p.Height > 0 {
		r.check("height", fmt.Sprint(p.Height), fmt.Sprint(o.Height), o.Height == p.Height)
	}
	if p.AspectRatio != nil {
		r.check("aspect_ratio", fmt.Sprintf("%.4f", *p.AspectRatio), fmt.Sprintf("%.4f", o.DisplayAspect),
			o.DisplayAspect > 0 && math.Abs(o.DisplayAspect-*p.AspectRatio) <= checkVideoAspectTol*max(1, *p.AspectRatio))
	}
	switch p.Audio {
	case "required":
		r.check("audio", "present", fmt.Sprint(o.HasAudio), o.HasAudio)
	case "forbidden":
		r.check("audio", "absent", fmt.Sprint(o.HasAudio), !o.HasAudio)
	}
}

func (r *videoCheckReport) finish(incomplete bool) (string, error) {
	switch {
	case incomplete:
		r.Technical = "incomplete"
	case len(r.Failures) > 0:
		r.Technical = "fail"
	default:
		r.Technical, r.TechnicalPass = "pass", true
	}
	out, err := json.Marshal(r)
	return string(out), err
}
