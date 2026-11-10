package builtin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

var jpegBytes = []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0xff, 0xd9}

// mp4Head is enough of an MP4 for the container to be recognised by its bytes.
func mp4Head() []byte {
	return append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 64)...)
}

type fakeMedia struct {
	calls [][]string
	probe string
	frame func(call int) ([]byte, string, error)
}

func (f *fakeMedia) run(_ context.Context, argv []string) ([]byte, string, error) {
	f.calls = append(f.calls, argv)
	if strings.Contains(filepath.Base(argv[0]), "ffprobe") {
		return []byte(f.probe), "", nil
	}
	if f.frame != nil {
		return f.frame(len(f.calls) - 2)
	}
	return jpegBytes, "", nil
}

const goodProbe = `{"streams":[{"codec_type":"audio","codec_name":"aac"},{"codec_type":"video","codec_name":"h264","width":1920,"height":1080}],"format":{"duration":"80.0","format_name":"mov,mp4,m4a,3gp,3g2,mj2"}}`

func videoTool(t *testing.T, media *fakeMedia) (readVideo, string) {
	t.Helper()
	dir := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), mp4Head(), 0o644); err != nil {
		t.Fatal(err)
	}
	return readVideo{
		workDir: dir,
		run:     media.run,
		look:    func(name string) (string, error) { return filepath.Join("/opt/bin", name), nil },
	}, dir
}

func callVideo(t *testing.T, v readVideo, args map[string]any) (string, []string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	return v.ExecuteWithImages(context.Background(), raw)
}

func refusalCode(err error) string {
	var r tool.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

func TestReadVideoReturnsEvenlySpreadFramesAsImages(t *testing.T) {
	media := &fakeMedia{probe: goodProbe}
	v, _ := videoTool(t, media)
	text, images, err := callVideo(t, v, map[string]any{"path": "clip.mp4", "frames": 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 4 {
		t.Fatalf("%d images, want 4", len(images))
	}
	want := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpegBytes)
	for i, img := range images {
		if img != want {
			t.Errorf("image %d is not the frame's JPEG as a data URL: %q", i, img)
		}
	}
	for _, label := range []string{"[frame 1/4 at 10.0s]", "[frame 2/4 at 30.0s]", "[frame 3/4 at 50.0s]", "[frame 4/4 at 70.0s]"} {
		if !strings.Contains(text, label) {
			t.Errorf("text lacks %q:\n%s", label, text)
		}
	}
	if !strings.Contains(text, "1920x1080") || !strings.Contains(text, "80.0s") {
		t.Errorf("text does not describe the video:\n%s", text)
	}
}

func TestReadVideoNeverLetsTheMediaProgramReachBeyondTheFile(t *testing.T) {
	media := &fakeMedia{probe: goodProbe}
	v, dir := videoTool(t, media)
	if _, _, err := callVideo(t, v, map[string]any{"path": "clip.mp4", "frames": 1}); err != nil {
		t.Fatal(err)
	}
	if len(media.calls) < 2 {
		t.Fatalf("calls %v", media.calls)
	}
	abs := filepath.Join(dir, "clip.mp4")
	for _, argv := range media.calls {
		joined := strings.Join(argv, " ")
		if !strings.Contains(joined, "-protocol_whitelist file") {
			t.Errorf("%v does not restrict protocols to files", argv)
		}
		if !strings.Contains(joined, "-f mov,mp4,m4a,3gp,3g2,mj2") {
			t.Errorf("%v does not force the demuxer the file's own bytes named", argv)
		}
		if !strings.Contains(joined, "-enable_drefs 0") || !strings.Contains(joined, "-use_absolute_path 0") {
			t.Errorf("%v does not turn off external references for MP4/MOV", argv)
		}
		at := -1
		for i, a := range argv {
			if a == "-i" {
				at = i
			}
		}
		if at < 0 || at+1 >= len(argv) || argv[at+1] != abs {
			t.Errorf("%v does not read exactly the resolved file %s", argv, abs)
		}
		for _, a := range argv {
			if strings.Contains(a, "://") || strings.HasPrefix(a, "concat:") {
				t.Errorf("%v carries a URL or concat spec: %q", argv, a)
			}
		}
	}
}

func TestReadVideoClampsFramesAndSaysSo(t *testing.T) {
	media := &fakeMedia{probe: goodProbe}
	v, _ := videoTool(t, media)
	text, images, err := callVideo(t, v, map[string]any{"path": "clip.mp4", "frames": 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != readVideoMaxFrames {
		t.Fatalf("%d images, want the cap %d", len(images), readVideoMaxFrames)
	}
	if !strings.Contains(text, "capped") {
		t.Errorf("the clamp is not mentioned:\n%s", text)
	}
}

func TestReadVideoHonoursAWindow(t *testing.T) {
	media := &fakeMedia{probe: goodProbe}
	v, _ := videoTool(t, media)
	text, images, err := callVideo(t, v, map[string]any{"path": "clip.mp4", "frames": 2, "start": 20, "end": 60})
	if err != nil || len(images) != 2 {
		t.Fatalf("%v %d", err, len(images))
	}
	if !strings.Contains(text, "at 30.0s") || !strings.Contains(text, "at 50.0s") {
		t.Errorf("frames are not spread over [20,60]:\n%s", text)
	}
}

func TestReadVideoNamesAMissingFFmpegByCode(t *testing.T) {
	media := &fakeMedia{probe: goodProbe}
	v, _ := videoTool(t, media)
	v.look = func(name string) (string, error) {
		if name == "ffprobe" {
			return "", errors.New("not found")
		}
		return "/opt/bin/" + name, nil
	}
	_, _, err := callVideo(t, v, map[string]any{"path": "clip.mp4"})
	if refusalCode(err) != CodeVideoFFmpegMissing {
		t.Fatalf("code %q (%v), want %s", refusalCode(err), err, CodeVideoFFmpegMissing)
	}
	if len(media.calls) != 0 {
		t.Error("a program ran although the tools were not found")
	}
}

func TestReadVideoRefusesWhatItsBytesDoNotSayIsAVideo(t *testing.T) {
	media := &fakeMedia{probe: goodProbe}
	v, dir := videoTool(t, media)
	// A playlist is how ffmpeg is made to read other files and the network.
	playlist := "#EXTM3U\n#EXTINF:1,\nfile:///etc/passwd\n"
	for name, body := range map[string][]byte{"list.mp4": []byte(playlist), "concat.mp4": []byte("ffconcat version 1.0\nfile '/etc/passwd'\n"), "empty.mp4": nil, "text.mkv": []byte("hello world, not a video at all")} {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := callVideo(t, v, map[string]any{"path": name})
		if refusalCode(err) != CodeVideoUnsupported {
			t.Errorf("%s: code %q (%v), want %s", name, refusalCode(err), err, CodeVideoUnsupported)
		}
	}
	if len(media.calls) != 0 {
		t.Error("a program ran on a file whose bytes are not a video")
	}
}

func TestReadVideoRecognisesTheContainersByTheirBytes(t *testing.T) {
	ts := make([]byte, 188*3)
	for i := range 3 {
		ts[i*188] = 0x47
	}
	for name, c := range map[string]struct {
		head []byte
		want string
	}{
		"mp4":  {mp4Head(), "mov,mp4,m4a,3gp,3g2,mj2"},
		"webm": {append([]byte{0x1a, 0x45, 0xdf, 0xa3}, make([]byte, 32)...), "matroska,webm"},
		"avi":  {append([]byte("RIFF\x00\x00\x00\x00AVI LIST"), make([]byte, 32)...), "avi"},
		"flv":  {append([]byte("FLV\x01\x05\x00\x00\x00\x09"), make([]byte, 32)...), "flv"},
		"ts":   {ts, "mpegts"},
		"junk": {[]byte("not a video"), ""},
	} {
		if got := videoDemuxer(c.head); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func TestReadVideoRefusesWhatTheReadBoundaryRefuses(t *testing.T) {
	media := &fakeMedia{probe: goodProbe}
	v, dir := videoTool(t, media)
	v.forbidRoots = realRoots([]string{dir})
	if _, _, err := callVideo(t, v, map[string]any{"path": "clip.mp4"}); err == nil {
		t.Fatal("a forbidden folder was read")
	}
	v.forbidRoots = nil
	other := testenv.TempDir(t)
	v.readRoots = realRoots([]string{other})
	_, _, err := callVideo(t, v, map[string]any{"path": "clip.mp4"})
	if refusalCode(err) != CodeReadOutsideScope {
		t.Fatalf("code %q (%v), want %s", refusalCode(err), err, CodeReadOutsideScope)
	}
	if len(media.calls) != 0 {
		t.Error("a program ran on a file the boundary refused")
	}
}

func TestReadVideoRefusesAFileOverTheSizeBound(t *testing.T) {
	media := &fakeMedia{probe: goodProbe}
	v, _ := videoTool(t, media)
	v.maxBytes = 16
	_, _, err := callVideo(t, v, map[string]any{"path": "clip.mp4"})
	if refusalCode(err) != CodeVideoTooLarge {
		t.Fatalf("code %q (%v), want %s", refusalCode(err), err, CodeVideoTooLarge)
	}
}

func TestReadVideoNamesAFileWithNoVideoStream(t *testing.T) {
	media := &fakeMedia{probe: `{"streams":[{"codec_type":"audio","codec_name":"aac"}],"format":{"duration":"10"}}`}
	v, _ := videoTool(t, media)
	_, _, err := callVideo(t, v, map[string]any{"path": "clip.mp4"})
	if refusalCode(err) != CodeVideoNoStream {
		t.Fatalf("code %q (%v), want %s", refusalCode(err), err, CodeVideoNoStream)
	}
}

func TestReadVideoKeepsTheFramesItGotWhenALaterOneFails(t *testing.T) {
	media := &fakeMedia{probe: goodProbe, frame: func(call int) ([]byte, string, error) {
		if call >= 2 {
			return nil, "decode error", errors.New("exit status 1")
		}
		return jpegBytes, "", nil
	}}
	v, _ := videoTool(t, media)
	text, images, err := callVideo(t, v, map[string]any{"path": "clip.mp4", "frames": 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 || !strings.Contains(text, "could not be read") {
		t.Errorf("%d images; text:\n%s", len(images), text)
	}
	allBad := &fakeMedia{probe: goodProbe, frame: func(int) ([]byte, string, error) { return nil, "boom", errors.New("exit status 1") }}
	v2, _ := videoTool(t, allBad)
	if _, _, err := callVideo(t, v2, map[string]any{"path": "clip.mp4"}); refusalCode(err) != CodeVideoDecodeFailed {
		t.Errorf("code %q (%v), want %s", refusalCode(err), err, CodeVideoDecodeFailed)
	}
}

func TestReadVideoRejectsOutputThatIsNotAJPEG(t *testing.T) {
	media := &fakeMedia{probe: goodProbe, frame: func(int) ([]byte, string, error) { return []byte("not a jpeg"), "", nil }}
	v, _ := videoTool(t, media)
	if _, _, err := callVideo(t, v, map[string]any{"path": "clip.mp4"}); refusalCode(err) != CodeVideoDecodeFailed {
		t.Fatalf("code %q (%v), want %s", refusalCode(err), err, CodeVideoDecodeFailed)
	}
}

func TestReadVideoIsReadOnlyAnImageToolAndRegistered(t *testing.T) {
	var v tool.Tool = readVideo{}
	if !v.ReadOnly() {
		t.Error("read_video must be read-only")
	}
	if _, ok := v.(tool.ImageTool); !ok {
		t.Error("read_video must return its frames through the image channel")
	}
	found := false
	for _, b := range tool.Builtins() {
		found = found || b.Name() == "read_video"
	}
	if !found {
		t.Error("read_video is not registered as a built-in")
	}
}

// The two options belong to the MP4/MOV demuxer; given to another one ffmpeg
// refuses the whole command as an unknown option.
func TestExternalReferenceOptionsAreOnlyGivenToTheMovFamily(t *testing.T) {
	for _, d := range []string{"matroska,webm", "avi", "flv", "asf", "mpeg", "mpegts", "ogg"} {
		if args := strings.Join(demuxArgs(d), " "); strings.Contains(args, "drefs") || strings.Contains(args, "absolute") {
			t.Errorf("%s was given the MP4 options: %s", d, args)
		}
	}
	if args := strings.Join(demuxArgs(movDemuxer), " "); !strings.Contains(args, "-enable_drefs 0") {
		t.Errorf("the MOV family was not: %s", args)
	}
}
