//go:build windows && reasonix_transcript_smoke

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"

	"reasonix/desktop/internal/transcriptsmoke"
)

type smokeGeometry struct {
	Label          string                 `json:"label"`
	DPR            float64                `json:"dpr"`
	Viewport       smokeViewport          `json:"viewport"`
	Shell          transcriptsmoke.Rect   `json:"shell"`
	Table          transcriptsmoke.Rect   `json:"table"`
	Row            transcriptsmoke.Rect   `json:"row"`
	Target         transcriptsmoke.Rect   `json:"target"`
	Toolbar        *transcriptsmoke.Rect  `json:"toolbar"`
	SelectionRects []transcriptsmoke.Rect `json:"selectionRects"`
	ScrollTop      float64                `json:"scrollTop"`
	ScrollHeight   float64                `json:"scrollHeight"`
	ClientHeight   float64                `json:"clientHeight"`
	HostCount      int                    `json:"hostCount"`
	HostStable     bool                   `json:"hostStable"`
	HostState      string                 `json:"hostState"`
}

type smokeViewport struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type smokePoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type smokeMessage struct {
	Type         string          `json:"type"`
	Message      string          `json:"message,omitempty"`
	Point        smokePoint      `json:"point,omitempty"`
	Geometry     smokeGeometry   `json:"geometry,omitempty"`
	EventSamples []smokeGeometry `json:"eventSamples,omitempty"`
	Platform     string          `json:"platform,omitempty"`
	Iteration    int             `json:"iteration,omitempty"`
}

type smokeFrameResult struct {
	Label        string                        `json:"label"`
	CapturePath  string                        `json:"capturePath"`
	DiffPath     string                        `json:"diffPath,omitempty"`
	Geometry     smokeGeometry                 `json:"geometry"`
	EventSamples []smokeGeometry               `json:"eventSamples"`
	Pixel        transcriptsmoke.CompareResult `json:"pixel"`
	GeometryOK   bool                          `json:"geometryOK"`
}

type smokeIterationResult struct {
	Iteration int                `json:"iteration"`
	Baseline  string             `json:"baseline"`
	Frames    []smokeFrameResult `json:"frames"`
	Passed    bool               `json:"passed"`
}

type smokeResult struct {
	Passed          bool                   `json:"passed"`
	Commit          string                 `json:"commit"`
	WindowsVersion  string                 `json:"windowsVersion"`
	WebView2Runtime string                 `json:"webView2Runtime"`
	Viewport        smokeViewport          `json:"viewport"`
	DPR             float64                `json:"dpr"`
	ClickIntervals  []int                  `json:"clickIntervalsMs"`
	Iterations      []smokeIterationResult `json:"iterations"`
	Error           string                 `json:"error,omitempty"`
}

func init() {
	runtime.LockOSThread()
}

func main() {
	url := flag.String("url", "http://127.0.0.1:4173/?mock=bench&bench=1", "built Reasonix bench URL")
	scriptPath := flag.String("script", "transcript_selection_smoke_contract.js", "selection assertion contract")
	artifacts := flag.String("artifacts", "transcript-selection-smoke-artifacts", "PNG and JSON artifact directory")
	resultPath := flag.String("result-file", "", "optional result JSON path")
	iterations := flag.Int("iterations", 3, "native multi-click iterations")
	width := flag.Int("width", 1200, "WebView2 viewport width")
	height := flag.Int("height", 800, "WebView2 viewport height")
	commit := flag.String("commit", os.Getenv("GITHUB_SHA"), "tested source commit")
	flag.Parse()

	result := smokeResult{Commit: *commit, ClickIntervals: []int{400, 320, 180}}
	script, err := os.ReadFile(*scriptPath)
	if err == nil {
		result, err = runSelectionSmoke(*url, string(script), *artifacts, *iterations, *width, *height, *commit)
	}
	if err != nil {
		result.Passed = false
		result.Error = err.Error()
	}
	payload, marshalErr := json.MarshalIndent(result, "", "  ")
	if marshalErr != nil {
		fmt.Fprintf(os.Stderr, "native selection smoke: encode result: %v\n", marshalErr)
		os.Exit(1)
	}
	if *resultPath != "" {
		if writeErr := os.WriteFile(*resultPath, payload, 0o600); writeErr != nil {
			fmt.Fprintf(os.Stderr, "native selection smoke: write result: %v\n", writeErr)
			os.Exit(1)
		}
		fmt.Printf("native selection smoke: passed=%t iterations=%d result=%s\n", result.Passed, len(result.Iterations), *resultPath)
	} else {
		fmt.Println(string(payload))
	}
	if err != nil || !result.Passed {
		os.Exit(1)
	}
}
