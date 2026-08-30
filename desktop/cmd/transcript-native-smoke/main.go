//go:build reasonix_transcript_smoke

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
)

type smokeMessage struct {
	Type                      string  `json:"type"`
	Message                   string  `json:"message,omitempty"`
	Passed                    bool    `json:"passed,omitempty"`
	Rows                      int     `json:"rows,omitempty"`
	Frames                    int     `json:"frames,omitempty"`
	FirstTop                  float64 `json:"firstTop,omitempty"`
	LastTop                   float64 `json:"lastTop,omitempty"`
	InitialDistance           float64 `json:"initialDistance,omitempty"`
	MaxReverse                float64 `json:"maxReverse,omitempty"`
	Occupied                  bool    `json:"occupied,omitempty"`
	Distance                  float64 `json:"distance,omitempty"`
	Mode                      string  `json:"mode,omitempty"`
	GrowthTicks               int     `json:"growthTicks,omitempty"`
	InitialScrollHeight       float64 `json:"initialScrollHeight,omitempty"`
	MinScrollHeight           float64 `json:"minScrollHeight,omitempty"`
	MaxScrollHeight           float64 `json:"maxScrollHeight,omitempty"`
	FinalScrollHeight         float64 `json:"finalScrollHeight,omitempty"`
	InitialScrollTop          float64 `json:"initialScrollTop,omitempty"`
	FinalScrollTop            float64 `json:"finalScrollTop,omitempty"`
	BlankFrames               int     `json:"blankFrames,omitempty"`
	TotalFrames               int     `json:"totalFrames,omitempty"`
	MountedCoverage           float64 `json:"mountedCoverage,omitempty"`
	FinalBottomDistance       float64 `json:"finalBottomDistance,omitempty"`
	FinalMode                 string  `json:"finalMode,omitempty"`
	DeliveredNativeEvents     int     `json:"deliveredNativeEvents,omitempty"`
	ComposerEnabled           bool    `json:"composerEnabled,omitempty"`
	ComposerPassed            bool    `json:"composerPassed,omitempty"`
	ComposerSamples           int     `json:"composerSamples,omitempty"`
	ComposerMaxReverse        float64 `json:"composerMaxReverse,omitempty"`
	ComposerGeometryChanges   int     `json:"composerGeometryChanges,omitempty"`
	ComposerFinalDistance     float64 `json:"composerFinalDistance,omitempty"`
	ComposerInputHeight       float64 `json:"composerInputHeight,omitempty"`
	ComposerFinalValueMatches bool    `json:"composerFinalValueMatches,omitempty"`
	Point                     struct {
		X int `json:"x"`
		Y int `json:"y"`
	} `json:"point,omitempty"`
}

var smokeResultPath string

func init() {
	// Cocoa, GTK, and WebView2 COM all bind their event loop to the initial OS
	// thread. Lock during package initialization, before the Go scheduler can
	// move main onto a worker thread.
	runtime.LockOSThread()
}

func main() {
	url := flag.String("url", "http://127.0.0.1:4173/?mock=bench&bench=1", "built Reasonix bench URL")
	scriptPath := flag.String("script", "transcript_native_smoke_contract.js", "shared JavaScript assertion contract")
	resultPath := flag.String("result-file", "", "optional result file for app-bundle launchers")
	flag.Parse()
	if runtime.GOOS == "darwin" &&
		os.Getenv("REASONIX_NATIVE_SMOKE_ISOLATED_CI") != "1" &&
		os.Getenv("REASONIX_ALLOW_FOREGROUND_NATIVE_INPUT") != "1" {
		fmt.Println("native transcript smoke unavailable: set REASONIX_ALLOW_FOREGROUND_NATIVE_INPUT=1 for the bounded 2s local micro-test; full native input runs only with REASONIX_NATIVE_SMOKE_ISOLATED_CI=1")
		return
	}
	smokeResultPath = *resultPath
	script, err := os.ReadFile(*scriptPath)
	if err != nil {
		fatalf("read assertion contract: %v", err)
	}
	raw, err := runTranscriptNativeSmoke(*url, string(script))
	if err != nil {
		fatalf("run native host: %v", err)
	}
	var message smokeMessage
	if err := json.Unmarshal([]byte(raw), &message); err != nil {
		fatalf("decode native result %q: %v", raw, err)
	}
	if message.Type == "error" {
		fatalf("fixture error: %s", message.Message)
	}
	if message.Type != "result" || !message.Passed {
		fmt.Fprintf(
			os.Stderr,
			"native transcript smoke: failed: type=%s rows=%d frames=%d top=%.1f->%.1f start-distance=%.1f reverse=%.1f occupied=%t distance=%.1f mode=%s growth=%d result=%s\n",
			message.Type, message.Rows, message.Frames, message.FirstTop, message.LastTop,
			message.InitialDistance, message.MaxReverse, message.Occupied, message.Distance, message.Mode, message.GrowthTicks, raw,
		)
		writeSmokeResult([]byte(raw))
		os.Exit(1)
	}
	fmt.Printf(
		"native transcript smoke passed: rows=%d frames=%d start-distance=%.1f reverse=%.1f distance=%.1f mode=%s growth=%d\n",
		message.Rows, message.Frames, message.InitialDistance, message.MaxReverse, message.Distance, message.Mode, message.GrowthTicks,
	)
	if message.ComposerEnabled {
		fmt.Printf(
			"native composer smoke passed: samples=%d reverse=%.1f geometry-changes=%d distance=%.1f input-height=%.1f value-restored=%t\n",
			message.ComposerSamples, message.ComposerMaxReverse, message.ComposerGeometryChanges,
			message.ComposerFinalDistance, message.ComposerInputHeight, message.ComposerFinalValueMatches,
		)
	}
	writeSmokeResult([]byte(raw))
}

func fatalf(format string, args ...any) {
	message := fmt.Sprintf("native transcript smoke: "+format, args...)
	fmt.Fprintln(os.Stderr, message)
	payload, _ := json.Marshal(smokeMessage{Type: "error", Message: message})
	writeSmokeResult(payload)
	os.Exit(1)
}

func writeSmokeResult(payload []byte) {
	if smokeResultPath != "" {
		_ = os.WriteFile(smokeResultPath, payload, 0o600)
	}
}
