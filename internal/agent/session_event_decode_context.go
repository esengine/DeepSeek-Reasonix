package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

func preflightSessionEventMessages(ctx context.Context, path string, raw []byte, existingMessages, existingCollectionItems int, limits sessionReplayLimits) (messageCount, collectionItems int, err error) {
	dec := json.NewDecoder(&contextReader{ctx: ctx, reader: bytes.NewReader(raw)})
	tok, err := dec.Token()
	if err != nil {
		return 0, existingCollectionItems, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return 0, existingCollectionItems, fmt.Errorf("messages must be an array")
	}
	collectionItems = existingCollectionItems
	for dec.More() {
		if err := ctx.Err(); err != nil {
			return 0, existingCollectionItems, err
		}
		if existingMessages+messageCount >= limits.maxMessages {
			return 0, existingCollectionItems, sessionReplayLimitError(
				path, "messages", int64(existingMessages+messageCount+1), int64(limits.maxMessages),
			)
		}
		messageCount++
		if err := preflightSessionEventValue(ctx, path, dec, &collectionItems, limits.maxCollectionItems); err != nil {
			return 0, existingCollectionItems, err
		}
	}
	if _, err := dec.Token(); err != nil {
		return 0, existingCollectionItems, err
	}
	return messageCount, collectionItems, nil
}

// preflightSessionEventValue walks JSON without materializing nested values.
func preflightSessionEventValue(ctx context.Context, path string, dec *json.Decoder, collectionItems *int, maxCollectionItems int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			if _, ok := key.(string); !ok {
				return fmt.Errorf("object key must be a string")
			}
			if err := preflightSessionEventValue(ctx, path, dec, collectionItems, maxCollectionItems); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("object is not terminated")
		}
		return nil
	case '[':
		for dec.More() {
			if err := ctx.Err(); err != nil {
				return err
			}
			if *collectionItems >= maxCollectionItems {
				return sessionReplayLimitError(path, "message_collection_items", int64(*collectionItems+1), int64(maxCollectionItems))
			}
			(*collectionItems)++
			if err := preflightSessionEventValue(ctx, path, dec, collectionItems, maxCollectionItems); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("array is not terminated")
		}
		return nil
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
