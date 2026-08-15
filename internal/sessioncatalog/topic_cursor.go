package sessioncatalog

// TopicSortKeyAfterCursor reports whether a topic key belongs after an
// exclusive ListTopics cursor under the catalog's canonical ordering.
func TopicSortKeyAfterCursor(encoded string, pinned bool, activity int64, topicID string) (bool, error) {
	cursor, err := decodeCursor(encoded)
	if err != nil {
		return false, err
	}
	if cursor == nil {
		return true, nil
	}
	pinnedValue := 0
	if pinned {
		pinnedValue = 1
	}
	return pinnedValue < cursor.Pinned ||
		pinnedValue == cursor.Pinned && activity < cursor.Activity ||
		pinnedValue == cursor.Pinned && activity == cursor.Activity && topicID > cursor.TopicID, nil
}
