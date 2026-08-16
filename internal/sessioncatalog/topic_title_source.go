package sessioncatalog

import "strings"

const manualTopicTitleSource = "manual"

func hydrateTopicDisplay(topic *TopicRecord) {
	topic.RepresentativePath = topicRepresentativePath(topic.Sessions)
	topic.TitleSource = topicDisplayTitleSource(topic.TitleSource, topic.Sessions, topic.RepresentativePath)
}

func topicDisplayTitleSource(source string, sessions []SessionRecord, representativePath string) string {
	representativePath = strings.TrimSpace(representativePath)
	for _, session := range sessions {
		if session.Path == representativePath && strings.TrimSpace(session.CustomTitle) != "" {
			return manualTopicTitleSource
		}
	}
	return source
}
