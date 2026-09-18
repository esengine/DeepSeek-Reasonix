// Package sessionexport renders a fixed authoritative display snapshot. It is
// shared by Desktop and Serve and never reads a provider workset or UI window.
package sessionexport

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"reasonix/internal/agent"
	"reasonix/internal/attachment"
	"reasonix/internal/provider"
	"reasonix/internal/session"
	"reasonix/internal/transcript"
)

type Item map[string]any

type Document struct {
	Snapshot  session.ExportSnapshot `json:"snapshot"`
	Records   int                    `json:"records"`
	Directory string                 `json:"-"`
}

type Block struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Label string `json:"label,omitempty"`
}

func text(item Item, key string) string { value, _ := item[key].(string); return value }
func resultPath(dir, id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(dir, "result-"+hex.EncodeToString(sum[:]))
}

// Build stages files privately. Only a successful caller may publish them.
// Bodies are retained for one record at a time; cross-page tool matching uses
// disk rather than an unbounded map of tool output strings.
func Build(ctx context.Context, q *session.Query, snapshot session.ExportSnapshot, dir string, progress func(int)) (*Document, error) {
	return BuildForRef(ctx, q, snapshot.Ref, snapshot, dir, progress)
}

// BuildForRef keeps an authenticated session identity separate from snapshot
// metadata supplied by a remote export client.
func BuildForRef(ctx context.Context, q *session.Query, ref session.SessionRef, snapshot session.ExportSnapshot, dir string, progress func(int)) (*Document, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := stageToolResults(ctx, q, ref, snapshot, dir); err != nil {
		return nil, err
	}
	items, err := os.CreateTemp(dir, "items-")
	if err != nil {
		return nil, err
	}
	defer items.Close()
	markdown, err := os.OpenFile(filepath.Join(dir, "markdown"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer markdown.Close()
	blocks, err := os.OpenFile(filepath.Join(dir, "blocks"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer blocks.Close()
	md := bufio.NewWriter(markdown)
	enc := json.NewEncoder(items)
	blockEncoder := json.NewEncoder(blocks)
	doc := &Document{Snapshot: snapshot, Directory: dir}
	title := strings.NewReplacer("\r", " ", "\n", " ").Replace(snapshot.Title)
	if title == "" {
		title = "Reasonix session"
	}
	heading := fmt.Sprintf("# %s\n\nSnapshot: %s · sequence %d\n\n", title, snapshot.CapturedAt.Format("2006-01-02T15:04:05Z07:00"), snapshot.SnapshotSequence)
	if _, err = io.WriteString(md, heading); err != nil {
		return nil, err
	}
	if err = blockEncoder.Encode(Block{Kind: "markdown", Text: heading}); err != nil {
		return nil, err
	}
	attribution := map[string]int{"sharedHost": 0, "diskCache": 0, "remote": 0, "networkCalls": 0}
	w := &documentWriter{ctx: ctx, query: q, sessionRef: ref, items: enc, md: md, blocks: blockEncoder, doc: doc, attribution: attribution, progress: progress}
	err = q.VisitExportMessagesForRef(ctx, ref, snapshot, func(record session.PersistentMessage) error { return w.writeRecord(record, dir) })
	if err != nil {
		return nil, err
	}
	if err = md.Flush(); err != nil {
		return nil, err
	}
	if err = writeJSONDocument(items, snapshot, dir, attribution); err != nil {
		return nil, err
	}
	for _, file := range []*os.File{markdown, blocks} {
		if err := errors.Join(file.Sync(), file.Close()); err != nil {
			return nil, err
		}
	}
	return doc, nil
}

type documentWriter struct {
	ctx         context.Context
	query       *session.Query
	sessionRef  session.SessionRef
	items       *json.Encoder
	md          io.Writer
	blocks      *json.Encoder
	doc         *Document
	attribution map[string]int
	progress    func(int)
}

func stageToolResults(ctx context.Context, q *session.Query, ref session.SessionRef, snapshot session.ExportSnapshot, dir string) error {
	err := q.VisitExportMessagesForRef(ctx, ref, snapshot, func(record session.PersistentMessage) error {
		var m provider.Message
		if err := json.Unmarshal(record.Inline, &m); err != nil {
			return err
		}
		for _, row := range transcript.History([]provider.Message{m}, transcript.HistoryOptions{}) {
			for _, call := range row.ToolCalls {
				if call.ID != "" {
					if err := os.WriteFile(resultPath(dir, call.ID)+".claimed", nil, 0600); err != nil {
						return err
					}
				}
			}
		}
		if m.Role == provider.RoleTool && m.ToolCallID != "" {
			return os.WriteFile(resultPath(dir, m.ToolCallID), record.Inline, 0600)
		}
		return nil
	})
	return err
}
func (w *documentWriter) write(item Item) error {
	if err := w.items.Encode(item); err != nil {
		return err
	}
	if err := WriteItemMarkdown(w.md, item); err != nil {
		return err
	}
	if err := writeBlocks(w.blocks, item); err != nil {
		return err
	}
	if text(item, "kind") == "notice" && (text(item, "code") == "mcp_tools_list" || strings.EqualFold(strings.TrimSpace(text(item, "text")), "mcp tools/list")) {
		var detail struct {
			Source  string `json:"source"`
			Network bool   `json:"network_call"`
		}
		if json.Unmarshal([]byte(text(item, "detail")), &detail) == nil {
			switch detail.Source {
			case "shared_host":
				w.attribution["sharedHost"]++
			case "disk_cache":
				w.attribution["diskCache"]++
			case "remote":
				w.attribution["remote"]++
			}
			if detail.Network {
				w.attribution["networkCalls"]++
			}
		}
	}
	w.doc.Records++
	if w.progress != nil {
		w.progress(w.doc.Records)
	}
	return nil
}
func (w *documentWriter) writeRecord(record session.PersistentMessage, dir string) error {
	var m provider.Message
	if err := json.Unmarshal(record.Inline, &m); err != nil {
		return err
	}
	rows := transcript.History([]provider.Message{m}, transcript.HistoryOptions{})
	if record.Role == "notice" {
		var row transcript.Message
		if err := json.Unmarshal(record.Inline, &row); err != nil {
			return err
		}
		row.MessageID = record.MessageID
		row.RecordID = "m:" + record.MessageID
		rows = []transcript.Message{row}
	}
	for _, row := range rows {
		if err := w.writeRow(record, m, row, dir); err != nil {
			return err
		}
	}
	return nil
}
func (w *documentWriter) writeRow(record session.PersistentMessage, m provider.Message, row transcript.Message, dir string) error {
	encodedRow, err := json.Marshal(row)
	if err != nil {
		return err
	}
	item := Item{}
	if err = json.Unmarshal(encodedRow, &item); err != nil {
		return err
	}
	delete(item, "role")
	delete(item, "content")
	delete(item, "toolCalls")
	item["id"], item["kind"], item["text"] = row.RecordID, row.Role, row.Content
	item["messageId"], item["submissionId"] = record.MessageID, record.SubmissionID
	switch row.Role {
	case "user":
		item["text"] = agent.UserMessageText(m)
		images, err := w.exportImages(m, dir)
		if err != nil {
			return err
		}
		if len(images) > 0 {
			item["images"] = images
		}
	case "assistant":
		return w.writeAssistant(record, row, item, dir)
	case "tool":
		if m.ToolCallID != "" {
			if _, err := os.Stat(resultPath(dir, m.ToolCallID) + ".claimed"); err == nil {
				return nil
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		item = Item{"kind": "tool", "id": row.RecordID, "name": m.Name, "args": "", "readOnly": false}
		applyResult(item, m)
	case "notice":
		item["code"] = row.Code
		item["level"] = row.Level
		item["detail"] = row.Detail
	}
	if err := w.write(item); err != nil {
		return err
	}
	return nil
}

func (w *documentWriter) exportImages(m provider.Message, dir string) ([]string, error) {
	images := append([]string(nil), m.Images...)
	for _, input := range m.ImageInputs {
		if err := w.ctx.Err(); err != nil {
			return nil, err
		}
		switch input.Kind {
		case attachment.KindURL:
			if err := input.Validate(); err != nil {
				return nil, err
			}
			images = append(images, input.URL)
		case attachment.KindAttachment:
			if err := input.Validate(); err != nil {
				return nil, err
			}
			dataURL, err := w.stageAttachment(input.Attachment, dir)
			if err != nil {
				return nil, err
			}
			images = append(images, dataURL)
		case attachment.KindFiles:
			if err := input.Validate(); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("session export: provider file image %q has no portable original", input.FilesID)
		default:
			return nil, fmt.Errorf("session export: unsupported image input kind %q", input.Kind)
		}
	}
	return images, nil
}

func (w *documentWriter) stageAttachment(ref *attachment.AttachmentRef, dir string) (string, error) {
	if ref == nil {
		return "", errors.New("session export: missing attachment reference")
	}
	attachmentsDir := filepath.Join(dir, "attachments")
	if err := os.MkdirAll(attachmentsDir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(attachmentsDir, ref.Content.Digest)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}
		if int64(len(body)) != ref.Content.Bytes {
			return "", errors.New("session export: staged attachment size changed")
		}
		return attachment.DataURL(ref.MIME(), body), nil
	}
	if err != nil {
		return "", err
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(path)
		}
	}()
	var body bytes.Buffer
	for offset := int64(0); offset < ref.Content.Bytes; {
		chunk, total, readErr := w.query.ReadSessionAttachment(w.ctx, w.sessionRef, ref.Content.Digest, offset, min(int64(1<<20), ref.Content.Bytes-offset))
		if readErr != nil {
			return "", fmt.Errorf("session export: read attachment %q: %w", attachment.NormalizeDisplayName(ref.DisplayName), readErr)
		}
		if total != ref.Content.Bytes || len(chunk) == 0 {
			return "", errors.New("session export: attachment size changed")
		}
		if _, err = file.Write(chunk); err != nil {
			return "", err
		}
		if _, err = body.Write(chunk); err != nil {
			return "", err
		}
		offset += int64(len(chunk))
	}
	if err = errors.Join(file.Sync(), file.Close()); err != nil {
		return "", err
	}
	success = true
	return attachment.DataURL(ref.MIME(), body.Bytes()), nil
}
func (w *documentWriter) writeAssistant(record session.PersistentMessage, row transcript.Message, item Item, dir string) error {
	if err := w.writeSearch(row, item); err != nil {
		return err
	}
	item["reasoning"] = row.Reasoning
	item["streaming"] = false
	item["turnFinal"] = record.TurnFinal
	item["turnDurationMs"] = record.TurnDurationMs
	if record.SamplingCount != nil {
		item["samplingCount"] = *record.SamplingCount
	}
	if record.ToolCount != nil {
		item["toolCount"] = *record.ToolCount
	}
	item["workDurationMs"] = row.WorkDurationMs
	item["createdAt"] = row.CreatedAt
	if len(row.MemoryCitations) > 0 {
		item["memoryCitations"] = row.MemoryCitations
	}
	if row.Content != "" || row.Reasoning != "" {
		if err := w.write(item); err != nil {
			return err
		}
	}
	return w.writeCalls(record, row, dir)
}
func (w *documentWriter) writeSearch(row transcript.Message, item Item) error {
	for _, search := range row.ServerSearch {
		if search.ID == "" {
			continue
		}
		args, err := json.Marshal(map[string]string{"query": search.Query})
		if err != nil {
			return err
		}
		sources := make([]map[string]string, 0, len(search.Results))
		for _, hit := range search.Results {
			sources = append(sources, map[string]string{"title": hit.Title, "url": hit.URL})
		}
		if err := w.write(Item{"kind": "tool", "id": search.ID, "name": "web_search", "args": string(args), "readOnly": true, "status": "done", "searchSourcesStatus": provider.ServerSearchSourcesStatus(search), "searchSources": sources, "output": provider.ServerSearchDisplayOutput(search), "contentState": "ready"}); err != nil {
			return err
		}
		item["searchSources"] = sources
	}
	return nil
}
func (w *documentWriter) writeCalls(record session.PersistentMessage, row transcript.Message, dir string) error {
	for index, call := range row.ToolCalls {
		tool := Item{"kind": "tool", "id": call.ID, "name": call.Name, "args": call.Arguments, "readOnly": false, "status": "unknown", "resultMissing": true, "contentState": "unloaded", "fileDiff": call.Diff, "added": call.Added, "removed": call.Removed}
		if observation, ok := record.ToolObservations[call.ID]; ok {
			tool["status"] = exportToolState(observation.State)
		}
		if call.ID == "" {
			tool["id"] = fmt.Sprintf("%s:call:%d", record.MessageID, index)
		}
		if call.ResolvedReadOnly != nil {
			tool["readOnly"] = *call.ResolvedReadOnly
		}
		if call.ResolvedName != "" {
			tool["resolvedName"] = call.ResolvedName
		}
		if call.CapabilityID != "" {
			tool["capabilityId"] = call.CapabilityID
		}
		if call.ID != "" {
			body, err := os.ReadFile(resultPath(dir, call.ID))
			if err == nil {
				var result provider.Message
				if err = json.Unmarshal(body, &result); err != nil {
					return err
				}
				applyResult(tool, result)
			} else if !os.IsNotExist(err) {
				return err
			}

		}
		if err := w.write(tool); err != nil {
			return err
		}
	}
	return nil
}
func writeJSONDocument(items *os.File, snapshot session.ExportSnapshot, dir string, attribution map[string]int) error {
	var err error
	if _, err = items.Seek(0, io.SeekStart); err != nil {
		return err
	}
	output, err := os.OpenFile(filepath.Join(dir, "json"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	header := map[string]any{"title": snapshot.Title, "exportedAt": snapshot.CapturedAt, "mcpList": attribution, "exportMetadata": map[string]any{"schemaVersion": 1, "snapshot": snapshot, "complete": true, "mcpAttributionScope": "persisted display notices; older unrecorded observations are unavailable"}}
	data, err := json.Marshal(header)
	if err != nil {
		return err
	}
	if _, err = output.Write(data[:len(data)-1]); err != nil {
		return err
	}
	if _, err = io.WriteString(output, ",\"items\":["); err != nil {
		return err
	}
	decoder := json.NewDecoder(items)
	first := true
	for {
		var item json.RawMessage
		err = decoder.Decode(&item)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if !first {
			if _, err = io.WriteString(output, ","); err != nil {
				return err
			}
		}
		if _, err = output.Write(item); err != nil {
			return err
		}
		first = false
	}
	if _, err = io.WriteString(output, "]}\n"); err != nil {
		return err
	}
	return errors.Join(output.Sync(), output.Close())
}

func applyResult(item Item, m provider.Message) {
	output := m.Content
	if m.RawContent != "" {
		output = m.RawContent
	}
	item["output"] = output
	item["resultMissing"] = false
	item["contentState"] = "ready"
	item["status"] = "done"
	switch provider.ToolResultRunState(m) {
	case provider.ToolRunFailed:
		item["status"] = "error"
		item["error"] = output
	case provider.ToolRunCancelled, provider.ToolRunNotStarted:
		item["status"] = "stopped"
	case provider.ToolRunUnknown:
		item["status"] = "unknown"
	case provider.ToolRunPending, provider.ToolRunStarted, provider.ToolRunRunning:
		item["status"] = "running"
	}
	if m.ToolExecution != nil {
		item["execution"] = m.ToolExecution
	}
	if m.MCPApp != nil {
		item["mcpApp"] = m.MCPApp
	}
	if m.PresentedFiles != nil {
		item["presentedFiles"] = m.PresentedFiles.Files
	}
}

func Fence(body string) string {
	longest, run := 2, 0
	for _, r := range body {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", longest+1)
}
func WriteItemMarkdown(dst io.Writer, item Item) error {
	for _, block := range itemBlocks(item) {
		if block.Kind == "code" {
			fence := Fence(block.Text)
			if _, err := fmt.Fprintf(dst, "%s\n%s\n%s\n%s\n\n", block.Label, fence, block.Text, fence); err != nil {
				return err
			}
		} else if _, err := io.WriteString(dst, block.Text+"\n\n"); err != nil {
			return err
		}
	}
	return nil
}
func itemBlocks(item Item) []Block {
	var blocks []Block
	add := func(kind, label, body string) { blocks = append(blocks, Block{Kind: kind, Label: label, Text: body}) }
	switch text(item, "kind") {
	case "assistant":
		add("markdown", "", "## Assistant")
		if value := text(item, "reasoning"); value != "" {
			add("markdown", "", "### Reasoning\n\n"+value)
		}
		if value := text(item, "text"); value != "" {
			add("markdown", "", value)
		}
	case "tool":
		add("markdown", "", "### Tool: "+text(item, "name")+"\n\nStatus: "+text(item, "status"))
		add("code", "Args", text(item, "args"))
		if value, ok := item["output"]; ok {
			if value == "" {
				add("markdown", "", "Output: 无输出 / No output")
			} else {
				add("code", "Output", text(item, "output"))
			}
		}
		if value := text(item, "error"); value != "" {
			add("code", "Error", value)
		}
	default:
		add("markdown", "", "## "+text(item, "kind")+"\n\n"+text(item, "text"))
		if value := text(item, "detail"); value != "" {
			add("code", "Details", value)
		}
		for _, field := range []string{"decisionReceipt", "readPause", "readCompletion", "readiness", "diagnostic"} {
			if value, ok := item[field]; ok {
				if encoded, err := json.MarshalIndent(value, "", "  "); err == nil {
					add("code", field, string(encoded))
				}
			}
		}
		if images, ok := item["images"].([]string); ok {
			for _, image := range images {
				add("markdown", "", "![attachment]("+image+")")
			}
		}
	}
	return blocks
}
func writeBlocks(enc *json.Encoder, item Item) error {
	for _, block := range itemBlocks(item) {
		// Code bodies can be arbitrarily large. Split them at UTF-8/line boundaries;
		// all bytes remain in the readable exports and in the visual continuation.
		for len(block.Text) > 32<<10 && block.Kind == "code" {
			cut := 32 << 10
			if n := strings.LastIndexByte(block.Text[:cut], '\n'); n > 0 {
				cut = n + 1
			}
			for !utf8.RuneStart(block.Text[cut]) {
				cut--
			}
			part := block
			part.Text = block.Text[:cut]
			if err := enc.Encode(part); err != nil {
				return err
			}
			block.Text = block.Text[cut:]
		}
		if err := enc.Encode(block); err != nil {
			return err
		}
	}
	return nil
}

func exportToolState(state string) string {
	switch state {
	case "completed", "user_confirmed":
		return "done"
	case "failed":
		return "error"
	case "cancelled", "not_started":
		return "stopped"
	case "pending", "started", "running":
		return "running"
	default:
		return "unknown"
	}
}
