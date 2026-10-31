package usagecatalog

import "context"

// AgentRollup is what one team agent's seats spent over a day range.
type AgentRollup struct {
	Agent    string
	Total    int64
	Requests int64
}

// QueryAgents sums the calls made from team agents' seats over a day range,
// largest first. Calls made off the team carry no agent and are not listed.
func (c *Catalog) QueryAgents(ctx context.Context, fromDay, toDay, source string) ([]AgentRollup, error) {
	args := []any{fromDay, toDay}
	where := `day>=? AND day<=? AND agent<>''`
	if source != "" && source != "all" {
		where += ` AND source=?`
		args = append(args, source)
	}
	rows, err := c.db.QueryContext(ctx, `SELECT agent,SUM(total),SUM(requests)
        FROM usage_records WHERE `+where+` GROUP BY agent ORDER BY SUM(total) DESC,agent`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentRollup{}
	for rows.Next() {
		var row AgentRollup
		if err := rows.Scan(&row.Agent, &row.Total, &row.Requests); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
