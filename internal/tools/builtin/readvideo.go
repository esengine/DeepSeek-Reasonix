package builtin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/base/proc"
	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
)

// Identities of the refusals read_video owns, so a caller never classifies on a
// sentence.
const (
	CodeVideoFFmpegMissing = "video.ffmpeg_missing"
	CodeVideoUnsupported   = "video.unsupported_container"
	CodeVideoTooLarge      = "video.too_large"
	CodeVideoNoStream      = "video.no_video_stream"
	CodeVideoProbeFailed   = "video.probe_failed"
	CodeVideoDecodeFailed  = "video.decode_failed"
)

const (
	readVideoDefaultFrames = 8
	readVideoMaxFrames     = 12
	readVideoFrameWidth    = 768
	readVideoMaxBytes      = int64(4) << 30
	readVideoMaxFrameBytes = 4 << 20
	readVideoMaxProbeBytes = 1 << 20
	readVideoProbeTimeout  = 20 * time.Second
	readVideoFrameTimeout  = 30 * time.Second
	readVideoSniffBytes    = 4096
)

// movDemuxer is ffmpeg's name for the MP4/MOV family.
const movDemuxer = "mov,mp4,m4a,3gp,3g2,mj2"

func init() { tool.RegisterBuiltin(readVideo{}) }

// readVideo looks at a video by sampling still frames with the host's ffmpeg. It
// reads one confined file and returns pictures; audio and motion are not seen.
type readVideo struct {
	workDir     string
	netRoots    []string
	readRoots   []string
	paths       *PathResolver
	forbidRoots []string
	sb          sandbox.Spec
	// maxBytes, run and look are for tests; zero values mean the real ones.
	maxBytes int64
	run      func(ctx context.Context, argv []string) (stdout []byte, stderrTail string, err error)
	look     func(name string) (string, error)
}

func (readVideo) Name() string   { return "read_video" }
func (readVideo) ReadOnly() bool { return true }

func (readVideo) Description() string {
	return "Look at a video: native if the model watches video, else ffmpeg frames."
}

func (readVideo) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"url":{"type":"string","description":"public https address the user wrote"},"mode":{"type":"string","enum":["auto","frames","native"]},"frames":{"type":"integer","description":"max 12"},"start":{"type":"number","description":"seconds"},"end":{"type":"number","description":"seconds"}},"additionalProperties":false}`)
}

func (r readVideo) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	text, _, err := r.ExecuteWithImages(ctx, args)
	return text, err
}

func (r readVideo) ExecuteWithImages(ctx context.Context, args json.RawMessage) (string, []string, error) {
	var p struct {
		Path   string   `json:"path"`
		Frames int      `json:"frames"`
		Start  *float64 `json:"start"`
		End    *float64 `json:"end"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", nil, fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", nil, errors.New("path is required")
	}
	rp := resolveReadablePath(r.workDir, p.Path, r.paths)
	if err := r.refuse(rp); err != nil {
		return "", nil, err
	}
	demuxer, err := r.sniff(rp)
	if err != nil {
		return "", nil, err
	}
	ffmpeg, ffprobe, err := r.programs()
	if err != nil {
		return "", nil, err
	}
	info, err := r.probe(ctx, ffprobe, demuxer, rp.Path)
	if err != nil {
		return "", nil, err
	}

	frames, capped := p.Frames, false
	if frames <= 0 {
		frames = readVideoDefaultFrames
	}
	if frames > readVideoMaxFrames {
		frames, capped = readVideoMaxFrames, true
	}
	from, to := 0.0, info.duration
	if p.Start != nil {
		from = math.Max(0, *p.Start)
	}
	if p.End != nil && (info.duration <= 0 || *p.End < info.duration) {
		to = math.Max(0, *p.End)
	}
	times := sampleTimes(from, to, frames)

	var images []string
	var lines []string
	var failed []string
	for i, at := range times {
		jpeg, err := r.frame(ctx, ffmpeg, demuxer, rp.Path, at)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%.1fs", at))
			if len(images) == 0 && i == len(times)-1 {
				return "", nil, err
			}
			continue
		}
		images = append(images, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(jpeg))
		lines = append(lines, fmt.Sprintf("[frame %d/%d at %.1fs]", i+1, len(times), at))
	}
	if len(images) == 0 {
		return "", nil, tool.Refusal{Code: CodeVideoDecodeFailed, Message: "no frame could be read from " + rp.DisplayPath}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "video %s: %dx%d", rp.DisplayPath, info.width, info.height)
	if info.duration > 0 {
		fmt.Fprintf(&b, ", %.1fs", info.duration)
	}
	fmt.Fprintf(&b, ". %d frame(s) sampled", len(images))
	if len(times) > 1 {
		fmt.Fprintf(&b, " evenly between %.1fs and %.1fs", from, to)
	}
	b.WriteString(". Audio and motion are not seen.")
	if info.duration <= 0 {
		b.WriteString(" The video's length is unknown, so only its first moment was sampled.")
	}
	if capped {
		fmt.Fprintf(&b, " frames was capped at %d.", readVideoMaxFrames)
	}
	if len(failed) > 0 {
		fmt.Fprintf(&b, " Frames at %s could not be read.", strings.Join(failed, ", "))
	}
	b.WriteString("\n")
	b.WriteString(strings.Join(lines, "\n"))
	return b.String(), images, nil
}

// refuse is the read boundary, the same one read_file applies.
func (r readVideo) refuse(rp ResolvedPath) error {
	if err := rp.refuseNetwork(r.netRoots); err != nil {
		return err
	}
	if err := confineScope(r.readRoots, rp.Path); err != nil {
		return err
	}
	if confineRead(r.forbidRoots, rp.Path) {
		return &os.PathError{Op: "open", Path: rp.Path, Err: os.ErrNotExist}
	}
	return nil
}

// sniff opens the file inside the read boundary and names its container from
// its own bytes. The name decides which demuxer ffmpeg is forced to use: a
// playlist or concat script wearing a video's extension is how ffmpeg gets
// pointed at other files and at the network, and it is never parsed.
func (r readVideo) sniff(rp ResolvedPath) (string, error) {
	f, err := os.Open(rp.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", rp.DisplayPath, err)
	}
	defer f.Close()
	if err := confineOpened(r.readRoots, f); err != nil {
		return "", err
	}
	st, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("read %s: %w", rp.DisplayPath, err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("%s is a directory, not a video file", rp.DisplayPath)
	}
	limit := r.maxBytes
	if limit <= 0 {
		limit = readVideoMaxBytes
	}
	if st.Size() > limit {
		return "", tool.Refusal{Code: CodeVideoTooLarge, Message: fmt.Sprintf("%s is %d bytes; read_video reads files up to %d bytes", rp.DisplayPath, st.Size(), limit)}
	}
	head := make([]byte, readVideoSniffBytes)
	n, _ := io.ReadFull(f, head)
	demuxer := videoDemuxer(head[:n])
	if demuxer == "" {
		return "", tool.Refusal{Code: CodeVideoUnsupported, Message: fmt.Sprintf("%s is not a video container read_video recognises (MP4/MOV, Matroska/WebM, AVI, FLV, ASF, MPEG-PS/TS, Ogg)", rp.DisplayPath)}
	}
	return demuxer, nil
}

// videoDemuxer names the ffmpeg demuxer for a container by its magic bytes, or
// "" for anything it does not recognise.
func videoDemuxer(head []byte) string {
	switch {
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		return movDemuxer
	case len(head) >= 4 && bytes.Equal(head[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return "matroska,webm"
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "AVI ":
		return "avi"
	case len(head) >= 4 && string(head[:3]) == "FLV" && head[3] == 1:
		return "flv"
	case len(head) >= 16 && bytes.Equal(head[:16], []byte{0x30, 0x26, 0xb2, 0x75, 0x8e, 0x66, 0xcf, 0x11, 0xa6, 0xd9, 0x00, 0xaa, 0x00, 0x62, 0xce, 0x6c}):
		return "asf"
	case len(head) >= 4 && bytes.Equal(head[:4], []byte{0x00, 0x00, 0x01, 0xba}):
		return "mpeg"
	case len(head) >= 4 && string(head[:4]) == "OggS":
		return "ogg"
	case len(head) >= 3*188 && head[0] == 0x47 && head[188] == 0x47 && head[376] == 0x47:
		return "mpegts"
	}
	return ""
}

func (r readVideo) programs() (ffmpeg, ffprobe string, err error) {
	look := r.look
	if look == nil {
		look = exec.LookPath
	}
	ffmpeg, e1 := look("ffmpeg")
	ffprobe, e2 := look("ffprobe")
	if e1 != nil || e2 != nil {
		return "", "", tool.Refusal{Code: CodeVideoFFmpegMissing, Message: "read_video needs ffmpeg and ffprobe on this machine's PATH and found at least one of them missing. Install ffmpeg (it includes ffprobe), then try again; no other tool can read a video"}
	}
	return ffmpeg, ffprobe, nil
}

type videoInfo struct {
	width, height int
	duration      float64
}

// demuxArgs forces the demuxer the file's own bytes named, and for MP4/MOV turns
// off the two ways that format can pull in data from other files: external data
// references and absolute paths. Both are off by default; this does not rely on
// the default.
func demuxArgs(demuxer string) []string {
	args := []string{"-f", demuxer}
	if demuxer == movDemuxer {
		args = append(args, "-enable_drefs", "0", "-use_absolute_path", "0")
	}
	return args
}

func (r readVideo) probe(ctx context.Context, ffprobe, demuxer, path string) (videoInfo, error) {
	argv := append([]string{ffprobe, "-v", "error", "-protocol_whitelist", "file"}, demuxArgs(demuxer)...)
	argv = append(argv, "-print_format", "json", "-show_entries", "format=duration:stream=codec_type,width,height", "-i", path)
	ctx, cancel := context.WithTimeout(ctx, readVideoProbeTimeout)
	defer cancel()
	out, tail, err := r.exec(ctx, argv, readVideoMaxProbeBytes)
	if err != nil {
		return videoInfo{}, tool.Refusal{Code: CodeVideoProbeFailed, Message: probeMessage(err, tail)}
	}
	var doc struct {
		Streams []struct {
			Type   string `json:"codec_type"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return videoInfo{}, tool.Refusal{Code: CodeVideoProbeFailed, Message: "ffprobe did not answer in the form read_video asks for"}
	}
	var info videoInfo
	found := false
	for _, s := range doc.Streams {
		if s.Type == "video" {
			info.width, info.height, found = s.Width, s.Height, true
			break
		}
	}
	if !found {
		return videoInfo{}, tool.Refusal{Code: CodeVideoNoStream, Message: "the file has no video stream, only audio or data"}
	}
	if d, err := strconv.ParseFloat(doc.Format.Duration, 64); err == nil && d > 0 && !math.IsInf(d, 0) {
		info.duration = d
	}
	return info, nil
}

func probeMessage(err error, tail string) string {
	msg := "ffprobe could not read the file: " + err.Error()
	if tail = strings.TrimSpace(tail); tail != "" {
		msg += " (" + tail + ")"
	}
	return msg
}

func (r readVideo) frame(ctx context.Context, ffmpeg, demuxer, path string, at float64) ([]byte, error) {
	argv := append([]string{ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin", "-protocol_whitelist", "file"}, demuxArgs(demuxer)...)
	argv = append(argv,
		"-ss", strconv.FormatFloat(at, 'f', 3, 64), "-i", path,
		"-map", "0:v:0", "-an", "-sn", "-dn", "-frames:v", "1",
		"-vf", fmt.Sprintf("scale=w='min(%d,iw)':h=-2", readVideoFrameWidth),
		"-c:v", "mjpeg", "-q:v", "4", "-f", "image2pipe", "pipe:1",
	)
	ctx, cancel := context.WithTimeout(ctx, readVideoFrameTimeout)
	defer cancel()
	out, tail, err := r.exec(ctx, argv, readVideoMaxFrameBytes)
	if err != nil {
		return nil, tool.Refusal{Code: CodeVideoDecodeFailed, Message: fmt.Sprintf("ffmpeg could not read the frame at %.1fs: %s", at, strings.TrimSpace(strings.Join([]string{err.Error(), tail}, " ")))}
	}
	if len(out) < 4 || out[0] != 0xff || out[1] != 0xd8 {
		return nil, tool.Refusal{Code: CodeVideoDecodeFailed, Message: fmt.Sprintf("ffmpeg gave no picture for the frame at %.1fs", at)}
	}
	return out, nil
}

// sampleTimes spreads n sample points over [from, to] at the middle of n equal
// slices, so the last one never sits on the end of the file. A window with no
// length, or an unknown one, is the single moment it names.
func sampleTimes(from, to float64, n int) []float64 {
	if n <= 1 || to <= from {
		return []float64{from}
	}
	step := (to - from) / float64(n)
	out := make([]float64, n)
	for i := range out {
		out[i] = from + (float64(i)+0.5)*step
	}
	return out
}

// exec runs one media program under the workspace's OS sandbox when there is
// one, with the secrets-scrubbed environment, and reads at most limit bytes of
// its output.
func (r readVideo) exec(ctx context.Context, argv []string, limit int64) ([]byte, string, error) {
	return r.execIn(ctx, argv, limit, "")
}

// execIn is exec with tempDir mapped into the sandbox, for a program that must
// read a snapshot the host wrote there.
func (r readVideo) execIn(ctx context.Context, argv []string, limit int64, tempDir string) ([]byte, string, error) {
	if r.run != nil {
		return r.run(ctx, argv)
	}
	prepared := sandbox.PrepareArgs(r.sb, argv, tempDir)
	cmd := exec.CommandContext(ctx, prepared.Argv[0], prepared.Argv[1:]...)
	cmd.Env = applyEnvOverrides(secrets.ProcessEnv(), prepared.EnvOverrides)
	proc.HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitedBuffer{buf: &stderr, max: 2048}
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	out, _ := io.ReadAll(io.LimitReader(stdout, limit+1))
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	if int64(len(out)) > limit {
		return nil, stderr.String(), fmt.Errorf("output is larger than %d bytes", limit)
	}
	if waitErr != nil {
		return nil, stderr.String(), waitErr
	}
	return out, stderr.String(), nil
}

// limitedBuffer keeps the first max bytes written and drops the rest, so a
// program that is noisy on stderr cannot grow the host's memory.
type limitedBuffer struct {
	buf *bytes.Buffer
	max int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := l.max - l.buf.Len(); room > 0 {
		l.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}
