package delegation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"reasonix/internal/contract/event"
)

const (
	SpawnToolName    = "spawn_agents"
	maxSpawnTasks    = 8
	spawnHeadBudget  = 8000
	spawnHeadMin     = 600
	spawnHeadMax     = 2000
	spawnTitleRunes  = 80
	spawnReadToolRef = "read_subagent_result"
)

type Spawner struct{ runner *Runner }

func NewSpawner(runner *Runner) *Spawner { return &Spawner{runner: runner} }

func (*Spawner) Name() string { return SpawnToolName }

func (*Spawner) Description() string {
	return "Run independent tasks in parallel, each in its own fresh session that does not see this conversation. A sub-agent has three tools: read_file, write_file and bash. In plan mode it can only read. Give each task a short title and a self-contained task. Every sub-agent's full answer is archived; you get back its title, a reference, and the opening of its answer. When an answer is cut off, read the rest with " + spawnReadToolRef + " using the reference. Each result also lists the files the sub-agent read and wrote, so you can open the exact ones yourself. Do not delegate reading a file you are about to edit: read it yourself. Use sub-agents for broad searches, for questions that need only a conclusion, and for independent work that can run in parallel. Sub-agents cannot start sub-agents."
}

func (*Spawner) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "tasks":{
    "type":"array",
    "minItems":1,
    "maxItems":8,
    "description":"Independent tasks to run at the same time.",
    "items":{
      "type":"object",
      "properties":{
        "title":{"type":"string","description":"Short label for the task, shown to the user."},
        "task":{"type":"string","description":"What to find out. Be specific about the evidence or summary to return."},
        "tools":{"type":"array","items":{"type":"string"},"description":"Optional narrowing of the sub-agent tools: any of read_file, write_file, bash."}
      },
      "required":["title","task"],
      "additionalProperties":false
    }
  }
},
"required":["tasks"],
"additionalProperties":false
}`)
}

func (*Spawner) ReadOnly() bool { return false }

func (*Spawner) PlanModeSafe() bool { return true }

func (*Spawner) CapabilityAliases() []string { return []string{"task:subagent"} }

func (*Spawner) ResolveProfile(args json.RawMessage) *event.Profile {
	var p struct {
		Tasks []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(args, &p); err != nil || len(p.Tasks) == 0 {
		return &event.Profile{Name: SpawnToolName}
	}
	return &event.Profile{Name: SpawnToolName, Count: len(p.Tasks)}
}

type spawnTask struct {
	Title string   `json:"title"`
	Task  string   `json:"task"`
	Tools []string `json:"tools"`
}

type spawnResult struct {
	Title string   `json:"title"`
	Ref   string   `json:"ref,omitempty"`
	Chars int      `json:"chars"`
	Head  string   `json:"head,omitempty"`
	More  bool     `json:"more,omitempty"`
	Read  []string `json:"read,omitempty"`
	Wrote []string `json:"wrote,omitempty"`
	Error string   `json:"error,omitempty"`
}

func (s *Spawner) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if s == nil || s.runner == nil {
		return "", fmt.Errorf("sub-agents are not available in this session")
	}
	var p struct {
		Tasks []spawnTask `json:"tasks"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("arguments must be {tasks: [{title, task}]}: %w", err)
	}
	if len(p.Tasks) == 0 {
		return "", fmt.Errorf("tasks must not be empty")
	}
	if len(p.Tasks) > maxSpawnTasks {
		return "", fmt.Errorf("at most %d tasks per call", maxSpawnTasks)
	}
	for i, t := range p.Tasks {
		if strings.TrimSpace(t.Task) == "" {
			return "", fmt.Errorf("task %d has no task text", i+1)
		}
	}
	head := min(max(spawnHeadBudget/len(p.Tasks), spawnHeadMin), spawnHeadMax)
	results := make([]spawnResult, len(p.Tasks))
	var wg sync.WaitGroup
	for i, t := range p.Tasks {
		wg.Go(func() { results[i] = s.one(ctx, i, t, head) })
	}
	wg.Wait()
	out, err := json.Marshal(map[string]any{"agents": results})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (s *Spawner) one(ctx context.Context, index int, t spawnTask, head int) spawnResult {
	title := truncateRunes(strings.TrimSpace(t.Title), spawnTitleRunes)
	res := spawnResult{Title: title}
	out, err := s.runner.Run(ctx, RunSpec{Title: title, Task: t.Task, Kind: "task", Name: title, Tools: t.Tools, Index: index})
	res.Ref = out.Ref
	if err != nil {
		res.Error = err.Error()
		return res
	}
	answer := strings.TrimSpace(out.Answer)
	res.Chars = utf8.RuneCountInString(answer)
	res.Head = truncateRunes(answer, head)
	res.More = res.Chars > head
	res.Read, res.Wrote = out.Read, out.Wrote
	return res
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n])
}
