package client

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"reasonix/internal/remote/protocol"
)

// Content reads one strictly bounded contentRef chunk from the current lease.
func (c *Client) Content(ctx context.Context, params protocol.SessionContentParams) (protocol.SessionContentResult, error) {
	conn, err := c.currentConnection()
	if err != nil {
		return protocol.SessionContentResult{}, err
	}
	return c.contentOn(ctx, conn, params)
}

func (c *Client) contentOn(ctx context.Context, conn *connectionState, params protocol.SessionContentParams) (protocol.SessionContentResult, error) {
	value, _, err := c.requestOn(ctx, conn, protocol.MethodSessionContent, params, true)
	if err != nil {
		return protocol.SessionContentResult{}, err
	}
	result, ok := value.(protocol.SessionContentResult)
	if !ok {
		return protocol.SessionContentResult{}, c.protocolViolation(conn, "content", fmt.Errorf("decoded result type %T", value))
	}
	if result.ContentRef != params.ContentRef || result.Offset != params.Offset {
		return protocol.SessionContentResult{}, c.protocolViolation(conn, "content", errors.New("content chunk identity differs from request"))
	}
	return result, nil
}

// FetchContent retrieves every fixed-size chunk, verifies stable metadata,
// exact byte offsets/count, lowercase SHA-256, and UTF-8 before returning the
// replacement value used by the two-phase protocol decoder.
func (c *Client) FetchContent(ctx context.Context, field protocol.ExternalizedField) (protocol.RehydratedExternalizedField, error) {
	conn, err := c.currentConnection()
	if err != nil {
		return protocol.RehydratedExternalizedField{}, err
	}
	return c.fetchContentOn(ctx, conn, field)
}

func (c *Client) fetchContentOn(ctx context.Context, conn *connectionState, field protocol.ExternalizedField) (protocol.RehydratedExternalizedField, error) {
	if field.ContentRef == "" || field.JSONPointer == "" || field.TotalBytes < 0 || field.TotalBytes > protocol.ContentRefObjectBytes {
		return protocol.RehydratedExternalizedField{}, errors.New("invalid contentRef descriptor identity or byte count")
	}
	digestBytes, err := hex.DecodeString(field.SHA256)
	if err != nil || len(digestBytes) != sha256.Size || hex.EncodeToString(digestBytes) != field.SHA256 {
		return protocol.RehydratedExternalizedField{}, errors.New("invalid contentRef descriptor SHA-256")
	}
	if field.Truncated {
		if field.OriginalBytes == nil || *field.OriginalBytes <= field.TotalBytes || field.TruncationReason == "" {
			return protocol.RehydratedExternalizedField{}, errors.New("invalid truncated contentRef descriptor")
		}
	} else if field.OriginalBytes != nil || field.TruncationReason != "" {
		return protocol.RehydratedExternalizedField{}, errors.New("non-truncated contentRef descriptor has truncation metadata")
	}

	data := make([]byte, 0, int(field.TotalBytes))
	offset := int64(0)
	for {
		chunk, err := c.contentOn(ctx, conn, protocol.SessionContentParams{ContentRef: field.ContentRef, Offset: offset})
		if err != nil {
			return protocol.RehydratedExternalizedField{}, err
		}
		if chunk.TotalBytes != field.TotalBytes || chunk.SHA256 != field.SHA256 || chunk.Encoding != protocol.ContentUTF8 {
			return protocol.RehydratedExternalizedField{}, c.protocolViolation(conn, "content", errors.New("content metadata changed between descriptor and chunk"))
		}
		decoded, err := base64.StdEncoding.DecodeString(chunk.DataBase64)
		if err != nil {
			return protocol.RehydratedExternalizedField{}, c.protocolViolation(conn, "content", errors.New("content chunk Base64 is invalid"))
		}
		if len(decoded) > protocol.ContentRefChunkBytes || int64(len(data)+len(decoded)) > field.TotalBytes {
			return protocol.RehydratedExternalizedField{}, c.protocolViolation(conn, "content", errors.New("content chunk exceeds declared bounds"))
		}
		data = append(data, decoded...)
		if chunk.NextOffset == nil {
			break
		}
		if *chunk.NextOffset != int64(len(data)) || *chunk.NextOffset <= offset {
			return protocol.RehydratedExternalizedField{}, c.protocolViolation(conn, "content", errors.New("content nextOffset is not contiguous"))
		}
		offset = *chunk.NextOffset
	}
	if int64(len(data)) != field.TotalBytes {
		return protocol.RehydratedExternalizedField{}, c.protocolViolation(conn, "content", errors.New("content ended before totalBytes"))
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != field.SHA256 {
		return protocol.RehydratedExternalizedField{}, c.protocolViolation(conn, "content", errors.New("content SHA-256 mismatch"))
	}
	if !utf8.Valid(data) {
		return protocol.RehydratedExternalizedField{}, c.protocolViolation(conn, "content", errors.New("content is not valid UTF-8"))
	}
	return protocol.RehydratedExternalizedField{JSONPointer: field.JSONPointer, Value: string(data)}, nil
}

func (c *Client) fetchFields(ctx context.Context, conn *connectionState, fields []protocol.ExternalizedField) ([]protocol.RehydratedExternalizedField, error) {
	replacements := make([]protocol.RehydratedExternalizedField, 0, len(fields))
	for _, field := range fields {
		replacement, err := c.fetchContentOn(ctx, conn, field)
		if err != nil {
			return nil, err
		}
		replacements = append(replacements, replacement)
	}
	return replacements, nil
}

func (c *Client) rehydrateSnapshot(ctx context.Context, conn *connectionState, raw json.RawMessage, wire protocol.SessionSnapshot) (protocol.SessionSnapshot, error) {
	replacements, err := c.fetchFields(ctx, conn, wire.Externalized)
	if err != nil {
		return protocol.SessionSnapshot{}, err
	}
	result, err := protocol.DecodeRehydratedJSON[protocol.SessionSnapshot](raw, replacements)
	if err != nil {
		return protocol.SessionSnapshot{}, c.protocolViolation(conn, "rehydrate snapshot", err)
	}
	return result, nil
}

func (c *Client) rehydrateEvent(ctx context.Context, conn *connectionState, raw json.RawMessage, wire protocol.SessionEvent) (protocol.SessionEvent, error) {
	replacements, err := c.fetchFields(ctx, conn, wire.Externalized)
	if err != nil {
		return protocol.SessionEvent{}, err
	}
	result, err := protocol.DecodeRehydratedJSON[protocol.SessionEvent](raw, replacements)
	if err != nil {
		return protocol.SessionEvent{}, c.protocolViolation(conn, "rehydrate event", err)
	}
	return result, nil
}

// History returns one snapshot-bound page after fully rehydrating all of its
// contentRef values. Pages from different snapshots are never combined here.
func (c *Client) History(ctx context.Context, params protocol.SessionHistoryParams) (protocol.HistoryPage, error) {
	conn, err := c.currentConnection()
	if err != nil {
		return protocol.HistoryPage{}, err
	}
	value, raw, err := c.requestOn(ctx, conn, protocol.MethodSessionHistory, params, true)
	if err != nil {
		return protocol.HistoryPage{}, err
	}
	wire, ok := value.(protocol.HistoryPage)
	if !ok {
		return protocol.HistoryPage{}, c.protocolViolation(conn, "history", fmt.Errorf("decoded result type %T", value))
	}
	if wire.SnapshotID != params.SnapshotID {
		return protocol.HistoryPage{}, c.protocolViolation(conn, "history", errors.New("history page snapshotId differs from request"))
	}
	replacements, err := c.fetchFields(ctx, conn, wire.Externalized)
	if err != nil {
		return protocol.HistoryPage{}, err
	}
	result, err := protocol.DecodeRehydratedJSON[protocol.HistoryPage](raw, replacements)
	if err != nil {
		return protocol.HistoryPage{}, c.protocolViolation(conn, "rehydrate history", err)
	}
	return result, nil
}
