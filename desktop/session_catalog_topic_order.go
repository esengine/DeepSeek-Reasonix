package main

import (
	"strings"

	"reasonix/internal/sessioncatalog"
)

func projectTopicLess(left, right ProjectNode, sortMode string, manualOrder bool) bool {
	if left.Pinned != right.Pinned {
		return left.Pinned
	}
	if manualOrder {
		leftRank, rightRank := left.SortOrder, right.SortOrder
		if leftRank < 0 {
			leftRank = int(^uint(0) >> 1)
		}
		if rightRank < 0 {
			rightRank = int(^uint(0) >> 1)
		}
		if leftRank != rightRank {
			return leftRank < rightRank
		}
	}
	leftActivity := projectTopicSortValue(left.CreatedAt, left.LastActivityAt, sortMode)
	rightActivity := projectTopicSortValue(right.CreatedAt, right.LastActivityAt, sortMode)
	if leftActivity != rightActivity {
		return leftActivity > rightActivity
	}
	return left.TopicID < right.TopicID
}

func manualTopicOrderFor(scope, workspaceRoot string) bool {
	f := loadProjectsFile()
	if strings.TrimSpace(scope) != "project" {
		return f.GlobalManualTopicOrder
	}
	if index := projectIndexByRoot(f.Projects, workspaceRoot); index >= 0 {
		return f.Projects[index].ManualTopicOrder
	}
	return false
}

func encodeProjectTopicCursor(topic sessioncatalog.TopicRecord, sortMode string, manualOrder bool) string {
	pinned := 0
	if topic.Pinned {
		pinned = 1
	}
	activity := projectTopicSortValue(topic.CreatedAt, topic.LastActivityAt, sortMode)
	if manualOrder {
		return sessioncatalog.EncodeOrderedTopicCursor(pinned, topic.SortOrder, activity, topic.TopicID)
	}
	return sessioncatalog.EncodeTopicCursor(pinned, activity, topic.TopicID)
}

func encodeProjectNodeCursor(topic ProjectNode, sortMode string, manualOrder bool) string {
	pinned := 0
	if topic.Pinned {
		pinned = 1
	}
	activity := projectTopicSortValue(topic.CreatedAt, topic.LastActivityAt, sortMode)
	if manualOrder {
		return sessioncatalog.EncodeOrderedTopicCursor(pinned, topic.SortOrder, activity, topic.TopicID)
	}
	return sessioncatalog.EncodeTopicCursor(pinned, activity, topic.TopicID)
}
