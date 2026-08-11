package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"reasonix/internal/taskmonitor"
)

// taskStore is the taskmonitor.Store used by the task CLI commands.
// Tests override it with mock stores via SetTaskStore.  When nil, the
// CLI defaults to a FileStore backed by .reasonix/tasks under the
// project directory.
var taskStore taskmonitor.Store

// taskJobKiller is an optional JobKiller for stopping running tasks.
// It is injected by the main wiring or by cli.go when a controller is
// available (Desktop or running session).  When nil, kill is a no-op.
var taskJobKiller taskmonitor.JobKiller

// SetTaskStore replaces the Store used by the task subcommands.
func SetTaskStore(s taskmonitor.Store) { taskStore = s }

// SetTaskJobKiller sets the JobKiller for control subcommands.
// Called by the wiring when a controller with jobs.Manager is available.
func SetTaskJobKiller(k taskmonitor.JobKiller) { taskJobKiller = k }

// The monitor commands are a content-free machine interface. Scrub optional
// free-form summaries at the output boundary as well as at current write sites
// so snapshots persisted by older versions cannot disclose paths or commands.
func contentFreeTaskSnapshot(s taskmonitor.TaskSnapshot) taskmonitor.TaskSnapshot {
	s.ErrorSummary = ""
	return s
}

func contentFreeTaskSnapshots(tasks []taskmonitor.TaskSnapshot) []taskmonitor.TaskSnapshot {
	if tasks == nil {
		return nil
	}
	contentFree := make([]taskmonitor.TaskSnapshot, len(tasks))
	for i := range tasks {
		contentFree[i] = contentFreeTaskSnapshot(tasks[i])
	}
	return contentFree
}

func contentFreeTaskEvents(events []taskmonitor.TaskEvent) []taskmonitor.TaskEvent {
	if events == nil {
		return nil
	}
	contentFree := make([]taskmonitor.TaskEvent, len(events))
	for i := range events {
		contentFree[i] = events[i]
		contentFree[i].ErrorSummary = ""
	}
	return contentFree
}

func taskCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix task <list|show|monitor|status|events|stop|cancel|requeue|open-session|tmux> [flags]")
		return 2
	}
	store := taskStore
	if store == nil {
		store = taskmonitor.NewFileStore(".reasonix/tasks")
	}
	switch args[0] {
	case "list":
		// Keep the pre-task-monitor machine contract intact.  New monitor
		// commands live below `task monitor` so existing callers do not see a
		// different schema or task identity model under the same command.
		return runTaskCommand(args, os.Stdout)
	case "show":
		return runTaskCommand(args, os.Stdout)
	case "monitor":
		return taskMonitorCommand(store, args[1:])
	case "machine-list":
		return runTaskCommand(append([]string{"list"}, args[1:]...), os.Stdout)
	case "machine-show":
		return runTaskCommand(append([]string{"show"}, args[1:]...), os.Stdout)
	case "status":
		return taskStatusCmd(store, args[1:])
	case "events":
		return taskEventsCmd(store, args[1:])
	case "stop":
		return taskStopCmd(store, args[1:])
	case "cancel":
		return taskCancelCmd(store, args[1:])
	case "requeue":
		return taskRequeueCmd(store, args[1:])
	case "open-session":
		return taskOpenSessionCmd(store, args[1:])
	case "tmux":
		return taskTmuxCmd(store, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown task subcommand: %s\n", args[0])
		return 2
	}
}

func taskMonitorCommand(store taskmonitor.Store, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix task monitor <list|status|events|stop|cancel|requeue|open-session> [flags]")
		return 2
	}
	switch args[0] {
	case "list":
		return taskListCmd(store, args[1:])
	case "status":
		return taskStatusCmd(store, args[1:])
	case "events":
		return taskEventsCmd(store, args[1:])
	case "stop":
		return taskStopCmd(store, args[1:])
	case "cancel":
		return taskCancelCmd(store, args[1:])
	case "requeue":
		return taskRequeueCmd(store, args[1:])
	case "open-session":
		return taskOpenSessionCmd(store, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown task monitor subcommand: %s\n", args[0])
		return 2
	}
}

func taskTmuxCmd(store taskmonitor.Store, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix task tmux <attach|status|open|detach>")
		return 2
	}
	a := taskmonitor.NewTmuxAdapter(store, ".reasonix/tasks")
	switch args[0] {
	case "attach":
		return taskTmuxAttachCmd(a, args[1:])
	case "status":
		return taskTmuxStatusCmd(a, args[1:])
	case "open":
		return taskTmuxOpenCmd(a, args[1:])
	case "detach":
		return taskTmuxDetachCmd(a, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown task tmux subcommand: %s\n", args[0])
		return 2
	}
}

func taskTmuxFlags(name string, args []string) (string, string, bool, *flag.FlagSet, int) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	dir := fs.String("dir", "", "project directory scope")
	session := fs.String("session", "", "tmux session name")
	jsonOut := fs.Bool("json", false, "output as JSON")
	if err := fs.Parse(reorderTaskID(fs, args)); err != nil {
		return "", "", false, fs, 2
	}
	return *dir, *session, *jsonOut, fs, 0
}

// reorderTaskID lets users place the positional task ID before or after flags.
// The standard flag package stops parsing at the first positional argument.
func reorderTaskID(fs *flag.FlagSet, args []string) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, 1)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			flags = append(flags, arg)
			positionals = append(positionals, args[i+1:]...)
			break
		}

		name, inlineValue := taskFlagName(arg)
		if name == "" {
			positionals = append(positionals, arg)
			continue
		}

		flags = append(flags, arg)
		registered := fs.Lookup(name)
		if registered == nil || inlineValue {
			continue
		}
		if boolean, ok := registered.Value.(interface{ IsBoolFlag() bool }); ok && boolean.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positionals...)
}

func taskFlagName(arg string) (name string, inlineValue bool) {
	if arg == "-" || !strings.HasPrefix(arg, "-") {
		return "", false
	}
	name = strings.TrimPrefix(arg, "-")
	name = strings.TrimPrefix(name, "-")
	if name == "" {
		return "", false
	}
	if before, _, ok := strings.Cut(name, "="); ok {
		return before, true
	}
	return name, false
}

func printTmuxResult(r taskmonitor.TmuxResult, jsonOut bool) int {
	if !jsonOut {
		fmt.Fprintln(os.Stderr, "tmux task commands require --json")
		return 2
	}
	if err := json.NewEncoder(os.Stdout).Encode(r); err != nil {
		return 1
	}
	if r.Error != nil {
		return 1
	}
	return 0
}

func taskTmuxAttachCmd(a *taskmonitor.TmuxAdapter, args []string) int {
	dir, session, jsonOut, fs, code := taskTmuxFlags("task tmux attach", args)
	if code != 0 || fs.Arg(0) == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix task tmux attach <id> --json [--dir DIR] [--session NAME]")
		return 2
	}
	return printTmuxResult(a.Attach(context.Background(), dir, fs.Arg(0), session), jsonOut)
}

func taskTmuxStatusCmd(a *taskmonitor.TmuxAdapter, args []string) int {
	dir, _, jsonOut, fs, code := taskTmuxFlags("task tmux status", args)
	if code != 0 || fs.Arg(0) == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix task tmux status <id> --json [--dir DIR]")
		return 2
	}
	return printTmuxResult(a.Status(context.Background(), dir, fs.Arg(0)), jsonOut)
}

func taskTmuxOpenCmd(a *taskmonitor.TmuxAdapter, args []string) int {
	dir, _, jsonOut, fs, code := taskTmuxFlags("task tmux open", args)
	if code != 0 || fs.Arg(0) == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix task tmux open <id> --json [--dir DIR]")
		return 2
	}
	return printTmuxResult(a.Open(context.Background(), dir, fs.Arg(0)), jsonOut)
}

func taskTmuxDetachCmd(a *taskmonitor.TmuxAdapter, args []string) int {
	dir, _, jsonOut, fs, code := taskTmuxFlags("task tmux detach", args)
	if code != 0 || fs.Arg(0) == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix task tmux detach <id> --json [--dir DIR]")
		return 2
	}
	return printTmuxResult(a.Detach(context.Background(), dir, fs.Arg(0)), jsonOut)
}

// list

func taskListCmd(store taskmonitor.Store, args []string) int {
	fs := flag.NewFlagSet("task list", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output as JSON")
	dir := fs.String("dir", "", "project directory scope")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*jsonOut {
		fmt.Fprintln(os.Stderr, "task list requires --json")
		return 2
	}

	ctx := context.Background()
	tasks, err := store.ListTasks(ctx, *dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	tasks = contentFreeTaskSnapshots(tasks)
	output := struct {
		SchemaVersion int                        `json:"schema_version"`
		Tasks         []taskmonitor.TaskSnapshot `json:"tasks"`
	}{SchemaVersion: 1, Tasks: tasks}
	if tasks == nil {
		output.Tasks = []taskmonitor.TaskSnapshot{}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// status

func taskStatusCmd(store taskmonitor.Store, args []string) int {
	fs := flag.NewFlagSet("task status", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output as JSON")
	dir := fs.String("dir", "", "project directory scope")
	if err := fs.Parse(reorderTaskID(fs, args)); err != nil {
		return 2
	}
	if !*jsonOut {
		fmt.Fprintln(os.Stderr, "task status requires --json")
		return 2
	}
	id := fs.Arg(0)
	if id == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix task status <id> --json [--dir DIR]")
		return 2
	}

	ctx := context.Background()
	snap, err := store.GetTask(ctx, *dir, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	output := struct {
		SchemaVersion int                       `json:"schema_version"`
		Task          *taskmonitor.TaskSnapshot `json:"task"`
	}{SchemaVersion: 1}
	if snap != nil {
		contentFree := contentFreeTaskSnapshot(*snap)
		output.Task = &contentFree
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// events

func taskEventsCmd(store taskmonitor.Store, args []string) int {
	fs := flag.NewFlagSet("task events", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output as JSON array")
	jsonl := fs.Bool("jsonl", false, "output as JSONL stream")
	dir := fs.String("dir", "", "project directory scope")
	after := fs.Int("after", 0, "only events with Sequence > N")
	follow := fs.Bool("follow", false, "poll for new events until interrupted")
	if err := fs.Parse(reorderTaskID(fs, args)); err != nil {
		return 2
	}
	if !*jsonOut && !*jsonl {
		fmt.Fprintln(os.Stderr, "task events requires --json or --jsonl")
		return 2
	}
	id := fs.Arg(0)
	if id == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix task events <id> --json|--jsonl [--dir DIR] [--after N] [--follow]")
		return 2
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cursor := *after

	for {
		events, err := store.ListEvents(ctx, *dir, id, cursor)
		if err != nil {
			if ctx.Err() != nil {
				return 0 // cancelled
			}
			fmt.Fprintln(os.Stderr, err)
			return 1
		}

		events = contentFreeTaskEvents(events)

		// Find max sequence to update cursor
		for _, e := range events {
			if e.Sequence > cursor {
				cursor = e.Sequence
			}
		}

		if *jsonl {
			enc := json.NewEncoder(os.Stdout)
			for _, e := range events {
				if err := enc.Encode(e); err != nil {
					return 1
				}
			}
		} else {
			// --json: output as JSON array
			output := struct {
				SchemaVersion int                     `json:"schema_version"`
				TaskID        string                  `json:"task_id"`
				Events        []taskmonitor.TaskEvent `json:"events"`
			}{SchemaVersion: 1, TaskID: id, Events: events}
			if events == nil {
				output.Events = []taskmonitor.TaskEvent{}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(output); err != nil {
				return 1
			}
		}

		if !*follow {
			break
		}
		// Check if task has reached a terminal state
		snap, _ := store.GetTask(ctx, *dir, id)
		if snap != nil && snap.State.Terminal() && len(events) == 0 {
			break
		}

		select {
		case <-ctx.Done():
			return 0
		case <-time.After(500 * time.Millisecond):
		}
	}
	return 0
}

// control commands

func taskStopCmd(store taskmonitor.Store, args []string) int {
	fs := flag.NewFlagSet("task stop", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output as JSON")
	dir := fs.String("dir", "", "project directory scope")
	expectedVersion := fs.Uint64("expected-version", 0, "expected task version for CAS")
	reason := fs.String("reason", "", "reason for stopping")
	idemKey := fs.String("idempotency-key", "", "idempotency key")
	if err := fs.Parse(reorderTaskID(fs, args)); err != nil {
		return 2
	}
	if !*jsonOut {
		fmt.Fprintln(os.Stderr, "task stop requires --json")
		return 2
	}
	id := fs.Arg(0)
	if id == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix task stop <id> --expected-version N --json")
		return 2
	}

	ws, ok := store.(taskmonitor.WriteStore)
	if !ok {
		fmt.Fprintln(os.Stderr, "task stop: store does not support writes")
		return 1
	}
	cs := taskmonitor.NewControlService(ws)
	res, err := cs.StopTaskWithKiller(context.Background(), *dir, id, *expectedVersion, *reason, *idemKey, taskJobKiller)
	return outputControlResult(res, err)
}

func taskCancelCmd(store taskmonitor.Store, args []string) int {
	fs := flag.NewFlagSet("task cancel", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output as JSON")
	dir := fs.String("dir", "", "project directory scope")
	expectedVersion := fs.Uint64("expected-version", 0, "expected task version for CAS")
	reason := fs.String("reason", "", "reason for cancelling")
	idemKey := fs.String("idempotency-key", "", "idempotency key")
	if err := fs.Parse(reorderTaskID(fs, args)); err != nil {
		return 2
	}
	if !*jsonOut {
		fmt.Fprintln(os.Stderr, "task cancel requires --json")
		return 2
	}
	id := fs.Arg(0)
	if id == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix task cancel <id> --expected-version N --json")
		return 2
	}

	ws, ok := store.(taskmonitor.WriteStore)
	if !ok {
		fmt.Fprintln(os.Stderr, "task cancel: store does not support writes")
		return 1
	}
	cs := taskmonitor.NewControlService(ws)
	res, err := cs.CancelTaskWithKiller(context.Background(), *dir, id, *expectedVersion, *reason, *idemKey, taskJobKiller)
	return outputControlResult(res, err)
}

func taskRequeueCmd(store taskmonitor.Store, args []string) int {
	fs := flag.NewFlagSet("task requeue", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output as JSON")
	dir := fs.String("dir", "", "project directory scope")
	expectedVersion := fs.Uint64("expected-version", 0, "expected task version for CAS")
	idemKey := fs.String("idempotency-key", "", "idempotency key")
	if err := fs.Parse(reorderTaskID(fs, args)); err != nil {
		return 2
	}
	if !*jsonOut {
		fmt.Fprintln(os.Stderr, "task requeue requires --json")
		return 2
	}
	id := fs.Arg(0)
	if id == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix task requeue <id> --expected-version N --json")
		return 2
	}

	ws, ok := store.(taskmonitor.WriteStore)
	if !ok {
		fmt.Fprintln(os.Stderr, "task requeue: store does not support writes")
		return 1
	}
	cs := taskmonitor.NewControlService(ws)
	res, err := cs.RequeueTask(context.Background(), *dir, id, *expectedVersion, *idemKey)
	return outputControlResult(res, err)
}

func taskOpenSessionCmd(store taskmonitor.Store, args []string) int {
	fs := flag.NewFlagSet("task open-session", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output as JSON")
	dir := fs.String("dir", "", "project directory scope")
	if err := fs.Parse(reorderTaskID(fs, args)); err != nil {
		return 2
	}
	if !*jsonOut {
		fmt.Fprintln(os.Stderr, "task open-session requires --json")
		return 2
	}
	id := fs.Arg(0)
	if id == "" {
		fmt.Fprintln(os.Stderr, "usage: reasonix task open-session <id> --json")
		return 2
	}

	// open-session is read-only — use Store directly
	snap, err := store.GetTask(context.Background(), *dir, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	res := taskmonitor.ControlResult{
		SchemaVersion: 1,
		Command:       "open_session",
		TaskID:        id,
	}
	if snap == nil {
		res.Error = &taskmonitor.CtrlError{Code: taskmonitor.ErrTaskNotFound, Message: "task not found"}
		return outputControlResult(res, nil)
	}
	res.SessionID = snap.SessionID
	res.State = snap.State
	res.Version = snap.Version
	res.Accepted = true
	return outputControlResult(res, nil)
}

func outputControlResult(res taskmonitor.ControlResult, err error) int {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !res.Accepted && !res.Idempotent {
		return 1
	}
	return 0
}
