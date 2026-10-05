// goal_lifecycle.go — pause and resume of the Goal this session already holds.
package serve

import "net/http"

// goalLifecycleResult says whether the call changed the Goal and what state it
// is in afterwards. Changed=false is an answer, not a failure: resume found no
// stopped Goal, or pause found none running.
type goalLifecycleResult struct {
	Changed bool   `json:"changed"`
	Status  string `json:"status"`
	Goal    string `json:"goal,omitempty"`
}

// goalResume re-enters a stopped Goal with its objective, counters and delivery
// checkpoint. It does not start a turn; the next message continues the Goal.
func (s *Server) goalResume(w http.ResponseWriter, _ *http.Request) {
	ctl := s.ctl()
	changed := ctl.ResumeGoal()
	if changed {
		ctl.SetPlanMode(false)
	}
	writeJSON(w, goalLifecycleResult{Changed: changed, Status: ctl.GoalStatus(), Goal: ctl.Goal()})
}

// goalPause stops a running Goal at its next continuation boundary.
func (s *Server) goalPause(w http.ResponseWriter, _ *http.Request) {
	ctl := s.ctl()
	changed := ctl.PauseGoal()
	writeJSON(w, goalLifecycleResult{Changed: changed, Status: ctl.GoalStatus(), Goal: ctl.Goal()})
}
