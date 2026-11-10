package builtin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// These run the real ffmpeg and are skipped where it is not installed: the unit
// tests beside them stand in for it, and this is what says the stand-in is a
// faithful one.
func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, name := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s is not installed", name)
		}
	}
}

func makeClip(t *testing.T, name, format string, size string, seconds string) string {
	t.Helper()
	path := filepath.Join(testenv.TempDir(t), name)
	out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size="+size+":rate=10:duration="+seconds,
		"-c:v", "mpeg4", "-f", format, path).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg could not make the clip: %v\n%s", err, out)
	}
	return path
}

func decodeDataURL(t *testing.T, url string) (width, height int) {
	t.Helper()
	const prefix = "data:image/jpeg;base64,"
	if !strings.HasPrefix(url, prefix) {
		t.Fatalf("not a JPEG data URL: %.40s", url)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, prefix))
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the frame is not a decodable JPEG: %v", err)
	}
	b := img.Bounds()
	return b.Dx(), b.Dy()
}

func TestReadVideoWithRealFFmpegSamplesDecodableFrames(t *testing.T) {
	requireFFmpeg(t)
	for _, c := range []struct{ name, format string }{{"clip.mp4", "mp4"}, {"clip.avi", "avi"}, {"clip.mkv", "matroska"}} {
		t.Run(c.format, func(t *testing.T) {
			path := makeClip(t, c.name, c.format, "320x240", "4")
			raw, _ := json.Marshal(map[string]any{"path": path, "frames": 3})
			text, images, err := readVideo{}.ExecuteWithImages(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			if len(images) != 3 {
				t.Fatalf("%d frames, want 3\n%s", len(images), text)
			}
			for _, img := range images {
				if w, h := decodeDataURL(t, img); w != 320 || h != 240 {
					t.Errorf("frame is %dx%d, want the clip's own 320x240", w, h)
				}
			}
			if !strings.Contains(text, "320x240") || !strings.Contains(text, "[frame 3/3 at ") {
				t.Errorf("text does not describe the sampling:\n%s", text)
			}
		})
	}
}

func TestReadVideoWithRealFFmpegShrinksWideFrames(t *testing.T) {
	requireFFmpeg(t)
	path := makeClip(t, "wide.mp4", "mp4", "1280x720", "2")
	raw, _ := json.Marshal(map[string]any{"path": path, "frames": 1})
	_, images, err := readVideo{}.ExecuteWithImages(context.Background(), raw)
	if err != nil || len(images) != 1 {
		t.Fatalf("%v %d", err, len(images))
	}
	if w, h := decodeDataURL(t, images[0]); w != readVideoFrameWidth || h != 432 {
		t.Errorf("frame is %dx%d, want %dx432", w, h, readVideoFrameWidth)
	}
}

func TestReadVideoWithRealFFmpegDoesNotFollowAPlaylistInsideAnMP4(t *testing.T) {
	requireFFmpeg(t)
	dir := testenv.TempDir(t)
	secret := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(secret, []byte("do not read me"), 0o644); err != nil {
		t.Fatal(err)
	}
	// ftyp makes the bytes pass for MP4; the demuxer is forced to MP4 as well, so
	// the playlist text that follows is never interpreted as a playlist.
	head := append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, []byte("\n#EXTM3U\n#EXTINF:1,\nfile://"+secret+"\n")...)
	path := filepath.Join(dir, "evil.mp4")
	if err := os.WriteFile(path, head, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"path": path})
	text, images, err := readVideo{}.ExecuteWithImages(context.Background(), raw)
	if err == nil {
		t.Fatalf("a hostile file produced frames: %d\n%s", len(images), text)
	}
	if strings.Contains(err.Error(), "do not read me") {
		t.Error("the file the playlist pointed at was read")
	}
}
