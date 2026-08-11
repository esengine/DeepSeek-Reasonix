package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/fileutil"
)

const topicArchiveMetadataPendingDir = "desktop-topic-archive-pending"

type topicArchiveMetadataPending struct {
	TopicID   string                               `json:"topicId"`
	CreatedAt int64                                `json:"createdAt"`
	Sessions  []topicArchiveMetadataPendingSession `json:"sessions,omitempty"`
}

type topicArchiveMetadataPendingSession struct {
	Dir         string `json:"dir"`
	SessionPath string `json:"sessionPath"`
}

func topicArchiveMetadataPendingPath(topicID string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(topicID)))
	return filepath.Join(desktopConfigDir(), topicArchiveMetadataPendingDir, hex.EncodeToString(digest[:])+".json")
}

func markTopicArchiveMetadataPending(topicID string, targets []topicTrashTarget) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return fmt.Errorf("topicID is required")
	}
	path := topicArchiveMetadataPendingPath(topicID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	sessions := make([]topicArchiveMetadataPendingSession, 0, len(targets))
	for _, target := range targets {
		sessions = append(sessions, topicArchiveMetadataPendingSession{Dir: target.dir, SessionPath: target.sessionPath})
	}
	body, err := json.MarshalIndent(topicArchiveMetadataPending{
		TopicID: topicID, CreatedAt: time.Now().UnixMilli(), Sessions: sessions,
	}, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, body, 0o644)
}

func clearTopicArchiveMetadataPending(topicID string) error {
	if err := os.Remove(topicArchiveMetadataPendingPath(topicID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func listTopicArchiveMetadataPending() ([]topicArchiveMetadataPending, error) {
	dir := filepath.Join(desktopConfigDir(), topicArchiveMetadataPendingDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	pending := make([]topicArchiveMetadataPending, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var item topicArchiveMetadataPending
		if err := json.Unmarshal(body, &item); err != nil {
			return nil, err
		}
		item.TopicID = strings.TrimSpace(item.TopicID)
		if item.TopicID == "" || filepath.Base(topicArchiveMetadataPendingPath(item.TopicID)) != entry.Name() {
			return nil, fmt.Errorf("invalid topic archive metadata marker")
		}
		pending = append(pending, item)
	}
	return pending, nil
}

func reconcileTopicArchiveMetadataPending(deleteTopic func(string) error) error {
	if deleteTopic == nil {
		return fmt.Errorf("topic archive metadata reconciler is unavailable")
	}
	pending, err := listTopicArchiveMetadataPending()
	if err != nil {
		return err
	}
	var errs []error
	for _, item := range pending {
		itemFailed := false
		for _, target := range item.Sessions {
			sessionPath, key, err := validateSessionPath(target.Dir, target.SessionPath)
			if err == nil {
				err = agent.MarkCleanupPending(sessionPath, "delete")
			}
			if err == nil {
				err = reconcileDesktopTrashSessionArtifacts(target.Dir, sessionPath, key)
			}
			if err != nil {
				errs = append(errs, err)
				itemFailed = true
			}
		}
		if err := deleteTopic(item.TopicID); err != nil {
			errs = append(errs, err)
			itemFailed = true
		}
		if itemFailed {
			continue
		}
		if err := clearTopicArchiveMetadataPending(item.TopicID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
