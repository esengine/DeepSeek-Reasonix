package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	scrollDiagnosticSchemaVersion   = 2
	maxScrollDiagnosticEvents       = 4096
	maxScrollDiagnosticPayloadBytes = 2 << 20
)

var scrollDiagnosticReportIDPattern = regexp.MustCompile(`^[a-f0-9]{32,64}$`)
var scrollDiagnosticBuildValuePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
var allowedScrollDiagnosticEventTypes = map[string]bool{
	"start": true, "stop": true, "mark": true, "sample": true, "wheel": true,
	"scroll": true, "scroll-write": true, "items-rendered": true, "list-height": true,
	"row-measure": true, "scroll-state": true, "blank-check": true, "blank-reset": true, "recovery": true,
}

type scrollDiagnosticPayload struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Manifest      scrollDiagnosticManifest `json:"manifest"`
	Summary       scrollDiagnosticSummary  `json:"summary"`
	Events        []scrollDiagnosticEvent  `json:"events"`
}

type scrollDiagnosticManifest struct {
	ReportID         string  `json:"reportId"`
	CreatedAt        string  `json:"createdAt"`
	BuildCommit      string  `json:"buildCommit"`
	BuildChannel     string  `json:"buildChannel"`
	Platform         string  `json:"platform"`
	UserAgent        string  `json:"userAgent"`
	DevicePixelRatio float64 `json:"devicePixelRatio"`
	ViewportWidth    int     `json:"viewportWidth"`
	ViewportHeight   int     `json:"viewportHeight"`
	ReducedMotion    bool    `json:"reducedMotion"`
	TranscriptWidth  float64 `json:"transcriptWidth"`
	ContentWidth     float64 `json:"contentWidth"`
	FontSize         float64 `json:"fontSize"`
	LineHeight       float64 `json:"lineHeight"`
	ProcessFold      string  `json:"processFoldPreference"`
	ReasoningDisplay string  `json:"reasoningDisplayMode"`
}

type scrollDiagnosticSummary struct {
	DurationMS        int `json:"durationMs"`
	EventCount        int `json:"eventCount"`
	DroppedEventCount int `json:"droppedEventCount"`
	MarkerCount       int `json:"markerCount"`
}

type scrollDiagnosticEvent struct {
	T                 int             `json:"t"`
	Type              string          `json:"type"`
	ScrollTop         *float64        `json:"scrollTop,omitempty"`
	ScrollHeight      *float64        `json:"scrollHeight,omitempty"`
	ClientHeight      *float64        `json:"clientHeight,omitempty"`
	BottomDistance    *float64        `json:"bottomDistance,omitempty"`
	MountedRows       *float64        `json:"mountedRows,omitempty"`
	TotalRows         *float64        `json:"totalRows,omitempty"`
	FirstVisibleIndex *float64        `json:"firstVisibleIndex,omitempty"`
	FirstVisibleTop   *float64        `json:"firstVisibleTop,omitempty"`
	DeltaY            *float64        `json:"deltaY,omitempty"`
	TargetTop         *float64        `json:"targetTop,omitempty"`
	TargetIndex       json.RawMessage `json:"targetIndex,omitempty"`
	ListHeight        *float64        `json:"listHeight,omitempty"`
	RowIndex          *float64        `json:"rowIndex,omitempty"`
	EstimatedSize     *float64        `json:"estimatedSize,omitempty"`
	PreviousSize      *float64        `json:"previousSize,omitempty"`
	MeasuredSize      *float64        `json:"measuredSize,omitempty"`
	SizeDelta         *float64        `json:"sizeDelta,omitempty"`
	ContentRevision   *float64        `json:"contentRevision,omitempty"`
	DisclosureCount   *float64        `json:"disclosureCount,omitempty"`
	SettleFrame       *float64        `json:"settleFrame,omitempty"`
	OffBottomFrames   *float64        `json:"offBottomFrames,omitempty"`
	StagnantFrames    *float64        `json:"stagnantFrames,omitempty"`
	Mode              string          `json:"mode,omitempty"`
	PreviousMode      string          `json:"previousMode,omitempty"`
	Owner             string          `json:"owner,omitempty"`
	WriteKind         string          `json:"writeKind,omitempty"`
	Source            string          `json:"source,omitempty"`
	Phase             string          `json:"phase,omitempty"`
	RowKind           string          `json:"rowKind,omitempty"`
	FoldState         string          `json:"foldState,omitempty"`
	State             string          `json:"state,omitempty"`
	Reason            string          `json:"reason,omitempty"`
	AtBottom          *bool           `json:"atBottom,omitempty"`
	Scrollable        *bool           `json:"scrollable,omitempty"`
	Blank             *bool           `json:"blank,omitempty"`
	ReaderIntent      *bool           `json:"readerIntent,omitempty"`
	CanClaimTail      *bool           `json:"canClaimTail,omitempty"`
	Substantial       *bool           `json:"substantial,omitempty"`
	TailCommand       *bool           `json:"tailCommand,omitempty"`
}

func decodeScrollDiagnosticsPayload(payload string) (scrollDiagnosticPayload, error) {
	if len(payload) == 0 || len(payload) > maxScrollDiagnosticPayloadBytes {
		return scrollDiagnosticPayload{}, errors.New("invalid scroll diagnostic payload size")
	}
	var decoded scrollDiagnosticPayload
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return scrollDiagnosticPayload{}, errors.New("invalid scroll diagnostic payload")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return scrollDiagnosticPayload{}, errors.New("invalid scroll diagnostic payload")
	}
	if err := validateScrollDiagnosticsPayload(decoded); err != nil {
		return scrollDiagnosticPayload{}, err
	}
	return decoded, nil
}

func validateScrollDiagnosticsPayload(payload scrollDiagnosticPayload) error {
	if payload.SchemaVersion != scrollDiagnosticSchemaVersion {
		return errors.New("unsupported scroll diagnostic schema")
	}
	if err := validateScrollDiagnosticManifest(payload.Manifest); err != nil {
		return err
	}
	if err := validateScrollDiagnosticSummary(payload.Summary, len(payload.Events)); err != nil {
		return err
	}
	markerCount := 0
	for _, event := range payload.Events {
		if err := validateScrollDiagnosticEvent(event, payload.Summary.DurationMS); err != nil {
			return err
		}
		if event.Type == "mark" {
			markerCount++
		}
	}
	if markerCount != payload.Summary.MarkerCount {
		return errors.New("invalid scroll diagnostic marker count")
	}
	return nil
}

func validateScrollDiagnosticManifest(manifest scrollDiagnosticManifest) error {
	if !scrollDiagnosticReportIDPattern.MatchString(manifest.ReportID) {
		return errors.New("invalid scroll diagnostic report id")
	}
	if _, err := time.Parse(time.RFC3339Nano, manifest.CreatedAt); err != nil {
		return errors.New("invalid scroll diagnostic creation time")
	}
	if !scrollDiagnosticBuildValuePattern.MatchString(manifest.BuildCommit) {
		return errors.New("invalid scroll diagnostic build commit")
	}
	if manifest.BuildChannel != "test" && manifest.BuildChannel != "development" && manifest.BuildChannel != "dev" {
		return errors.New("invalid scroll diagnostic build channel")
	}
	if manifest.Platform != "windows" && manifest.Platform != "macos" && manifest.Platform != "linux" && manifest.Platform != "other" {
		return errors.New("invalid scroll diagnostic platform")
	}
	if len(manifest.UserAgent) == 0 || len(manifest.UserAgent) > 512 || hasUnsafeDiagnosticText(manifest.UserAgent) {
		return errors.New("invalid scroll diagnostic user agent")
	}
	return validateScrollDiagnosticDisplay(manifest)
}

func validateScrollDiagnosticDisplay(manifest scrollDiagnosticManifest) error {
	if !finiteInRange(manifest.DevicePixelRatio, 0.25, 16) || manifest.ViewportWidth < 0 || manifest.ViewportWidth > 32768 || manifest.ViewportHeight < 0 || manifest.ViewportHeight > 32768 ||
		!finiteInRange(manifest.TranscriptWidth, 0, 32768) || !finiteInRange(manifest.ContentWidth, 0, 32768) ||
		!finiteInRange(manifest.FontSize, 0, 512) || !finiteInRange(manifest.LineHeight, 0, 1024) {
		return errors.New("invalid scroll diagnostic display metrics")
	}
	if !oneOf(manifest.ProcessFold, "auto", "expanded") || !oneOf(manifest.ReasoningDisplay, "hidden", "summary", "auto", "expanded", "legacy-collapsed", "pending") {
		return errors.New("invalid scroll diagnostic display preferences")
	}
	return nil
}

func validateScrollDiagnosticSummary(summary scrollDiagnosticSummary, eventCount int) error {
	if eventCount == 0 || eventCount > maxScrollDiagnosticEvents {
		return errors.New("invalid scroll diagnostic event count")
	}
	if summary.EventCount != eventCount || summary.DurationMS < 0 || summary.DurationMS > 95_000 || summary.DroppedEventCount < 0 || summary.DroppedEventCount > 1_000_000 || summary.MarkerCount < 0 || summary.MarkerCount > eventCount {
		return errors.New("invalid scroll diagnostic summary")
	}
	return nil
}

func hasUnsafeDiagnosticText(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func finiteInRange(value, min, max float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= min && value <= max
}

func validateScrollDiagnosticEvent(event scrollDiagnosticEvent, durationMS int) error {
	if !allowedScrollDiagnosticEventTypes[event.Type] || event.T < 0 || event.T > durationMS+1_000 {
		return errors.New("invalid scroll diagnostic event")
	}
	for _, value := range []*float64{
		event.ScrollTop, event.ScrollHeight, event.ClientHeight, event.BottomDistance,
		event.MountedRows, event.TotalRows, event.FirstVisibleIndex, event.FirstVisibleTop,
		event.DeltaY, event.TargetTop, event.ListHeight, event.RowIndex, event.EstimatedSize,
		event.PreviousSize, event.MeasuredSize, event.SizeDelta, event.ContentRevision,
		event.DisclosureCount, event.SettleFrame, event.OffBottomFrames, event.StagnantFrames,
	} {
		if value != nil && !finiteInRange(*value, -1_000_000_000, 1_000_000_000) {
			return errors.New("invalid scroll diagnostic event metric")
		}
	}
	for _, value := range []*float64{
		event.RowIndex, event.ContentRevision, event.DisclosureCount,
		event.SettleFrame, event.OffBottomFrames, event.StagnantFrames,
	} {
		if value != nil && (*value < 0 || *value != math.Trunc(*value)) {
			return errors.New("invalid scroll diagnostic event counter")
		}
	}
	if err := validateScrollDiagnosticTargetIndex(event.TargetIndex); err != nil {
		return err
	}
	return validateScrollDiagnosticEventLabels(event)
}

func validateScrollDiagnosticTargetIndex(value json.RawMessage) error {
	if len(value) == 0 {
		return nil
	}
	var number float64
	if err := json.Unmarshal(value, &number); err == nil {
		if finiteInRange(number, 0, 1_000_000_000) && number == math.Trunc(number) {
			return nil
		}
		return errors.New("invalid scroll diagnostic target index")
	}
	var label string
	if json.Unmarshal(value, &label) != nil || label != "LAST" {
		return errors.New("invalid scroll diagnostic target index")
	}
	return nil
}

func validateScrollDiagnosticEventLabels(event scrollDiagnosticEvent) error {
	if event.Mode != "" && !oneOf(event.Mode, "tail-follow", "manual", "user-resize", "selection", "restoring", "unknown") {
		return errors.New("invalid scroll diagnostic mode")
	}
	if event.PreviousMode != "" && !oneOf(event.PreviousMode, "tail-follow", "manual", "user-resize", "selection", "restoring", "unknown") {
		return errors.New("invalid scroll diagnostic previous mode")
	}
	if event.Owner != "" && !oneOf(event.Owner, "tail-follow", "jump", "rewind", "jump-bottom", "custom-scrollbar", "selection-edge-scroll", "recovery", "other") {
		return errors.New("invalid scroll diagnostic owner")
	}
	if event.WriteKind != "" && !oneOf(event.WriteKind, "scrollTo", "scrollBy", "scrollToIndex") {
		return errors.New("invalid scroll diagnostic write kind")
	}
	if event.Source != "" && !oneOf(event.Source,
		"reset", "user-scroll-intent", "manual-reading", "reader-intent-ended", "scroll-delivered",
		"tail-content-changed", "content-shrank", "layout-height-changed", "viewport-resized",
		"user-resize-begin", "user-resize-end", "selection-begin", "selection-end",
		"programmatic-begin", "programmatic-end", "jump-bottom", "jump-index", "scroll-offset",
		"recovery-begin", "recovery-end", "native-scrollbar-release", "other") {
		return errors.New("invalid scroll diagnostic source")
	}
	if event.Phase != "" && !oneOf(event.Phase, "initial", "settle") {
		return errors.New("invalid scroll diagnostic phase")
	}
	if event.RowKind != "" && !oneOf(event.RowKind,
		"older-history", "user", "process-header", "reasoning", "tool", "tool-batch", "tool-group", "phase",
		"process-notice", "notice", "compaction", "answer", "extension", "turn-actions") {
		return errors.New("invalid scroll diagnostic row kind")
	}
	if event.FoldState != "" && !oneOf(event.FoldState, "none", "open", "closed", "mixed") {
		return errors.New("invalid scroll diagnostic fold state")
	}
	if event.State != "" && !oneOf(event.State, "begin", "suspend", "retry", "done", "cancelled", "expired") {
		return errors.New("invalid scroll diagnostic state")
	}
	if event.Reason != "" && !oneOf(event.Reason, "user-takeover", "surface-switch", "superseded", "viewport-blank", "other") {
		return errors.New("invalid scroll diagnostic reason")
	}
	return nil
}

func oneOf(value string, candidates ...string) bool {
	return slices.Contains(candidates, value)
}

func buildScrollDiagnosticsZip(payload string) ([]byte, error) {
	data, _, err := buildScrollDiagnosticsArchive(payload)
	return data, err
}

func buildScrollDiagnosticsArchive(payload string) ([]byte, string, error) {
	decoded, err := decodeScrollDiagnosticsPayload(payload)
	if err != nil {
		return nil, "", err
	}
	manifest, err := json.MarshalIndent(decoded.Manifest, "", "  ")
	if err != nil {
		return nil, "", errors.New("encode scroll diagnostic manifest")
	}
	summary, err := json.MarshalIndent(decoded.Summary, "", "  ")
	if err != nil {
		return nil, "", errors.New("encode scroll diagnostic summary")
	}
	var eventLines bytes.Buffer
	encoder := json.NewEncoder(&eventLines)
	encoder.SetEscapeHTML(false)
	for _, event := range decoded.Events {
		if err := encoder.Encode(event); err != nil {
			return nil, "", errors.New("encode scroll diagnostic events")
		}
	}
	digest := sha256.Sum256(eventLines.Bytes())
	checksum := fmt.Appendf(nil, "%s  scroll-events.jsonl\n", hex.EncodeToString(digest[:]))

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	entries := []struct {
		name string
		data []byte
	}{
		{name: "manifest.json", data: append(manifest, '\n')},
		{name: "summary.json", data: append(summary, '\n')},
		{name: "scroll-events.jsonl", data: eventLines.Bytes()},
		{name: "sha256.txt", data: checksum},
	}
	for _, entry := range entries {
		writer, createErr := zw.Create(entry.name)
		if createErr != nil {
			_ = zw.Close()
			return nil, "", errors.New("create scroll diagnostic archive")
		}
		if _, writeErr := writer.Write(entry.data); writeErr != nil {
			_ = zw.Close()
			return nil, "", errors.New("write scroll diagnostic archive")
		}
	}
	if err := zw.Close(); err != nil {
		return nil, "", errors.New("finish scroll diagnostic archive")
	}
	return archive.Bytes(), decoded.Manifest.ReportID, nil
}

// ExportScrollDiagnostics validates a local, content-free trace, asks the user
// where to save it, and writes a ZIP. It performs no network requests.
func (a *App) ExportScrollDiagnostics(payload string) (string, error) {
	archive, reportID, err := buildScrollDiagnosticsArchive(payload)
	if err != nil {
		return "", err
	}
	if a.ctx == nil {
		return "", nil
	}
	defaultFilename := fmt.Sprintf("reasonix-scroll-diagnostics-%s.zip", reportID[:8])
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:                "Export scroll diagnostics",
		DefaultDirectory:     dialogDefaultDirectory(a.activeWorkspaceRoot()),
		DefaultFilename:      safeExportFilename(defaultFilename),
		CanCreateDirectories: true,
		Filters:              exportFileFilters("application/zip", ".zip"),
	})
	if err != nil || path == "" {
		return "", err
	}
	if filepath.Ext(path) == "" {
		path += ".zip"
	}
	if err := a.SaveExportFile(path, string(archive), false); err != nil {
		return "", err
	}
	return path, nil
}
