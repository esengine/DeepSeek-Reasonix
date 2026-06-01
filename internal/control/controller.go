// Package control is the transport-agnostic session driver. A Controller owns
// the agent run loop and session lifecycle, takes commands (Send/Cancel/Approve/
// SetPlanMode/Compact/NewSession/…), and emits everything that happens —
// reasoning, tool calls, approvals, turn completion — as a typed event stream to
// a single event.Sink.
//
// The point is one orchestration layer behind every frontend: a terminal TUI, a
// desktop webview, or an HTTP/SSE server each drive the Controller identically
// (issue commands, render events) and none of them re-implement turn lifecycle,
// cancellation, or approval. The Controller depends on no frontend.
package control

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"reasonix/internal/agent"
	"reasonix/internal/billing"
	"reasonix/internal/checkpoint"
	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/hook"
	"reasonix/internal/jobs"
	"reasonix/internal/memory"
	"reasonix/internal/permission"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// Controller drives one chat session. Construct with New; drive with the command
// methods; observe through the Sink passed in Options.
type Controller struct {
	runner   agent.Runner
	executor *agent.Agent
	sink     event.Sink
	policy   permission.Policy

	label        string
	systemPrompt string
	sessionDir   string
	host         *plugin.Host
	commands     []command.Command
	skills       []skill.Skill
	hooks        *hook.Runner // session hook runner; nil-safe (no hooks configured)
	mem          *memory.Set
	cleanup      func()
	startedOnce  bool // guards the one-shot SessionStart hook on first turn

	// balanceURL/balanceKey target the active provider's optional wallet-balance
	// endpoint (empty when the provider declares none). Captured at build so a
	// model/key switch — which rebuilds the controller — refreshes them.
	balanceURL string
	balanceKey string

	// jobs is the session-scoped background-job manager. The agent's background
	// tools spawn into it; Compose drains its completion notes into the next turn;
	// Close cancels its still-running jobs.
	jobs *jobs.Manager

	// reg is the live tool registry the executor reads each turn; pluginCtx is the
	// session-scoped context a hot-added stdio server binds its subprocess to.
	// Together they let AddMCPServer connect a server mid-session and have its tools
	// available on the next turn (see AddMCPServer / RemoveMCPServer).
	reg       *tool.Registry
	pluginCtx context.Context

	// Checkpoints (snapshot-based rewind). cp is the per-session store rebound when
	// the session path changes; cpRoot is the workspace root used to guard restore
	// writes. cpTurn is the monotonic turn counter (decoupled from the store so it
	// never collides after a restructure); cpBound[turn] records len(Session.Messages)
	// at that turn's start — the truncation boundary for a conversation rewind/fork.
	// Boundaries are persisted in each checkpoint and rebuilt from the store on
	// resume (so a reopened session can still rewind conversation / fork), but
	// dropped after a summarize restructures the log so those operations report
	// "unavailable" rather than mis-truncating; code rewind (file-based) is unaffected.
	cp      *checkpoint.Store
	cpRoot  string
	cpTurn  int
	cpBound map[int]int

	// promptMu serialises approval prompts so at most one is outstanding at a
	// time (parallel read-only tool calls don't normally gate, writers run
	// serially — but this keeps the contract explicit). Held across the blocking
	// wait, so it must never be taken by the Approve command path.
	promptMu sync.Mutex

	// mu guards the run state and approval bookkeeping; every critical section
	// under it is short and non-blocking.
	mu          sync.Mutex
	cancel      context.CancelFunc
	running     bool
	planMode    bool
	sessionPath string
	approvals   map[string]chan approvalReply
	asks        map[string]chan []event.AskAnswer
	granted     map[string]bool
	nextID      int
	// turn counts model turns this session, passed to hooks in their payload.
	turn int
	// autoApprove auto-allows writer tool calls without prompting. Set only while
	// executing a just-approved plan: approving the plan is the go-ahead, so the
	// model shouldn't re-prompt for every write of the work it just got cleared to
	// do. Deny rules still bite (those never reach the approver). Reset when the
	// execution turn returns.
	autoApprove bool

	// bypass is "YOLO" mode: while set, every approval prompt is auto-allowed for
	// the rest of the session (writers and bash run without asking). It is a
	// deliberate, session-scoped opt-in (the --dangerously-skip-permissions flag or
	// a runtime toggle), never persisted. Deny rules are unaffected — they're
	// resolved before the approver, so a denied tool is still blocked in YOLO mode.
	bypass bool

	// pendingMemory holds memory notes added mid-session (via "#" quick-add or a
	// memory edit) that haven't yet been folded into a turn. Compose drains it
	// onto the next outgoing turn — never into the cache-stable system prefix — so
	// a fresh memory takes effect this session without busting the prompt cache;
	// it joins the prefix naturally on the next session.
	pendingMemory []string
}

type approvalReply struct {
	allow   bool
	session bool
}

// Options carries the already-built pieces setup assembles. Lifecycle metadata
// lets the controller mint and rotate session files; Host/Commands are surfaced
// to frontends that resolve MCP prompts and slash commands.
type Options struct {
	Runner       agent.Runner
	Executor     *agent.Agent
	Sink         event.Sink
	Policy       permission.Policy
	Label        string
	SystemPrompt string
	SessionDir   string
	SessionPath  string
	Host         *plugin.Host
	Commands     []command.Command
	Skills       []skill.Skill
	Hooks        *hook.Runner
	Memory       *memory.Set
	Cleanup      func()
	// BalanceURL/BalanceKey wire the active provider's optional wallet-balance
	// endpoint and bearer key; empty when the provider declares no balance_url.
	BalanceURL string
	BalanceKey string
	// Jobs is the session-scoped background-job manager (nil disables background jobs).
	Jobs *jobs.Manager
	// Registry is the executor's live tool set, and PluginCtx the session-scoped
	// context; both are needed for hot-adding MCP servers via AddMCPServer.
	Registry  *tool.Registry
	PluginCtx context.Context
	// WorkspaceRoot is the project root checkpoint restores are confined to ("" =
	// no confinement). Frontends pass the cwd they launched the session in.
	WorkspaceRoot string
}

// New builds a Controller. A nil Sink is replaced with event.Discard.
func New(opts Options) *Controller {
	sink := opts.Sink
	if sink == nil {
		sink = event.Discard
	}
	pluginCtx := opts.PluginCtx
	if pluginCtx == nil {
		pluginCtx = context.Background()
	}
	c := &Controller{
		runner:       opts.Runner,
		executor:     opts.Executor,
		sink:         sink,
		policy:       opts.Policy,
		label:        opts.Label,
		systemPrompt: opts.SystemPrompt,
		sessionDir:   opts.SessionDir,
		sessionPath:  opts.SessionPath,
		host:         opts.Host,
		commands:     opts.Commands,
		skills:       opts.Skills,
		hooks:        opts.Hooks,
		mem:          opts.Memory,
		cleanup:      opts.Cleanup,
		balanceURL:   opts.BalanceURL,
		balanceKey:   opts.BalanceKey,
		jobs:         opts.Jobs,
		reg:          opts.Registry,
		pluginCtx:    pluginCtx,
		cpRoot:       opts.WorkspaceRoot,
		approvals:    map[string]chan approvalReply{},
		asks:         map[string]chan []event.AskAnswer{},
		granted:      map[string]bool{},
	}
	// Checkpoints: bind a store to the session and route writer pre-edits into it.
	c.rebindCheckpoints(opts.SessionPath)
	if c.executor != nil {
		c.executor.SetPreEditHook(func(ch diff.Change) {
			if c.cp != nil {
				c.cp.Snapshot(ch)
			}
		})
	}
	return c
}

// ckptDir derives a session's checkpoint directory from its file path
// (…/<id>.jsonl → …/<id>.ckpt). Empty path → empty (in-memory checkpoints).
func ckptDir(sessionPath string) string {
	if sessionPath == "" {
		return ""
	}
	return strings.TrimSuffix(sessionPath, ".jsonl") + ".ckpt"
}

// rebindCheckpoints points the store at the (possibly new) session, loading any
// checkpoints already on disk, and resets the turn boundaries. Called on
// construction and whenever the session path changes (NewSession/Resume/SetSessionPath).
func (c *Controller) rebindCheckpoints(sessionPath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cp = checkpoint.New(ckptDir(sessionPath), c.cpRoot)
	c.cpTurn = c.cp.NextTurn() // continue numbering past any checkpoints on disk
	c.cpBound = c.cp.Bounds()  // rebuilt from persisted checkpoints so a resumed
	if c.cpBound == nil {      // session can still rewind conversation / fork
		c.cpBound = map[int]int{}
	}
}

// beginCheckpoint opens a checkpoint for the turn about to run, recording the
// current message count as the conversation-rewind boundary. Called at the top of
// runTurn, before the user message is appended.
func (c *Controller) beginCheckpoint(input string) {
	if c.cp == nil || c.executor == nil {
		return
	}
	c.mu.Lock()
	turn := c.cpTurn
	c.cpTurn++
	msgIndex := len(c.executor.Session().Messages)
	c.cpBound[turn] = msgIndex
	c.mu.Unlock()
	c.cp.Begin(turn, input, msgIndex)
}

// --- commands (frontend → controller) ---

// runGuarded runs body on a background goroutine under a fresh cancellable
// context, guarding against concurrent turns and emitting a TurnDone event when
// it finishes (Err set on failure; nil also for a user Cancel). A no-op if a
// turn is already in flight.
func (c *Controller) runGuarded(body func(ctx context.Context) error) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.running = true
	c.mu.Unlock()

	go func() {
		err := body(ctx)
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.mu.Unlock()
		c.sink.Emit(event.Event{Kind: event.TurnDone, Err: err})
	}()
}

// Send starts a turn with an already-composed message (the caller applied any
// plan-mode marker and @-ref expansion). Used by the chat TUI, which resolves
// those itself for live UI feedback.
func (c *Controller) Send(input string) {
	c.runGuarded(func(ctx context.Context) error { return c.runTurn(ctx, input) })
}

// planApprovalTool is the Tool name on the ApprovalRequest the controller emits
// to gate a proposed plan. Frontends key their plan-approval UI on it (the
// desktop renders a plan card; the chat TUI a plan banner).
const planApprovalTool = "exit_plan_mode"

// planApprovedMessage is the follow-up turn sent once the user approves a plan —
// the in-context nudge to execute and keep the (already-seeded) task list honest.
const planApprovedMessage = "Plan approved — plan mode is off; you're cleared to make the changes without asking again. Implement the plan now. Keep the task list current with todo_write, preserving its two-level shape (phases at level 0, their sub-steps at level 1): mark the sub-step you start as in_progress, one in_progress at a time. Sign off each finished sub-step with complete_step, attaching the evidence it's done — the verification you ran, the diff/files you changed, or a manual check. Don't claim a step is done without evidence."

// runTurn runs one model turn, then applies the plan-approval gate. This is the
// single, frontend-agnostic plan flow: in plan mode the model just researches
// (writers are blocked) and writes its plan as a normal answer — no special tool.
// When the turn ends with a text proposal, the controller asks the user to
// approve (reusing the ApprovalRequest channel both frontends already render);
// on approval it exits plan mode, seeds the task list from the plan, and
// continues straight into execution; on rejection it stays in plan mode so the
// next turn can revise. Plan mode is only ever set interactively, so the headless
// `Run` path (which doesn't call this) never blocks on a prompt.
func (c *Controller) runTurn(ctx context.Context, input string) error {
	c.maybeSessionStart(ctx)
	// Open a checkpoint for this turn before the user message is appended, so the
	// recorded message boundary precedes it and pre-edit snapshots land here.
	c.beginCheckpoint(input)
	// UserPromptSubmit / Stop hooks bracket the whole turn (incl. the plan
	// research + approved-execution sub-turns below): a gating UserPromptSubmit
	// aborts before any model call; Stop fires once when the turn returns.
	if c.hooks.Enabled() {
		c.mu.Lock()
		c.turn++
		turn := c.turn
		c.mu.Unlock()
		if block, _ := c.hooks.PromptSubmit(ctx, input, turn); block {
			return nil // the hook's notify callback already surfaced the reason
		}
		defer func() { c.hooks.Stop(ctx, lastAssistantText(c.History()), turn) }()
	}
	if err := c.runner.Run(ctx, input); err != nil {
		return err
	}
	c.mu.Lock()
	plan := c.planMode
	c.mu.Unlock()
	if !plan {
		return nil
	}
	proposal := lastAssistantText(c.History())
	if proposal == "" {
		return nil // no substantive proposal to gate
	}
	// The plan is already visible as the assistant's answer, so the request
	// carries no subject — it's purely the gate.
	allow, _, err := c.requestApproval(ctx, planApprovalTool, "")
	if err != nil {
		return err
	}
	if !allow {
		return nil // keep planning; plan mode stays on
	}
	c.SetPlanMode(false)
	c.seedPlanTodos(proposal)
	// The plan is the go-ahead: don't re-prompt for each write of the approved
	// work. Auto-approve writers for the duration of this execution turn only.
	c.mu.Lock()
	c.autoApprove = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.autoApprove = false
		c.mu.Unlock()
	}()
	return c.runner.Run(ctx, planApprovedMessage)
}

// lastAssistantText returns the content of the most recent assistant message with
// non-empty text — the model's final answer for the turn (its plan, in plan mode).
func lastAssistantText(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleAssistant && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content
		}
	}
	return ""
}

// Submit is the one-call entry for a simple frontend: it takes raw user input
// and does everything — slash-command dispatch, @-reference expansion, plan-mode
// composition — emitting all output as events. The HTTP/SSE server uses this so
// a browser client only POSTs the typed line.
//
// Slash commands route to the matching primitive: /compact and /new run their
// session op and emit a Notice; /mcp__server__prompt and custom /commands
// resolve to a turn; an unknown slash emits a Notice. Anything else is a normal
// turn with its @-references resolved first.
func (c *Controller) Submit(input string) {
	trimmed := strings.TrimSpace(input)
	switch {
	case trimmed == "/compact" || strings.HasPrefix(trimmed, "/compact "):
		focus := strings.TrimSpace(strings.TrimPrefix(trimmed, "/compact"))
		go func() {
			if err := c.Compact(context.Background(), focus); err != nil {
				c.notice("compaction failed: " + err.Error())
			} else {
				c.notice("compacted")
				if err := c.Snapshot(); err != nil {
					slog.Warn("controller: snapshot after compact", "err", err)
				}
			}
		}()
	case trimmed == "/new":
		go func() {
			if err := c.NewSession(); err != nil {
				c.notice("new session failed: " + err.Error())
			} else {
				c.notice("new session")
			}
		}()
	case strings.HasPrefix(trimmed, "#"):
		// "#<note>" quick-adds a memory line — same shortcut as the chat TUI, so
		// the desktop and HTTP frontends (which route raw input through Submit)
		// get it for free. It never starts a model turn.
		note := strings.TrimSpace(trimmed[1:])
		if note == "" {
			c.notice("nothing to remember")
			return
		}
		if path, err := c.QuickAdd(memory.ScopeProject, note); err != nil {
			c.notice("memory: " + err.Error())
		} else {
			c.notice("remembered → " + path)
		}
	case strings.HasPrefix(trimmed, "/mcp__"):
		c.runGuarded(func(ctx context.Context) error {
			sent, found, err := c.MCPPrompt(ctx, trimmed)
			if err != nil {
				return err
			}
			if !found {
				c.notice("unknown command: " + trimmed)
				return nil
			}
			return c.runner.Run(ctx, c.Compose(sent))
		})
	case strings.HasPrefix(trimmed, "/"):
		// Read-only management verbs (/model /memory /skill /hooks /mcp) emit a
		// listing Notice, so Submit-based frontends (desktop, HTTP) get them with
		// no extra wiring. (The chat TUI handles these itself with richer output.)
		fields := strings.Fields(trimmed)
		switch fields[0] {
		case "/tree":
			c.notice(c.BranchTreeText())
			return
		case "/branch":
			args := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
			if turn, name, fromTurn, err := ParseBranchTarget(args); err != nil {
				c.notice(err.Error())
			} else if fromTurn {
				_, _ = c.ForkNamed(turn-1, name)
			} else {
				_, _ = c.Branch(name)
			}
			return
		case "/switch":
			ref := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
			_, _ = c.SwitchBranch(ref)
			return
		}
		if c.managementNotice(trimmed) {
			return
		}
		// A custom command wins over a skill of the same name; both resolve to a
		// turn. (Built-in slash verbs like /compact are handled above.)
		if sent, ok := c.CustomCommand(trimmed); ok {
			c.runGuarded(func(ctx context.Context) error {
				return c.runTurn(ctx, c.Compose(sent))
			})
			return
		}
		if sent, ok := c.RunSkill(trimmed); ok {
			c.runGuarded(func(ctx context.Context) error {
				return c.runTurn(ctx, c.Compose(sent))
			})
			return
		}
		c.notice("unknown command: " + trimmed)
	default:
		c.runGuarded(func(ctx context.Context) error {
			block, errs := c.ResolveRefs(ctx, input)
			for _, e := range errs {
				c.notice(e)
			}
			sent := input
			if block != "" {
				sent = "Referenced context:\n\n" + block + "\n\n" + input
			}
			return c.runTurn(ctx, c.Compose(sent))
		})
	}
}

// notice emits an informational Notice event.
func (c *Controller) notice(text string) {
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text})
}

// Run executes a turn synchronously, returning the agent's error. Used by the
// headless `reasonix run` path, where the Sink renders to stdout and the caller
// just needs the exit status — no TurnDone event, no cancel bookkeeping.
func (c *Controller) Run(ctx context.Context, input string) error {
	c.maybeSessionStart(ctx)
	if c.hooks.Enabled() {
		c.turn++
		if block, _ := c.hooks.PromptSubmit(ctx, input, c.turn); block {
			return nil
		}
		defer func() { c.hooks.Stop(ctx, lastAssistantText(c.History()), c.turn) }()
	}
	return c.runner.Run(ctx, input)
}

// Cancel aborts the in-flight turn. A goroutine blocked awaiting approval
// unblocks via the cancelled context.
func (c *Controller) Cancel() {
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Running reports whether a turn is currently in flight.
func (c *Controller) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// Approve answers a pending ApprovalRequest by ID: allow runs the call, session
// also remembers a grant for the rest of the session so the same tool+subject
// isn't re-prompted. Unknown/expired IDs are ignored.
func (c *Controller) Approve(id string, allow, session bool) {
	c.mu.Lock()
	reply := c.approvals[id]
	delete(c.approvals, id)
	c.mu.Unlock()
	if reply != nil {
		reply <- approvalReply{allow: allow, session: session} // buffered, never blocks
	}
}

// EnableInteractiveApproval swaps the executor's gate for one that routes "ask"
// decisions to the frontend via ApprovalRequest events, and wires the controller
// in as the executor's Asker so the `ask` tool can question the user. Interactive
// frontends (chat, desktop) call this; the headless run keeps the silent gate and
// a nil asker from setup.
func (c *Controller) EnableInteractiveApproval() {
	if c.executor != nil {
		c.executor.SetGate(permission.NewGate(c.policy, gateApprover{c}))
		c.executor.SetAsker(c)
	}
}

// Ask implements agent.Asker: it emits an AskRequest and blocks until
// AnswerQuestion(ID, …) answers or ctx is cancelled. promptMu serialises it
// against tool-approval prompts so at most one user prompt is outstanding.
func (c *Controller) Ask(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error) {
	c.promptMu.Lock()
	defer c.promptMu.Unlock()

	c.mu.Lock()
	c.nextID++
	id := strconv.Itoa(c.nextID)
	reply := make(chan []event.AskAnswer, 1)
	c.asks[id] = reply
	c.mu.Unlock()

	c.sink.Emit(event.Event{Kind: event.AskRequest, Ask: event.Ask{ID: id, Questions: questions}})

	select {
	case ans := <-reply:
		return ans, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.asks, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// AnswerQuestion resolves a pending AskRequest by ID with the user's selections.
// Unknown/expired IDs are ignored.
func (c *Controller) AnswerQuestion(id string, answers []event.AskAnswer) {
	c.mu.Lock()
	reply := c.asks[id]
	delete(c.asks, id)
	c.mu.Unlock()
	if reply != nil {
		reply <- answers // buffered, never blocks
	}
}

// SetPlanMode flips the executor's read-only gate without touching the
// cache-stable prompt prefix, and remembers the state so Compose can prepend the
// plan-mode marker to outgoing turns.
func (c *Controller) SetPlanMode(v bool) {
	c.mu.Lock()
	c.planMode = v
	c.mu.Unlock()
	if c.executor != nil {
		c.executor.SetPlanMode(v)
	}
}

// Compact runs one compaction pass on the executor's session on demand.
// instructions is optional `/compact <focus>` guidance steering what to keep.
func (c *Controller) Compact(ctx context.Context, instructions string) error {
	if c.executor == nil {
		return nil
	}
	return c.executor.CompactNow(ctx, instructions)
}

// maybeSessionStart fires the SessionStart hook exactly once per session, lazily
// on the first turn — by then the sink/notify is wired, and a resumed session
// fires it too (its first post-resume turn).
func (c *Controller) maybeSessionStart(ctx context.Context) {
	c.mu.Lock()
	if c.startedOnce {
		c.mu.Unlock()
		return
	}
	c.startedOnce = true
	c.mu.Unlock()
	c.hooks.SessionStart(ctx)
}

// NewSession snapshots the current conversation, rotates to a fresh file, and
// resets the executor to a clean session carrying the same system prompt. It
// ends the old session and starts the new one for lifecycle hooks.
func (c *Controller) NewSession() error {
	if c.executor == nil {
		return nil
	}
	if err := c.Snapshot(); err != nil {
		return err
	}
	c.hooks.SessionEnd(context.Background())
	if c.sessionDir != "" {
		c.mu.Lock()
		c.sessionPath = agent.NewSessionPath(c.sessionDir, c.label)
		c.mu.Unlock()
	}
	c.executor.SetSession(agent.NewSession(c.systemPrompt))
	c.rebindCheckpoints(c.SessionPath())
	c.mu.Lock()
	c.startedOnce = true // NewSession fires SessionStart itself; don't re-fire on the next turn
	c.mu.Unlock()
	c.hooks.SessionStart(context.Background())
	return nil
}

// RewindScope selects what a Rewind restores.
type RewindScope int

const (
	RewindCode         RewindScope = iota // files only
	RewindConversation                    // message log only
	RewindBoth                            // both
)

// Checkpoints lists the session's rewind points (one per user turn), oldest first.
func (c *Controller) Checkpoints() []checkpoint.Meta {
	if c.cp == nil {
		return nil
	}
	return c.cp.List()
}

// rewindFail emits the error as a Warn notice (so a frontend that swallows the
// returned error — e.g. the desktop bridge's .catch — still shows the user why
// the rewind did nothing) and returns it.
func (c *Controller) rewindFail(err error) error {
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: err.Error()})
	return err
}

// Rewind restores the session to the start of `turn`: Code reverts every file that
// turn (or a later one) changed to its pre-turn content; Conversation truncates the
// message log back to that turn; Both does both. Refused while a turn is running.
// Conversation rewind relies on the live boundary recorded at turn start, so it is
// unavailable for turns inherited from a resumed session (code rewind still works).
// Frontends re-render their transcript from History after the call.
func (c *Controller) Rewind(turn int, scope RewindScope) error {
	if c.cp == nil || c.executor == nil {
		return c.rewindFail(fmt.Errorf("checkpoints unavailable"))
	}
	c.mu.Lock()
	running := c.running
	boundary, hasBound := c.cpBound[turn]
	c.mu.Unlock()
	if running {
		return c.rewindFail(fmt.Errorf("cannot rewind while a turn is running"))
	}

	if scope == RewindCode || scope == RewindBoth {
		written, deleted, err := c.cp.RestoreCode(turn)
		if err != nil {
			return c.rewindFail(fmt.Errorf("rewind code: %w", err))
		}
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("rewound code to turn %d — %d file(s) restored, %d removed", turn, len(written), len(deleted))})
	}
	if scope == RewindConversation || scope == RewindBoth {
		if !hasBound {
			return c.rewindFail(fmt.Errorf("conversation rewind unavailable for turn %d (resumed session)", turn))
		}
		s := c.executor.Session()
		if boundary <= len(s.Messages) {
			s.Messages = s.Messages[:boundary]
			c.mu.Lock()
			c.cpTurn = turn // renumber future turns from here; later turns are gone
			for k := range c.cpBound {
				if k >= turn {
					delete(c.cpBound, k)
				}
			}
			c.mu.Unlock()
			if err := c.Snapshot(); err != nil {
				slog.Warn("controller: snapshot after rewind", "err", err)
			}
		}
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("rewound conversation to turn %d", turn)})
	}
	return nil
}

// Fork branches the conversation at the start of turn into a NEW session file,
// preserving the current one as the branch point, and switches to the branch. Code
// is untouched (it's a conversation operation). Like a conversation rewind it needs
// the live boundary, so it is unavailable for resumed-session turns and refused
// while a turn runs. Returns the new session path.
func (c *Controller) Fork(turn int) (string, error) {
	return c.ForkNamed(turn, "")
}

func (c *Controller) ForkNamed(turn int, name string) (string, error) {
	if c.executor == nil {
		return "", c.rewindFail(fmt.Errorf("checkpoints unavailable"))
	}
	if c.sessionDir == "" {
		return "", c.rewindFail(fmt.Errorf("fork needs session persistence, which is disabled"))
	}
	c.mu.Lock()
	running := c.running
	boundary, hasBound := c.cpBound[turn]
	c.mu.Unlock()
	if running {
		return "", c.rewindFail(fmt.Errorf("cannot fork while a turn is running"))
	}
	if !hasBound {
		return "", c.rewindFail(fmt.Errorf("fork unavailable for turn %d (resumed session)", turn))
	}

	// Persist the current conversation first so the branch point survives, then
	// seed a fresh session with the messages up to the fork and switch to it.
	_ = c.Snapshot()
	parentPath := c.SessionPath()
	parentID := agent.BranchID(parentPath)
	src := c.executor.Session().Messages
	if boundary > len(src) {
		boundary = len(src)
	}
	forked := append([]provider.Message(nil), src[:boundary]...)
	sess := agent.NewSession("")
	sess.Messages = forked

	newPath := agent.NewSessionPath(c.sessionDir, c.label)
	if err := sess.Save(newPath); err != nil {
		return "", c.rewindFail(err)
	}
	if err := agent.SaveBranchMeta(newPath, agent.BranchMeta{
		Name:             strings.TrimSpace(name),
		ParentID:         parentID,
		ForkTurn:         turn,
		ForkMessageIndex: boundary,
	}); err != nil {
		return "", c.rewindFail(err)
	}
	c.executor.SetSession(sess)
	c.mu.Lock()
	c.sessionPath = newPath
	c.mu.Unlock()
	c.rebindCheckpoints(newPath)
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("forked conversation at turn %d into a new session", turn)})
	return newPath, nil
}

// Branch copies the current conversation into a child branch and switches to it.
// Unlike Fork, it branches at the current tip and does not require a checkpoint.
func (c *Controller) Branch(name string) (string, error) {
	if c.executor == nil {
		return "", c.rewindFail(fmt.Errorf("branch unavailable"))
	}
	if c.sessionDir == "" {
		return "", c.rewindFail(fmt.Errorf("branch needs session persistence, which is disabled"))
	}
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if running {
		return "", c.rewindFail(fmt.Errorf("cannot branch while a turn is running"))
	}
	if !c.executor.Session().HasContent() {
		return "", c.rewindFail(fmt.Errorf("nothing to branch yet"))
	}
	if err := c.Snapshot(); err != nil {
		return "", c.rewindFail(err)
	}
	parentPath := c.SessionPath()
	parentID := agent.BranchID(parentPath)
	src := c.executor.Session().Messages
	branched := append([]provider.Message(nil), src...)
	sess := agent.NewSession("")
	sess.Messages = branched

	newPath := agent.NewSessionPath(c.sessionDir, c.label)
	if err := sess.Save(newPath); err != nil {
		return "", c.rewindFail(err)
	}
	if err := agent.SaveBranchMeta(newPath, agent.BranchMeta{
		Name:             strings.TrimSpace(name),
		ParentID:         parentID,
		ForkTurn:         -1,
		ForkMessageIndex: len(branched),
	}); err != nil {
		return "", c.rewindFail(err)
	}
	c.executor.SetSession(sess)
	c.mu.Lock()
	c.sessionPath = newPath
	c.mu.Unlock()
	c.rebindCheckpoints(newPath)
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("created branch %s", agent.BranchID(newPath))})
	return newPath, nil
}

// Branches lists saved conversation branches in this controller's session dir.
func (c *Controller) Branches() ([]agent.BranchInfo, error) {
	if c.sessionDir == "" {
		return nil, fmt.Errorf("session persistence is disabled")
	}
	if err := c.Snapshot(); err != nil {
		return nil, err
	}
	return agent.ListBranches(c.sessionDir)
}

func (c *Controller) SwitchBranch(ref string) (agent.BranchInfo, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return agent.BranchInfo{}, c.rewindFail(fmt.Errorf("usage: /switch <branch id|name>"))
	}
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if running {
		return agent.BranchInfo{}, c.rewindFail(fmt.Errorf("cannot switch branches while a turn is running"))
	}
	branches, err := c.Branches()
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	match, err := resolveBranch(branches, ref)
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	loaded, err := agent.LoadSession(match.Path)
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	if c.executor != nil {
		c.executor.SetSession(loaded)
	}
	c.mu.Lock()
	c.sessionPath = match.Path
	c.mu.Unlock()
	c.rebindCheckpoints(match.Path)
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("switched to branch %s", branchDisplayName(match))})
	return match, nil
}

func resolveBranch(branches []agent.BranchInfo, ref string) (agent.BranchInfo, error) {
	refLower := strings.ToLower(ref)
	var matches []agent.BranchInfo
	for _, b := range branches {
		nameLower := strings.ToLower(strings.TrimSpace(b.Name))
		switch {
		case b.ID == ref || strings.EqualFold(b.ID, ref):
			return b, nil
		case b.Name != "" && nameLower == refLower:
			matches = append(matches, b)
		case strings.HasPrefix(strings.ToLower(b.ID), refLower):
			matches = append(matches, b)
		case b.Path == ref:
			return b, nil
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return agent.BranchInfo{}, fmt.Errorf("branch %q is ambiguous", ref)
	}
	return agent.BranchInfo{}, fmt.Errorf("branch %q not found", ref)
}

func branchDisplayName(b agent.BranchInfo) string {
	if strings.TrimSpace(b.Name) != "" {
		return fmt.Sprintf("%s (%s)", b.Name, b.ID)
	}
	return b.ID
}

// SummarizeFrom compresses the conversation from turn onward into one summary;
// SummarizeUpTo compresses everything before it. Both are Claude Code's "summarize
// from/up to here" — they restructure the message log (keeping code untouched), so
// afterwards the per-turn boundaries no longer map and conversation rewind/fork
// report "unavailable" until new turns rebuild them (code rewind, file-based, is
// unaffected). Refused while a turn runs; need the live boundary.
func (c *Controller) SummarizeFrom(ctx context.Context, turn int) error {
	return c.summarizeAt(ctx, turn, true)
}

func (c *Controller) SummarizeUpTo(ctx context.Context, turn int) error {
	return c.summarizeAt(ctx, turn, false)
}

func (c *Controller) summarizeAt(ctx context.Context, turn int, from bool) error {
	if c.executor == nil {
		return c.rewindFail(fmt.Errorf("checkpoints unavailable"))
	}
	c.mu.Lock()
	running := c.running
	boundary, hasBound := c.cpBound[turn]
	c.mu.Unlock()
	if running {
		return c.rewindFail(fmt.Errorf("cannot summarize while a turn is running"))
	}
	if !hasBound {
		return c.rewindFail(fmt.Errorf("summarize unavailable for turn %d (resumed session)", turn))
	}
	var err error
	if from {
		err = c.executor.SummarizeFrom(ctx, boundary)
	} else {
		err = c.executor.SummarizeUpTo(ctx, boundary)
	}
	if err != nil {
		return c.rewindFail(err)
	}
	// The log was restructured; existing boundaries no longer map. Drop them (keep
	// cpTurn monotonic so new turns don't collide with the store) — conversation
	// rewind degrades to "unavailable" until fresh turns rebuild boundaries.
	c.mu.Lock()
	c.cpBound = map[int]int{}
	c.mu.Unlock()
	_ = c.Snapshot()
	return nil
}

// Resume seeds the session from a loaded transcript and pins the active file to
// its path so auto-save keeps appending there.
func (c *Controller) Resume(s *agent.Session, path string) {
	if c.executor != nil {
		c.executor.SetSession(s)
	}
	c.mu.Lock()
	c.sessionPath = path
	c.mu.Unlock()
	c.rebindCheckpoints(path)
}

// Snapshot writes the executor's conversation to the active session file. No-op
// when persistence is unavailable or the session has never been used (no user
// interaction). Called after every turn so a crash loses at most one in-flight
// prompt.
func (c *Controller) Snapshot() error {
	c.mu.Lock()
	path := c.sessionPath
	c.mu.Unlock()
	if c.executor == nil || path == "" {
		return nil
	}
	s := c.executor.Session()
	if !s.HasContent() {
		return nil
	}
	if err := s.Save(path); err != nil {
		return err
	}
	return agent.TouchBranchMeta(path)
}

// SetSessionPath pins where auto-save lands (a fresh session file minted by the
// caller when no resume path applies).
func (c *Controller) SetSessionPath(p string) {
	c.mu.Lock()
	c.sessionPath = p
	c.mu.Unlock()
	c.rebindCheckpoints(p)
}

// SessionDir reports the directory new session files land in ("" disables
// persistence), so the caller can decide whether to mint a path.
func (c *Controller) SessionDir() string { return c.sessionDir }

// SessionPath reports the file the current conversation auto-saves to ("" when
// persistence is disabled), so a history view can mark the active session.
func (c *Controller) SessionPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionPath
}

// History returns the executor's current message log (for repopulating a
// resumed frontend's view).
func (c *Controller) History() []provider.Message {
	if c.executor == nil {
		return nil
	}
	return c.executor.Session().Messages
}

// ContextSnapshot returns (promptTokens, contextWindow) from the most recent
// turn. Both zero means no data yet — a gauge hides itself.
func (c *Controller) ContextSnapshot() (int, int) {
	if c.executor == nil {
		return 0, 0
	}
	u := c.executor.LastUsage()
	if u == nil {
		return 0, c.executor.ContextWindow()
	}
	return u.PromptTokens, c.executor.ContextWindow()
}

// CompactRatio returns the auto-compaction threshold as a fraction of the window
// (0 when the executor is unset). The status line shows headroom against it.
func (c *Controller) CompactRatio() float64 {
	if c.executor == nil {
		return 0
	}
	return c.executor.CompactRatio()
}

// LastUsage returns the most recent turn's token telemetry (nil before the first
// turn), so frontends can derive the prompt cache-hit rate for the status line.
func (c *Controller) LastUsage() *provider.Usage {
	if c.executor == nil {
		return nil
	}
	return c.executor.LastUsage()
}

// SessionCache returns cumulative cache hit/miss prompt tokens for the session,
// so a frontend can render the aggregate (session-wide) cache-hit rate — steadier
// than the single-turn rate and unaffected by compaction.
func (c *Controller) SessionCache() (hit, miss int) {
	if c.executor == nil {
		return 0, 0
	}
	return c.executor.SessionCache()
}

// Balance queries the active provider's wallet balance, or (nil, nil) when the
// provider declares no balance_url — so a caller treats "not configured" and
// "fetched" the same and just omits the readout when nil.
func (c *Controller) Balance(ctx context.Context) (*billing.Balance, error) {
	if strings.TrimSpace(c.balanceURL) == "" {
		return nil, nil
	}
	return billing.Fetch(ctx, c.balanceURL, c.balanceKey)
}

// Host returns the running MCP host (nil when no plugins), for frontends that
// list servers / resolve MCP prompts.
func (c *Controller) Host() *plugin.Host { return c.host }

// Commands returns the loaded custom slash commands.
func (c *Controller) Commands() []command.Command { return c.commands }

// Skills returns the discoverable skills (for the slash menu and `/skill`).
func (c *Controller) Skills() []skill.Skill { return c.skills }

// HookRunner returns the session's hook runner (nil-safe; may hold zero hooks),
// so a frontend can list the active hooks via `/hooks`.
func (c *Controller) HookRunner() *hook.Runner { return c.hooks }

// AddMCPServer connects an MCP server live and persists it to the config file. Its
// tools are registered immediately and become available on the next turn (the
// agent reads the registry per turn). The raw entry — ${VARS} intact — is what's
// written to disk; the live connection uses the expanded form. Returns the number
// of tools the server exposed. A save failure after a successful connect is
// reported but non-fatal: the server still works this session.
func (c *Controller) AddMCPServer(e config.PluginEntry) (int, error) {
	n, err := c.connectMCPServer(e)
	if err != nil {
		return 0, err
	}
	cfg, lerr := config.Load()
	if lerr != nil {
		return n, fmt.Errorf("connected, but reloading config to save failed: %w", lerr)
	}
	if err := cfg.UpsertPlugin(e); err != nil {
		return n, fmt.Errorf("connected, but config rejected the entry: %w", err)
	}
	if err := cfg.Save(); err != nil {
		return n, fmt.Errorf("connected, but saving config failed: %w", err)
	}
	return n, nil
}

func (c *Controller) connectMCPServer(e config.PluginEntry) (int, error) {
	if c.host == nil {
		c.host = plugin.NewHost()
	}
	exp := e.ExpandedPlugin()
	tools, err := c.host.Add(c.pluginCtx, plugin.Spec{
		Name:    exp.Name,
		Type:    exp.Type,
		Command: exp.Command,
		Args:    exp.Args,
		Env:     exp.Env,
		URL:     exp.URL,
		Headers: exp.Headers,
	})
	if err != nil {
		return 0, err
	}
	if c.reg != nil {
		for _, t := range tools {
			c.reg.Add(t)
		}
	}
	return len(tools), nil
}

func (c *Controller) ConfiguredMCPNames() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Plugins))
	for _, p := range cfg.Plugins {
		names = append(names, p.Name)
	}
	return names
}

func (c *Controller) DisconnectedMCPNames() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	connected := map[string]bool{}
	if c.host != nil {
		for _, name := range c.host.ServerNames() {
			connected[name] = true
		}
	}
	var names []string
	for _, p := range cfg.Plugins {
		if !connected[p.Name] {
			names = append(names, p.Name)
		}
	}
	return names
}

func (c *Controller) ConnectConfiguredMCPServer(name string) (int, error) {
	cfg, err := config.Load()
	if err != nil {
		return 0, err
	}
	for _, p := range cfg.Plugins {
		if p.Name == name {
			return c.connectMCPServer(p)
		}
	}
	return 0, fmt.Errorf("no configured MCP server named %q", name)
}

// RemoveMCPServer disconnects a live MCP server — its tools vanish from the next
// turn — and removes it from the config file. It reports whether a live server was
// disconnected; an error only when the name is neither connected nor in config (or
// the config save fails). A server declared in .mcp.json disconnects for this
// session but returns on the next start, since that file isn't ours to edit.
func (c *Controller) RemoveMCPServer(name string) (disconnected bool, err error) {
	if c.host != nil {
		if prefix, ok := c.host.Remove(name); ok {
			disconnected = true
			if c.reg != nil {
				c.reg.RemovePrefix(prefix)
			}
		}
	}
	cfg, lerr := config.Load()
	if lerr != nil {
		return disconnected, lerr
	}
	inConfig := cfg.RemovePlugin(name)
	if inConfig {
		if serr := cfg.Save(); serr != nil {
			return disconnected, serr
		}
	}
	if !disconnected && !inConfig {
		return false, fmt.Errorf("no MCP server named %q", name)
	}
	return disconnected, nil
}

// Label returns the human-readable model label, e.g. "deepseek-flash".
func (c *Controller) Label() string { return c.label }

// Close stops plugin subprocesses and releases resources. A session that ever
// started fires SessionEnd so a teardown hook runs.
func (c *Controller) Close() {
	c.mu.Lock()
	started := c.startedOnce
	c.mu.Unlock()
	if started {
		c.hooks.SessionEnd(context.Background())
	}
	if c.jobs != nil {
		c.jobs.Close() // cancel any still-running background jobs
	}
	if c.cleanup != nil {
		c.cleanup()
	}
}

// Jobs returns the still-running background jobs for the status bar (nil when
// background jobs are disabled).
func (c *Controller) Jobs() []jobs.View {
	if c.jobs == nil {
		return nil
	}
	return c.jobs.Running()
}

// SetBypass turns YOLO/bypass mode on or off for the session: while on, every
// approval prompt is auto-allowed (writers and bash run without asking). Deny
// rules still block. Runtime-only — never written to config.
func (c *Controller) SetBypass(on bool) {
	c.mu.Lock()
	c.bypass = on
	c.mu.Unlock()
}

// Bypass reports whether YOLO/bypass mode is on, for the status-bar indicator.
func (c *Controller) Bypass() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bypass
}

// --- memory ---
//
// c.mem is treated as an immutable snapshot guarded by c.mu: reads take the lock
// and return the pointer; writes mutate disk then swap in a freshly discovered
// snapshot. A turn-tail note is queued for each write so the change applies this
// session without disturbing the cache-stable system prefix (it folds into the
// prefix on the next session). All of these are no-ops returning "" when memory
// is disabled.

// QuickAdd appends a one-line note to the doc-memory file for scope (project
// REASONIX.md by default) — the write side of "#<note>". Returns the file written.
func (c *Controller) QuickAdd(scope memory.Scope, note string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mem == nil {
		return "", nil
	}
	path := c.mem.DocPath(scope)
	if path == "" {
		return "", fmt.Errorf("no target file for memory scope %q", scope)
	}
	if err := memory.AppendDoc(path, note); err != nil {
		return "", err
	}
	c.pendingMemory = append(c.pendingMemory, note)
	c.refreshMemoryLocked()
	return path, nil
}

// SaveDoc overwrites a recognized memory doc with body — the save side of the
// desktop panel's in-place editor. Returns the file written.
func (c *Controller) SaveDoc(path, body string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mem == nil {
		return "", nil
	}
	written, err := c.mem.WriteDoc(path, body)
	if err != nil {
		return "", err
	}
	// Inject the new content once on the next turn: the cached prefix still holds
	// the pre-edit version this session, so handing the model the current text
	// avoids a stale-guidance gap until the next session re-folds it into the
	// prefix. Trimmed to a single tail note (drained by Compose), not per-turn.
	c.pendingMemory = append(c.pendingMemory,
		"Memory file "+written+" was just edited. Its current contents:\n"+strings.TrimSpace(body))
	c.refreshMemoryLocked()
	return written, nil
}

// Memory returns the loaded memory snapshot (nil when memory is disabled), for
// frontends that surface a memory panel or the /memory command. The returned
// *Set is immutable — mutations go through QuickAdd / SaveDoc.
func (c *Controller) Memory() *memory.Set {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mem
}

// refreshMemoryLocked re-discovers memory from disk so a later Memory() reflects
// a just-applied write. Caller holds c.mu.
func (c *Controller) refreshMemoryLocked() {
	if c.mem == nil {
		return
	}
	c.mem = memory.Load(memory.Options{CWD: c.mem.CWD, UserDir: c.mem.UserDir})
}

// --- approval bridge (agent gate → events) ---

// gateApprover adapts the Controller to permission.Approver. It is distinct
// from the public Approve command (different signature, different direction).
type gateApprover struct{ c *Controller }

func (g gateApprover) Approve(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, error) {
	// Auto-allow without prompting while executing a just-approved plan (the plan
	// was the approval) or while YOLO/bypass mode is on. Deny rules already bit
	// before this point, so they still block.
	g.c.mu.Lock()
	auto := g.c.autoApprove || g.c.bypass
	g.c.mu.Unlock()
	if auto {
		return true, false, nil
	}
	return g.c.requestApproval(ctx, tool, subject)
}

type seedTodo struct {
	Content string `json:"content"`
	Status  string `json:"status"`
	Level   int    `json:"level,omitempty"`
}

// seedPlanTodos turns an approved plan into a starter task list and emits it as a
// synthetic todo_write event, so the live task panel populates the instant the
// user approves — a structural guarantee, not a prompt the model might ignore.
// The model still flips item status as it works (only it knows its own
// progress); this just makes the list exist. No-op when the plan has no list.
func (c *Controller) seedPlanTodos(plan string) {
	args := PlanTodosJSON(plan)
	if args == "" {
		return
	}
	t := event.Tool{ID: "plan-seed", Name: "todo_write", Args: args, ReadOnly: true}
	c.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: t})
	t.Output = "task list seeded from the approved plan"
	c.sink.Emit(event.Event{Kind: event.ToolResult, Tool: t})
}

// PlanTodosJSON parses an approved plan's markdown into todo_write-shaped args
// JSON ({"todos":[...]}), or "" when the plan has no list items. The exit_plan_mode
// path seeds via seedPlanTodos (an event); a frontend whose own approval flow
// bypasses exit_plan_mode (the chat TUI's text-plan approval) calls this directly
// to render the same starter checklist. Shared parsing keeps the two consistent.
func PlanTodosJSON(plan string) string {
	items := parsePlanTodos(plan)
	if len(items) == 0 {
		return ""
	}
	b, err := json.Marshal(map[string]any{"todos": items})
	if err != nil {
		return ""
	}
	return string(b)
}

// parsePlanTodos extracts a starter task list from an approved plan's markdown
// list items (bulleted or numbered): the first is in_progress, the rest pending,
// capped so a long plan can't flood the panel. It understands ONLY markdown lists
// — an unambiguous, standard structure — and deliberately does not guess at prose,
// tables, or arrow sequences (those need brittle, language-specific heuristics).
// The plan-mode marker steers the model to present its plan as a list, so this
// catches the normal case; anything it misses is covered by the model's own
// todo_write calls as it executes.
func parsePlanTodos(plan string) []seedTodo {
	var todos []seedTodo
	for _, raw := range strings.Split(plan, "\n") {
		item, level, ok := listItem(raw)
		if !ok {
			continue
		}
		status := "pending"
		if len(todos) == 0 {
			status = "in_progress"
		}
		todos = append(todos, seedTodo{Content: item, Status: status, Level: level})
		if len(todos) >= 20 {
			break
		}
	}
	return todos
}

// listItem parses a markdown list line ("- x", "* x", "1. x", "2) x") into its
// task text and a nesting level derived from leading indentation (0 for a
// top-level item, 1 for an indented sub-step — capped at 1 since the plan is
// two-level). ok is false when the line isn't a list item. Light inline-markdown
// stripping keeps the checklist readable.
func listItem(line string) (content string, level int, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return "", 0, false
	}
	indent := 0
	for _, c := range line[:len(line)-len(trimmed)] {
		if c == '\t' {
			indent += 4
		} else {
			indent++
		}
	}
	s := trimmed
	// A numbered markdown heading ("### 1. Add the loader") is how models often
	// write a phase even when asked for a list; strip the heading marker and
	// treat it as a top-level phase. A heading without a number (a section
	// title like "## Plan") falls through and is ignored.
	heading := false
	if h := strings.TrimLeft(s, "#"); h != s && strings.HasPrefix(h, " ") {
		heading = true
		s = strings.TrimSpace(h)
	}
	switch {
	case strings.HasPrefix(s, "- "), strings.HasPrefix(s, "* "), strings.HasPrefix(s, "+ "):
		s = s[2:]
	default:
		// numbered: leading digits, then "." or ")", then a space
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 || i+1 >= len(s) || (s[i] != '.' && s[i] != ')') || s[i+1] != ' ' {
			return "", 0, false
		}
		s = s[i+2:]
	}
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[ ] ")
	s = strings.TrimPrefix(s, "[x] ")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, false
	}
	if heading {
		return s, 0, true // a heading is always a top-level phase
	}
	if indent >= 2 {
		return s, 1, true
	}
	return s, 0, true
}

// requestApproval emits an ApprovalRequest and blocks until Approve(ID, …)
// answers or ctx is cancelled. A prior session grant for the same tool+subject
// short-circuits. promptMu serialises outstanding prompts.
func (c *Controller) requestApproval(ctx context.Context, tool, subject string) (bool, bool, error) {
	key := tool + "\x00" + subject

	c.mu.Lock()
	if c.granted[key] {
		c.mu.Unlock()
		return true, false, nil
	}
	c.mu.Unlock()

	c.promptMu.Lock()
	defer c.promptMu.Unlock()

	// Re-check the grant: a session grant may have landed while we queued behind
	// another prompt for the same subject.
	c.mu.Lock()
	if c.granted[key] {
		c.mu.Unlock()
		return true, false, nil
	}
	c.nextID++
	id := strconv.Itoa(c.nextID)
	reply := make(chan approvalReply, 1)
	c.approvals[id] = reply
	c.mu.Unlock()

	c.sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: id, Tool: tool, Subject: subject}})
	// The agent now needs the user's attention; a Notification hook can ping an
	// external channel (desktop notice, phone) while the run blocks on the reply.
	if subject != "" {
		go c.hooks.Notification(ctx, "approval needed: "+tool+" "+subject)
	} else {
		go c.hooks.Notification(ctx, "approval needed: "+tool)
	}

	select {
	case r := <-reply:
		// Plan approvals are one-shot — never persist a session grant for them, or
		// every future plan would auto-approve.
		if r.allow && r.session && tool != planApprovalTool {
			c.mu.Lock()
			c.granted[key] = true
			c.mu.Unlock()
		}
		// remember=false: session grants live here, not in the on-disk policy.
		return r.allow, false, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.approvals, id)
		c.mu.Unlock()
		return false, false, ctx.Err()
	}
}
