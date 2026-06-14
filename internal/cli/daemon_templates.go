package cli

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"reasonix/internal/daemon"
)

type daemonTemplateSpec struct {
	ID          string
	Description string
	GoalStarter string
	Aliases     []string
}

var daemonTemplates = []daemonTemplateSpec{
	{
		ID:          "daily-triage",
		Description: "Daily PR / issue triage with deterministic cron and wakeup budget.",
		GoalStarter: "每天检查本仓库未处理的 PR 和 issue，先起草 triage 建议；评论、关闭、合并或其他写操作前等待我审批。",
	},
	{
		ID:          "ci-watcher",
		Description: "Wait for GitHub CI success, diagnose failures, and continue the original goal.",
		GoalStarter: "等待目标 PR 或分支的 CI 结果；失败时总结原因和修复建议，成功后继续准备发布、合并或下一步说明。",
		Aliases:     []string{"ci-watch"},
	},
	{
		ID:          "release-assist",
		Description: "Wake on changelog or version-file changes and prepare release checks.",
		GoalStarter: "当 changelog 或版本文件变化后，检查发布准备状态、版本号和发布说明；真正发布前等待我审批。",
	},
	{
		ID:          "repo-health",
		Description: "Daily repository health scan with deterministic schedule and bounded model budget.",
		GoalStarter: "每天巡检仓库健康状况，关注 flaky tests、长期未合并 PR、过期依赖和轻量维护建议；修复或写操作前等待我审批。",
	},
}

func daemonTemplatesCmd(args []string) int {
	fs := flag.NewFlagSet("daemon templates", flag.ContinueOnError)
	jsonLike := fs.Bool("json", false, "以简单 JSON lines 输出模板")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	specs := append([]daemonTemplateSpec(nil), daemonTemplates...)
	sort.Slice(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	for _, spec := range specs {
		if *jsonLike {
			fmt.Printf(`{"id":%q,"description":%q,"goal_starter":%q}`+"\n", spec.ID, spec.Description, spec.GoalStarter)
			continue
		}
		fmt.Printf("%s\n  %s\n  goal starter: %s\n", spec.ID, spec.Description, spec.GoalStarter)
	}
	return 0
}

func daemonApplyTemplateCmd(args []string) int {
	fs := flag.NewFlagSet("daemon apply-template", flag.ContinueOnError)
	addr := fs.String("addr", daemon.DefaultAddr, "daemon 地址")
	dir := fs.String("dir", "", "会话目录（用于读取本地 token）")
	templateID := fs.String("template", "", "模板 ID：daily-triage、ci-watcher、release-assist、repo-health")
	sessionID := fs.String("session", "", "要配置模板的 session ID")
	dailyAt := fs.String("daily-at", "", "daily 模板唤醒时间 HH:MM")
	timezone := fs.String("timezone", "", "daily-at 使用的 IANA 时区，例如 Asia/Shanghai")
	dailyWakeups := fs.Int("daily-wakeups", -2, "每日自动唤醒次数上限；-1 表示不修改预算")
	maxGoalAutoTurns := fs.Int("max-goal-auto-turns", -2, "每个 goal 最大自动续跑轮次；-1 表示不修改")
	dailyModelCalls := fs.Int("daily-model-calls", -2, "每日模型调用次数上限；-1 表示不修改")
	dailyModelCost := fs.Float64("daily-model-cost", -2, "每日模型费用上限；-1 表示不修改")
	source := fs.String("source", "", "CI 模板事件来源：workflow_run、check_suite 或 status")
	repo := fs.String("repo", "", "CI 模板关联仓库 owner/repo")
	pr := fs.Int("pr", 0, "CI 模板关联 PR 编号")
	subject := fs.String("subject", "", "等待对象说明")
	paths := fs.String("paths", "", "release 模板等待文件，逗号分隔")
	ignore := fs.String("ignore", "", "release 模板额外忽略 glob，逗号分隔")
	debounce := fs.String("debounce", "", "release 模板文件变化防抖时间，例如 3s")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	session := strings.TrimSpace(*sessionID)
	if session == "" {
		fmt.Fprintln(os.Stderr, "error: --session is required")
		return 2
	}
	spec, ok := lookupDaemonTemplate(*templateID)
	if !ok {
		fmt.Fprintln(os.Stderr, "error: --template must be daily-triage, ci-watcher, release-assist, or repo-health")
		return 2
	}

	rc := 1
	switch spec.ID {
	case "daily-triage":
		rc = daemonDailyTriageCmd(templateDailyTriageArgs(*addr, *dir, session, *dailyAt, *timezone, *dailyWakeups))
	case "ci-watcher":
		rc = daemonCIWatchCmd(templateCIWatcherArgs(*addr, *dir, session, *source, *repo, *pr, *subject))
	case "release-assist":
		rc = daemonReleaseAssistCmd(templateReleaseAssistArgs(*addr, *dir, session, *paths, *ignore, *debounce, *subject))
	case "repo-health":
		rc = daemonRepoHealthCmd(templateRepoHealthArgs(*addr, *dir, session, *dailyAt, *timezone, *dailyWakeups, *maxGoalAutoTurns, *dailyModelCalls, *dailyModelCost))
	}
	if rc != 0 {
		return rc
	}
	fmt.Printf("template: %s\n", spec.ID)
	fmt.Printf("goal starter: %s\n", spec.GoalStarter)
	return 0
}

func lookupDaemonTemplate(id string) (daemonTemplateSpec, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, spec := range daemonTemplates {
		if id == spec.ID {
			return spec, true
		}
		for _, alias := range spec.Aliases {
			if id == alias {
				return spec, true
			}
		}
	}
	return daemonTemplateSpec{}, false
}

func templateBaseArgs(addr, dir, session string) []string {
	args := []string{"--addr", addr, "--session", session}
	if strings.TrimSpace(dir) != "" {
		args = append(args, "--dir", dir)
	}
	return args
}

func templateDailyTriageArgs(addr, dir, session, dailyAt, timezone string, dailyWakeups int) []string {
	args := templateBaseArgs(addr, dir, session)
	if strings.TrimSpace(dailyAt) == "" {
		dailyAt = "09:00"
	}
	args = append(args, "--daily-at", dailyAt)
	if strings.TrimSpace(timezone) != "" {
		args = append(args, "--timezone", timezone)
	}
	if dailyWakeups == -2 {
		dailyWakeups = 1
	}
	args = append(args, "--daily-wakeups", strconv.Itoa(dailyWakeups))
	return args
}

func templateCIWatcherArgs(addr, dir, session, source, repo string, pr int, subject string) []string {
	args := templateBaseArgs(addr, dir, session)
	if strings.TrimSpace(source) != "" {
		args = append(args, "--source", source)
	}
	if strings.TrimSpace(repo) != "" {
		args = append(args, "--repo", repo)
	}
	if pr > 0 {
		args = append(args, "--pr", strconv.Itoa(pr))
	}
	if strings.TrimSpace(subject) != "" {
		args = append(args, "--subject", subject)
	}
	return args
}

func templateReleaseAssistArgs(addr, dir, session, paths, ignore, debounce, subject string) []string {
	args := templateBaseArgs(addr, dir, session)
	if strings.TrimSpace(paths) != "" {
		args = append(args, "--paths", paths)
	}
	if strings.TrimSpace(ignore) != "" {
		args = append(args, "--ignore", ignore)
	}
	if strings.TrimSpace(debounce) != "" {
		args = append(args, "--debounce", debounce)
	}
	if strings.TrimSpace(subject) != "" {
		args = append(args, "--subject", subject)
	}
	return args
}

func templateRepoHealthArgs(addr, dir, session, dailyAt, timezone string, dailyWakeups, maxGoalAutoTurns, dailyModelCalls int, dailyModelCost float64) []string {
	args := templateBaseArgs(addr, dir, session)
	if strings.TrimSpace(dailyAt) == "" {
		dailyAt = "10:00"
	}
	args = append(args, "--daily-at", dailyAt)
	if strings.TrimSpace(timezone) != "" {
		args = append(args, "--timezone", timezone)
	}
	if dailyWakeups == -2 {
		dailyWakeups = 1
	}
	args = append(args, "--daily-wakeups", strconv.Itoa(dailyWakeups))
	if maxGoalAutoTurns != -2 {
		args = append(args, "--max-goal-auto-turns", strconv.Itoa(maxGoalAutoTurns))
	}
	if dailyModelCalls != -2 {
		args = append(args, "--daily-model-calls", strconv.Itoa(dailyModelCalls))
	}
	if dailyModelCost != -2 {
		args = append(args, "--daily-model-cost", strconv.FormatFloat(dailyModelCost, 'g', -1, 64))
	}
	return args
}
