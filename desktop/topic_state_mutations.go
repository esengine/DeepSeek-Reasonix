package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"

	"reasonix/internal/topicstate"
)

func (m *topicStateManager) createTopic(workspaceRoot, topicID, title, source string, createdAt int64) error {
	topicID, title, source = strings.TrimSpace(topicID), strings.TrimSpace(title), strings.TrimSpace(source)
	return m.mutate(workspaceRoot, func(ctx context.Context, store *topicstate.Store) (topicstate.State, error) {
		return store.Update(ctx, topicID, func(record *topicstate.Record) {
			applyTopicTitle(record, title, source)
			record.CreatedAtMS = createdAt
		})
	}, func() error {
		if err := setLegacyTopicTitle(workspaceRoot, topicID, title, source); err != nil {
			return err
		}
		return setLegacyTopicCreatedAt(workspaceRoot, topicID, createdAt)
	})
}

func (m *topicStateManager) applyAutoTitle(workspaceRoot, topicID, title string, value topicAutoTitleMeta) (bool, error) {
	topicID, title = strings.TrimSpace(topicID), strings.TrimSpace(title)
	applied := false
	err := m.mutate(workspaceRoot, func(ctx context.Context, store *topicstate.Store) (topicstate.State, error) {
		return store.Update(ctx, topicID, func(record *topicstate.Record) {
			if record.TitleSource != topicTitleSourceAuto {
				return
			}
			applyTopicTitle(record, title, topicTitleSourceAuto)
			record.AutoMeta = mergeKnownAutoMeta(record.AutoMeta, value)
			applied = true
		})
	}, func() error {
		sources, err := loadLegacyStringMap(topicTitleSourcesPath(workspaceRoot))
		if err != nil {
			return err
		}
		if sources[topicID] != topicTitleSourceAuto {
			return nil
		}
		if err := setLegacyTopicTitle(workspaceRoot, topicID, title, topicTitleSourceAuto); err != nil {
			return err
		}
		if err := setLegacyTopicAutoMeta(workspaceRoot, topicID, &value); err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied, err
}

func applyTopicTitle(record *topicstate.Record, title, source string) {
	record.Title = title
	if title == "" || source == "" {
		record.TitleSource = ""
	} else {
		record.TitleSource = source
	}
	if source == topicTitleSourceManual || (source == topicTitleSourceAuto && isDefaultTopicTitle(title)) {
		record.AutoMeta = nil
	}
}

func logTopicStateReadFallback(workspaceRoot string, stateErr, legacyErr error, legacyExists bool) {
	attrs := []any{"scope", topicScopeKind(workspaceRoot), "error_type", topicStateErrorType(stateErr), "legacy_available", legacyExists}
	if legacyErr != nil {
		attrs = append(attrs, "legacy_error_type", topicStateErrorType(legacyErr))
	}
	slog.Warn("desktop: topic state read fallback", attrs...)
}

func topicStateReadable(workspaceRoot string) error {
	if _, err := desktopTopicState.snapshot(workspaceRoot); err != nil {
		legacy, legacyErr := readLegacyTopicSnapshot(workspaceRoot)
		if legacyErr == nil && legacy.exists {
			return nil
		}
		logTopicStateReadFallback(workspaceRoot, err, legacyErr, legacy.exists)
		var future *topicstate.FutureSchemaError
		if errors.As(err, &future) {
			return errors.New("topic metadata was written by a newer Reasonix version; upgrade Reasonix to open it safely")
		}
		return fmt.Errorf("topic metadata is unavailable (%s); retry or check the Reasonix state directory permissions", topicStateErrorType(err))
	}
	return nil
}

func mergeLegacyAutoMeta(existing, legacy json.RawMessage) json.RawMessage {
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(existing, &fields)
	legacyFields := map[string]json.RawMessage{}
	if json.Unmarshal(legacy, &legacyFields) != nil {
		return append(json.RawMessage(nil), existing...)
	}
	for _, key := range []string{"stage", "userTurns", "basisHash", "updatedAt"} {
		delete(fields, key)
	}
	maps.Copy(fields, legacyFields)
	data, _ := json.Marshal(fields)
	return data
}

func mergeLegacyMissingTitleIndex(workspaceRoot string, titles, sources map[string]string) error {
	currentTitles, err := loadLegacyStringMap(topicTitlesPath(workspaceRoot))
	if err != nil {
		return err
	}
	currentSources, err := loadLegacyStringMap(topicTitleSourcesPath(workspaceRoot))
	if err != nil {
		return err
	}
	deleted := deletedTopicSet()
	for id := range deleted {
		delete(currentTitles, id)
		delete(currentSources, id)
	}
	for id, title := range titles {
		if deleted[id] || strings.TrimSpace(currentTitles[id]) != "" {
			continue
		}
		currentTitles[id] = title
	}
	for id, source := range sources {
		if deleted[id] || strings.TrimSpace(currentSources[id]) != "" {
			continue
		}
		currentSources[id] = source
	}
	if err := writeLegacyStringMap(workspaceRoot, topicTitlesPath(workspaceRoot), currentTitles); err != nil {
		return err
	}
	return writeLegacyStringMap(workspaceRoot, topicTitleSourcesPath(workspaceRoot), currentSources)
}
