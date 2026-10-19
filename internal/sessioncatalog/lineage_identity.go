package sessioncatalog

import (
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
)

type groupAnchor struct {
	topicID       string
	topicTitle    string
	createdAt     int64
	rootIndex     int
	fallbackIndex int
}

func logicalSessionAnchors(records []SessionRecord) map[string]*groupAnchor {
	anchors := map[string]*groupAnchor{}
	for i, rec := range records {
		if !rec.Recovered {
			id := agent.BranchID(rec.Path)
			anchors[id] = &groupAnchor{
				topicID: rec.TopicID, topicTitle: rec.TopicTitle,
				createdAt: rec.CreatedAt, rootIndex: i, fallbackIndex: i,
			}
		}
	}
	// Fill missing logical topics from recovered members (root topic empty or
	// root missing). Prefer earliest non-empty member topic_id.
	for i, rec := range records {
		if !rec.Recovered {
			continue
		}
		groupID := strings.TrimSpace(rec.RecoveryGroupID)
		if groupID == "" {
			continue
		}
		anchor := anchors[groupID]
		if anchor == nil {
			anchor = &groupAnchor{rootIndex: -1, createdAt: rec.CreatedAt, fallbackIndex: i}
			anchors[groupID] = anchor
		}
		if anchor.rootIndex < 0 {
			preferred := pickPreferredRecovery([]SessionRecord{records[anchor.fallbackIndex], rec})
			if preferred.Path == rec.Path {
				anchor.fallbackIndex = i
			}
		}
		topic := strings.TrimSpace(rec.TopicID)
		if topic == "" {
			continue
		}
		if strings.TrimSpace(anchor.topicID) == "" ||
			(rec.CreatedAt > 0 && (anchor.createdAt == 0 || rec.CreatedAt < anchor.createdAt) &&
				(anchor.rootIndex < 0 || strings.TrimSpace(records[anchor.rootIndex].TopicID) == "")) {
			// Keep a non-empty root topic when present; only override empty roots.
			if anchor.rootIndex >= 0 && strings.TrimSpace(records[anchor.rootIndex].TopicID) != "" {
				continue
			}
			anchor.topicID = topic
			anchor.topicTitle = rec.TopicTitle
			if rec.CreatedAt > 0 {
				anchor.createdAt = rec.CreatedAt
			}
		}
	}
	for _, anchor := range anchors {
		if strings.TrimSpace(anchor.topicID) == "" {
			// Content proof must retain metadata discovery's legacy identity.
			// With no surviving root, use the ordinary recovery representative;
			// every sibling still folds onto that one topic without sidecar writes.
			path := records[anchor.fallbackIndex].Path
			anchor.topicID = agent.LegacySessionTopicID(path)
			if anchor.topicTitle == "" {
				anchor.topicTitle = strings.TrimSuffix(filepath.Base(path), ".jsonl")
			}
		}
	}
	return anchors
}
