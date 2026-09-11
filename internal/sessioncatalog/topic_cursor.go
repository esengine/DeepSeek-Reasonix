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
	if cursor.ManualOrder {
		return false, errCursorSortModeChanged
	}
	pinnedValue := 0
	if pinned {
		pinnedValue = 1
	}
	return pinnedValue < cursor.Pinned ||
		pinnedValue == cursor.Pinned && activity < cursor.Activity ||
		pinnedValue == cursor.Pinned && activity == cursor.Activity && topicID > cursor.TopicID, nil
}

// TopicSortKeyAfterOrderedCursor is the manual-order counterpart used by the
// metadata continuity path. Its comparison exactly matches ListTopics SQL.
func TopicSortKeyAfterOrderedCursor(encoded string, pinned bool, sortOrder int, activity int64, topicID string) (bool, error) {
	cursor, err := decodeCursor(encoded)
	if err != nil {
		return false, err
	}
	if cursor == nil {
		return true, nil
	}
	if !cursor.ManualOrder {
		return false, errCursorSortModeChanged
	}
	pinnedValue := 0
	if pinned {
		pinnedValue = 1
	}
	manualSortOrder := int64(sortOrder)
	if sortOrder < 0 {
		manualSortOrder = unrankedTopicSortOrder
	}
	return pinnedValue < cursor.Pinned ||
		pinnedValue == cursor.Pinned && manualSortOrder > cursor.SortOrder ||
		pinnedValue == cursor.Pinned && manualSortOrder == cursor.SortOrder && activity < cursor.Activity ||
		pinnedValue == cursor.Pinned && manualSortOrder == cursor.SortOrder && activity == cursor.Activity && topicID > cursor.TopicID, nil
}
