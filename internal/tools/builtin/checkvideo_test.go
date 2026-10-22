package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckVideoFullDecodeAndExplicitUncheckedScopes(t *testing.T) {
	m := &fakeMedia{probe: goodProbe}
	r, _ := videoTool(t, m)
	checker := videoCheck{media: r}
	out, err := checker.Execute(context.Background(), json.RawMessage(`{"path":"clip.mp4","duration":80,"duration_tolerance":0.1,"width":1920,"height":1080,"aspect_ratio":1.7777778,"audio":"required"}`))
	if err != nil {
		t.Fatal(err)
	}
	var report videoCheckReport
	if err := json.Unmarshal([]byte(out), &report); err != nil || !report.TechnicalPass || report.SHA256 == "" || len(report.Unchecked) != 3 {
		t.Fatalf("report %s: %v", out, err)
	}
	if len(m.calls) != 2 {
		t.Fatalf("calls %v", m.calls)
	}
	decode := strings.Join(m.calls[1], " ")
	for _, arg := range []string{"-xerror", "-protocol_whitelist file", "-f null", "-map 0:v:0", "-map 0:a?"} {
		if !strings.Contains(decode, arg) {
			t.Errorf("missing %s in %s", arg, decode)
		}
	}
	if strings.Contains(decode, "-frames:") {
		t.Fatal("sample decode reported as full decode")
	}
}

func TestCheckVideoReportsMismatchAndDecodeFailure(t *testing.T) {
	for _, tc := range []struct {
		name, args string
		decodeErr  error
		failure    string
	}{
		{"duration", `{"duration":10}`, nil, "duration"},
		{"dimensions", `{"width":1280,"height":720}`, nil, "width"},
		{"ratio", `{"aspect_ratio":1}`, nil, "aspect_ratio"},
		{"audio", `{"audio":"forbidden"}`, nil, "audio"},
		{"corrupt tail", `{}`, errors.New("decode failed"), "decode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &fakeMedia{probe: goodProbe, frame: func(int) ([]byte, string, error) { return nil, "bad packet", tc.decodeErr }}
			r, _ := videoTool(t, m)
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.args), &args)
			args["path"] = "clip.mp4"
			raw, _ := json.Marshal(args)
			out, err := (videoCheck{media: r}).Execute(context.Background(), raw)
			if err != nil {
				t.Fatal(err)
			}
			var report videoCheckReport
			if err := json.Unmarshal([]byte(out), &report); err != nil || report.TechnicalPass || !strings.Contains(strings.Join(report.Failures, ","), tc.failure) {
				t.Fatalf("%s: %v", out, err)
			}
		})
	}
}

func TestCheckVideoConfinesSourceAndDecodesImmutableSnapshot(t *testing.T) {
	m := &fakeMedia{probe: goodProbe}
	r, dir := videoTool(t, m)
	r.readRoots = realRoots([]string{dir})
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outside, mp4Head(), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"path": outside})
	if _, err := (videoCheck{media: r}).Execute(context.Background(), raw); err == nil {
		t.Fatal("outside file read")
	}
	if len(m.calls) != 0 {
		t.Fatal("decoder launched before confinement")
	}
	var snapshot string
	r.run = func(_ context.Context, argv []string) ([]byte, string, error) {
		for i, arg := range argv {
			if arg == "-i" {
				snapshot = argv[i+1]
			}
		}
		if strings.Contains(filepath.Base(argv[0]), "ffprobe") {
			if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("changed"), 0o600); err != nil {
				t.Fatal(err)
			}
			return []byte(goodProbe), "", nil
		}
		data, err := os.ReadFile(snapshot)
		if err != nil || string(data) != string(mp4Head()) {
			t.Fatalf("snapshot mutated: %v", err)
		}
		return nil, "", nil
	}
	out, err := (videoCheck{media: r}).Execute(context.Background(), json.RawMessage(`{"path":"clip.mp4"}`))
	if err != nil || !strings.Contains(out, "sha256") {
		t.Fatalf("%s: %v", out, err)
	}
	if _, err := os.Stat(snapshot); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("snapshot not cleaned")
	}
}

func TestCheckVideoTimeoutIsIncompleteNotPass(t *testing.T) {
	m := &fakeMedia{probe: goodProbe, frame: func(int) ([]byte, string, error) {
		time.Sleep(50 * time.Millisecond)
		return nil, "", context.DeadlineExceeded
	}}
	r, _ := videoTool(t, m)
	out, err := (videoCheck{media: r, timeout: time.Millisecond}).Execute(context.Background(), json.RawMessage(`{"path":"clip.mp4"}`))
	if err != nil {
		t.Fatal(err)
	}
	var report videoCheckReport
	if err := json.Unmarshal([]byte(out), &report); err != nil || report.TechnicalPass || report.Technical != "incomplete" {
		t.Fatalf("timeout report %s: %v", out, err)
	}
}

func TestCheckVideoDisplayAspectHonoursRotationAndPixelShape(t *testing.T) {
	for _, tc := range []struct {
		name, probe string
		want        float64
	}{
		{"rotated portrait", `{"streams":[{"codec_type":"video","width":1920,"height":1080,"side_data_list":[{"rotation":-90}]}],"format":{"duration":"5"}}`, 0.5625},
		{"anamorphic", `{"streams":[{"codec_type":"video","width":1440,"height":1080,"sample_aspect_ratio":"4:3"}],"format":{"duration":"5"}}`, 1.7778},
		{"declared display ratio", `{"streams":[{"codec_type":"video","width":720,"height":576,"display_aspect_ratio":"16:9"}],"format":{"duration":"5"}}`, 1.7778},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := videoTool(t, &fakeMedia{probe: tc.probe})
			out, err := (videoCheck{media: r}).Execute(context.Background(), json.RawMessage(`{"path":"clip.mp4","aspect_ratio":`+jsonFloat(tc.want)+`}`))
			if err != nil {
				t.Fatal(err)
			}
			var report videoCheckReport
			if err := json.Unmarshal([]byte(out), &report); err != nil || !report.TechnicalPass || report.Observed.DisplayAspect != tc.want {
				t.Fatalf("%s: %v", out, err)
			}
		})
	}
}

func jsonFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func TestCheckVideoReportsReplacedBytes(t *testing.T) {
	r, _ := videoTool(t, &fakeMedia{probe: goodProbe})
	out, err := (videoCheck{media: r}).Execute(context.Background(), json.RawMessage(`{"path":"clip.mp4","expected_sha256":"00"}`))
	if err != nil {
		t.Fatal(err)
	}
	var report videoCheckReport
	if err := json.Unmarshal([]byte(out), &report); err != nil || report.TechnicalPass || !strings.Contains(strings.Join(report.Failures, ","), "sha256") {
		t.Fatalf("%s: %v", out, err)
	}
}

func TestCheckVideoRealFFmpegFailsATruncatedTail(t *testing.T) {
	requireFFmpeg(t)
	clip := makeClip(t, "good.mkv", "matroska", "320x240", "2")
	dir := filepath.Dir(clip)
	data, err := os.ReadFile(clip)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cut.mkv"), data[:len(data)*2/3], 0o600); err != nil {
		t.Fatal(err)
	}
	check := videoCheck{media: readVideo{workDir: dir}}
	for _, tc := range []struct {
		path string
		pass bool
	}{{"good.mkv", true}, {"cut.mkv", false}} {
		out, err := check.Execute(context.Background(), json.RawMessage(`{"path":"`+tc.path+`","width":320,"height":240,"audio":"forbidden"}`))
		if err != nil {
			t.Fatal(err)
		}
		var report videoCheckReport
		if err := json.Unmarshal([]byte(out), &report); err != nil || report.TechnicalPass != tc.pass {
			t.Fatalf("%s: %s %v", tc.path, out, err)
		}
	}
}

func TestCheckVideoDecodedTimeShortOfTheDeclaredDurationIsTruncated(t *testing.T) {
	for _, tc := range []struct {
		progress string
		pass     bool
	}{
		{"frame=10\nout_time_us=80000000\nprogress=end\n", true},
		{"frame=10\nout_time_us=40000000\nprogress=continue\nout_time_us=52000000\nprogress=end\n", false},
	} {
		m := &fakeMedia{probe: goodProbe, frame: func(int) ([]byte, string, error) { return []byte(tc.progress), "", nil }}
		r, _ := videoTool(t, m)
		out, err := (videoCheck{media: r}).Execute(context.Background(), json.RawMessage(`{"path":"clip.mp4","duration":80}`))
		if err != nil {
			t.Fatal(err)
		}
		var report videoCheckReport
		if err := json.Unmarshal([]byte(out), &report); err != nil || report.TechnicalPass != tc.pass {
			t.Fatalf("%s: %v", out, err)
		}
		if !tc.pass && (!strings.Contains(strings.Join(report.Failures, ","), "truncated") || report.Observed.Decoded != 52) {
			t.Fatalf("truncation not reported with the decoded time: %s", out)
		}
	}
}
