package sessioncatalog

import (
	"context"
	"database/sql"
	"strings"
)

func (c *Catalog) ListTopics(ctx context.Context, req TopicPageRequest) (TopicPage, error) {
	out := TopicPage{Items: []TopicRecord{}, Revision: c.revision.Load()}
	req.Scope, req.WorkspaceRoot = normalizeScope(req.Scope, req.WorkspaceRoot)
	if req.Limit <= 0 {
		req.Limit = DefaultLimit
	}
	if req.Limit > MaxLimit {
		req.Limit = MaxLimit
	}
	cursor, err := decodeCursor(req.Cursor)
	if err != nil {
		return out, err
	}
	if cursor != nil && cursor.ManualOrder != req.ManualOrder {
		return out, errCursorSortModeChanged
	}
	rootKey := c.workspaceRootKey(req.Scope, req.WorkspaceRoot)
	args := []any{req.Scope, rootKey}
	where := `scope=? AND workspace_root_key=?`
	if query := strings.TrimSpace(req.Query); query != "" {
		where += ` AND lower(title) LIKE ?`
		args = append(args, "%"+strings.ToLower(query)+"%")
	}
	if cutoff := timeFilterCutoff(req.TimeFilter, c.opts.Now()); cutoff > 0 {
		where += ` AND last_activity_at>=?`
		args = append(args, cutoff)
	}
	scanCursor := cursor
	scanLimit := max(req.Limit+1, 64)
	for len(out.Items) <= req.Limit {
		query, pageArgs := topicPageQuery(req, where, args, scanCursor, scanLimit)
		rows, queryErr := c.db.QueryContext(ctx, query, pageArgs...)
		if queryErr != nil {
			return out, queryErr
		}
		scanned, scanErr := scanTopicRows(rows, scanLimit)
		if scanErr != nil {
			return out, scanErr
		}
		rawCount := len(scanned)
		overflow := false
		for _, item := range scanned {
			sessions, listErr := c.listTopicSessionsByRootKey(ctx, TopicKey{
				Scope: item.Scope, WorkspaceRoot: item.WorkspaceRoot, TopicID: item.TopicID,
			}, rootKey)
			if listErr != nil {
				return TopicPage{Items: []TopicRecord{}, Revision: out.Revision}, listErr
			}
			if len(sessions) == 0 {
				continue
			}
			// Skip recovery shells that lost their ordinary representative while
			// lineage is re-anchored, unless the topic is explicitly pinned.
			hasOrdinary := false
			for _, session := range sessions {
				if session.OrdinaryVisible || (!session.Recovered && !session.RecoveryCopy) {
					hasOrdinary = true
					break
				}
			}
			if !hasOrdinary && !item.Pinned {
				continue
			}
			item.Sessions = sessions
			hydrateTopicDisplay(&item)
			out.Items = append(out.Items, item)
			if len(out.Items) > req.Limit {
				overflow = true
				break
			}
		}
		if overflow || rawCount < scanLimit || rawCount == 0 {
			break
		}
		scanCursor = cursorForTopic(scanned[rawCount-1], req)
	}
	more := len(out.Items) > req.Limit
	if more {
		out.Items = out.Items[:req.Limit]
	}
	if more && len(out.Items) > 0 {
		out.NextCursor = encodeCursor(*cursorForTopic(out.Items[len(out.Items)-1], req))
	}
	return out, nil
}

func topicPageQuery(req TopicPageRequest, where string, args []any, cursor *pageCursor, limit int) (string, []any) {
	sortExpression := topicPageSortExpression(req.SortMode)
	manualSortExpression := topicPageManualSortExpression()
	pageArgs := append([]any(nil), args...)
	if cursor != nil && req.ManualOrder {
		where += ` AND (pinned<? OR (pinned=? AND ` + manualSortExpression + `>?) OR ` +
			`(pinned=? AND ` + manualSortExpression + `=? AND ` + sortExpression + `<?) OR ` +
			`(pinned=? AND ` + manualSortExpression + `=? AND ` + sortExpression + `=? AND topic_id>?))`
		pageArgs = append(pageArgs,
			cursor.Pinned,
			cursor.Pinned, cursor.SortOrder,
			cursor.Pinned, cursor.SortOrder, cursor.Activity,
			cursor.Pinned, cursor.SortOrder, cursor.Activity, cursor.TopicID,
		)
	} else if cursor != nil {
		where += ` AND (pinned<? OR (pinned=? AND ` + sortExpression + `<?) OR (pinned=? AND ` + sortExpression + `=? AND topic_id>?))`
		pageArgs = append(pageArgs, cursor.Pinned, cursor.Pinned, cursor.Activity,
			cursor.Pinned, cursor.Activity, cursor.TopicID)
	}
	orderBy := `pinned DESC,` + sortExpression + ` DESC,topic_id ASC`
	if req.ManualOrder {
		orderBy = `pinned DESC,` + manualSortExpression + ` ASC,` + sortExpression + ` DESC,topic_id ASC`
	}
	pageArgs = append(pageArgs, limit)
	return `SELECT scope,workspace_root,topic_id,title,title_source,pinned,
		CASE WHEN metadata_present=1 THEN sort_order ELSE -1 END,
		turns,turns_state,created_at,last_activity_at,recovery_state,recovery_branch_count,
		recovery_unresolved_count,recovery_cleanup_eligible_count,health
		FROM catalog_topics WHERE ` + where + ` ORDER BY ` + orderBy + ` LIMIT ?`, pageArgs
}

func scanTopicRows(rows *sql.Rows, capacity int) ([]TopicRecord, error) {
	defer rows.Close()
	// Drain before hydrating sessions: the nested read needs another connection
	// and an open cursor deadlocks when the in-memory pool is saturated.
	scanned := make([]TopicRecord, 0, capacity)
	for rows.Next() {
		var item TopicRecord
		if err := rows.Scan(&item.Scope, &item.WorkspaceRoot, &item.TopicID, &item.Title,
			&item.TitleSource, &item.Pinned, &item.SortOrder, &item.Turns, &item.TurnsState,
			&item.CreatedAt, &item.LastActivityAt, &item.RecoveryState, &item.RecoveryBranchCount,
			&item.RecoveryUnresolvedCount, &item.RecoveryCleanupEligibleCount, &item.Health); err != nil {
			return nil, err
		}
		scanned = append(scanned, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return scanned, nil
}

func cursorForTopic(topic TopicRecord, req TopicPageRequest) *pageCursor {
	pinned := 0
	if topic.Pinned {
		pinned = 1
	}
	return &pageCursor{
		Pinned: pinned, ManualOrder: req.ManualOrder,
		SortOrder: topicPageManualSortValue(topic),
		Activity:  topicPageSortValue(topic, req.SortMode), TopicID: topic.TopicID,
	}
}
