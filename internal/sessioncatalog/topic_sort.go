package sessioncatalog

import (
	"errors"
	"strings"
)

const unrankedTopicSortOrder int64 = 1<<63 - 1

var errCursorSortModeChanged = errors.New("session catalog cursor sort mode changed")

func topicPageSortExpression(sortMode string) string {
	if strings.TrimSpace(sortMode) == "created" {
		return "COALESCE(NULLIF(created_at,0),last_activity_at,0)"
	}
	return "COALESCE(NULLIF(last_activity_at,0),created_at,0)"
}

func topicPageSortValue(topic TopicRecord, sortMode string) int64 {
	if strings.TrimSpace(sortMode) == "created" {
		if topic.CreatedAt > 0 {
			return topic.CreatedAt
		}
		return topic.LastActivityAt
	}
	if topic.LastActivityAt > 0 {
		return topic.LastActivityAt
	}
	return topic.CreatedAt
}

func topicPageManualSortValue(topic TopicRecord) int64 {
	if topic.SortOrder < 0 {
		return unrankedTopicSortOrder
	}
	return int64(topic.SortOrder)
}

func topicPageManualSortExpression() string {
	return "CASE WHEN metadata_present=1 AND sort_order>=0 THEN sort_order ELSE 9223372036854775807 END"
}
