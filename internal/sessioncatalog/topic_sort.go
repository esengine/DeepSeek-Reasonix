package sessioncatalog

import "strings"

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
