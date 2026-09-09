package sessioncatalog

func hydrateTopicDisplay(topic *TopicRecord) {
	topic.RepresentativePath = topicRepresentativePath(topic.Sessions)
}
