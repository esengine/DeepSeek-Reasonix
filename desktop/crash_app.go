package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"runtime"
)

// crash_app.go is the crash/feedback reporting surface. Reports are sent only on
// an explicit user click in the frontend crash overlay — never automatically.

var crashEndpoint = "https://crash.reasonix.io/v1/report"

const maxCrashDetailBytes = 16 << 10

var userPathSegment = regexp.MustCompile(`(?i)([A-Z]:\\Users\\|/(?:home|Users)/)[^/\\:\s"']+`)

func scrubUserPaths(s string) string {
	return userPathSegment.ReplaceAllString(s, "${1}_")
}

type crashReport struct {
	Kind    string     `json:"kind"`
	Version string     `json:"version"`
	OS      string     `json:"os"`
	Arch    string     `json:"arch"`
	Message string     `json:"message"`
	Device  deviceInfo `json:"device"`
}

func (a *App) ReportCrash(kind, detail string) error {
	if kind != "crash" && kind != "feedback" {
		return fmt.Errorf("unknown report kind %q", kind)
	}
	if detail == "" {
		return fmt.Errorf("empty report")
	}
	if len(detail) > maxCrashDetailBytes {
		detail = detail[:maxCrashDetailBytes]
	}
	c, err := httpClient()
	if err != nil {
		return err
	}
	return postCrashReport(a.reqCtx(), c, crashEndpoint, crashReport{
		Kind:    kind,
		Version: version,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Message: scrubUserPaths(detail),
		Device:  collectDeviceInfo(),
	})
}

func postCrashReport(ctx context.Context, c *http.Client, endpoint string, r crashReport) error {
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("crash endpoint returned %s", resp.Status)
	}
	return nil
}
