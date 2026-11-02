package serve

import (
	"encoding/json"
	"os"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/teamroster"
	"reasonix/internal/state/usagereport"
)

type teamViewCache struct {
	previews fileutil.ReadCache[string]
	usage    fileutil.ReadCache[usagereport.Report]
	lines    fileutil.ReadCache[[]teamroster.GroupLine]
	rounds   fileutil.ReadCache[teamroster.RoundState]
}

func (t *hubTeam) lastGroupLine(group string) ([]teamroster.GroupLine, error) {
	path, _, err := t.store.GroupPaths(group)
	if err != nil {
		return nil, err
	}
	return t.viewCache.lines.Load(path, func(string) ([]teamroster.GroupLine, error) { return t.store.LinesBefore(group, 0, 1) })
}

func (t *hubTeam) groupRound(group string) (teamroster.RoundState, error) {
	_, path, err := t.store.GroupPaths(group)
	if err != nil {
		return teamroster.RoundState{}, err
	}
	return t.viewCache.rounds.Load(path, func(string) (teamroster.RoundState, error) { return t.store.Round(group) })
}

func (t *hubTeam) preview(path string) string {
	v, _ := t.viewCache.previews.Load(path, func(path string) (string, error) {
		return sessionstore.LatestPreview(path), nil
	})
	return v
}

func (t *hubTeam) usage(path string) (usagereport.Report, bool) {
	v, err := t.viewCache.usage.Load(usagereport.Path(path), func(path string) (usagereport.Report, error) {
		var report usagereport.Report
		data, err := os.ReadFile(path)
		if err == nil {
			err = json.Unmarshal(data, &report)
		}
		return report, err
	})
	return v, err == nil
}
