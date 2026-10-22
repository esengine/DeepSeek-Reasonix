package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type checkProbeStream struct {
	Type   string `json:"codec_type"`
	Codec  string `json:"codec_name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	SAR    string `json:"sample_aspect_ratio"`
	DAR    string `json:"display_aspect_ratio"`
	Tags   struct {
		Rotate string `json:"rotate"`
	} `json:"tags"`
	SideData []struct {
		Rotation *float64 `json:"rotation"`
	} `json:"side_data_list"`
}

func (c videoCheck) probe(ctx context.Context, ffprobe, demuxer, snap, dir string) (videoObserved, error) {
	argv := append([]string{ffprobe, "-v", "error", "-protocol_whitelist", "file"}, demuxArgs(demuxer)...)
	argv = append(argv, "-print_format", "json", "-show_entries",
		"format=duration:stream=codec_type,codec_name,width,height,sample_aspect_ratio,display_aspect_ratio:stream_tags=rotate:stream_side_data=rotation",
		"-i", snap)
	ctx, cancel := context.WithTimeout(ctx, readVideoProbeTimeout)
	defer cancel()
	out, tail, err := c.media.execIn(ctx, argv, readVideoMaxProbeBytes, dir)
	if err != nil {
		return videoObserved{}, fmt.Errorf("%s", probeMessage(err, tail))
	}
	var doc struct {
		Streams []checkProbeStream `json:"streams"`
		Format  struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return videoObserved{}, fmt.Errorf("ffprobe did not answer in the form check_video asks for")
	}
	var o videoObserved
	if d, err := strconv.ParseFloat(doc.Format.Duration, 64); err == nil && d > 0 && !math.IsInf(d, 0) {
		o.Duration = d
	}
	video := false
	for _, s := range doc.Streams {
		switch {
		case s.Type == "video" && !video:
			video = true
			o.Width, o.Height, o.VideoCodec = s.Width, s.Height, s.Codec
			o.Rotation = streamRotation(s)
			o.DisplayAspect = displayAspect(s, o.Rotation)
		case s.Type == "audio" && !o.HasAudio:
			o.HasAudio, o.AudioCodec = true, s.Codec
		}
	}
	return o, nil
}

func streamRotation(s checkProbeStream) int {
	for _, d := range s.SideData {
		if d.Rotation != nil {
			return normalizeRotation(int(math.Round(*d.Rotation)))
		}
	}
	if r, err := strconv.Atoi(strings.TrimSpace(s.Tags.Rotate)); err == nil {
		return normalizeRotation(r)
	}
	return 0
}

func normalizeRotation(r int) int { return ((r % 360) + 360) % 360 }

// displayAspect is the shape a player shows: the stored size corrected by the
// pixel aspect ratio, turned when the stream is rotated a quarter turn.
func displayAspect(s checkProbeStream, rotation int) float64 {
	if s.Width <= 0 || s.Height <= 0 {
		return 0
	}
	aspect := float64(s.Width) / float64(s.Height)
	if dar, ok := parseRatio(s.DAR); ok {
		aspect = dar
	} else if sar, ok := parseRatio(s.SAR); ok {
		aspect *= sar
	}
	if rotation == 90 || rotation == 270 {
		aspect = 1 / aspect
	}
	return math.Round(aspect*1e4) / 1e4
}

func parseRatio(v string) (float64, bool) {
	num, den, ok := strings.Cut(v, ":")
	if !ok {
		return 0, false
	}
	n, err1 := strconv.ParseFloat(num, 64)
	d, err2 := strconv.ParseFloat(den, 64)
	if err1 != nil || err2 != nil || n <= 0 || d <= 0 {
		return 0, false
	}
	return n / d, true
}

// decodedSeconds reads the last out_time_us ffmpeg's -progress stream reported.
func decodedSeconds(progress []byte) (float64, bool) {
	seconds, found := 0.0, false
	for line := range strings.SplitSeq(string(progress), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "out_time_us="); ok {
			if us, err := strconv.ParseInt(v, 10, 64); err == nil && us >= 0 {
				seconds, found = float64(us)/1e6, true
			}
		}
	}
	return seconds, found
}
