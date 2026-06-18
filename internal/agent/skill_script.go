package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(runSkillScript{}) }

// runSkillScript executes a script from a skill's scripts/ directory.
// The script must be bundled with the skill at <skill-path>/scripts/<name>.
type runSkillScript struct{}

func (runSkillScript) Name() string { return "run_skill_script" }

func (runSkillScript) Description() string {
	return "Run a script from a skill's scripts/ directory. The agent must specify the full path to the skill directory and the script filename."
}

func (runSkillScript) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "skill_dir":{"type":"string","description":"The absolute path to the skill directory (contains SKILL.md / <name>.md)."},
  "script":{"type":"string","description":"Script filename to execute from scripts/ (e.g. 'lint.py', 'check.sh')."},
  "args":{"type":"array","items":{"type":"string"},"description":"Optional arguments passed to the script."},
  "timeout_seconds":{"type":"integer","description":"Optional timeout in seconds (default 30).","minimum":1}
},
"required":["skill_dir","script"]
}`)
}

func (runSkillScript) ReadOnly() bool { return false }

func (runSkillScript) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SkillDir       string   `json:"skill_dir"`
		Script         string   `json:"script"`
		Args           []string `json:"args"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.SkillDir == "" {
		return "", fmt.Errorf("skill_dir is required")
	}
	if p.Script == "" {
		return "", fmt.Errorf("script is required")
	}

	scriptPath := filepath.Join(p.SkillDir, "scripts", p.Script)
	if _, err := os.Stat(scriptPath); err != nil {
		return "", fmt.Errorf("script %q not found at %s", p.Script, scriptPath)
	}

	timeout := p.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, scriptPath, p.Args...)
	cmd.Dir = p.SkillDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return string(output), fmt.Errorf("script timed out after %d seconds", timeout)
		}
		return string(output), fmt.Errorf("script exited with error: %w\n%s", err, string(output))
	}
	return string(output), nil
}
