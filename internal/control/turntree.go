package control

import (
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TurnTree builds the session turn-level tree for interactive navigation.
func (c *Controller) TurnTree() (*agent.TurnTree, error) {
	if c.sessionDir == "" {
		return nil, fmt.Errorf("session persistence is disabled")
	}
	c.mu.Lock()
	origin := c.jumpOrigin
	c.mu.Unlock()
	if !origin.active {
		if err := c.Snapshot(); err != nil {
			return nil, err
		}
	}
	tree, err := agent.BuildTurnTree(c.sessionDir, c.SessionPath())
	if err != nil {
		return nil, err
	}
	if origin.active {
		tree.SetCurrent(origin.branchID, origin.turn)
	}
	return tree, nil
}

// jumpOrigin records where the user jumped to, so the next Send can auto-fork.
type jumpOrigin struct {
	branchID     string
	turn         int
	messageIndex int
	endIndex     int
	targetMeta   agent.BranchMeta
	active       bool
}

// JumpToTurn navigates to a historical turn in a unique temporary in-memory
// session. It does not create a branch file. The first real Send or /branch from
// that temporary session materializes the branch at the recorded origin.
func (c *Controller) JumpToTurn(branchID string, turn int) error {
	if c.executor == nil {
		return c.rewindFail(fmt.Errorf("jump unavailable"))
	}
	if c.sessionDir == "" {
		return c.rewindFail(fmt.Errorf("jump needs session persistence, which is disabled"))
	}

	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if running {
		return c.rewindFail(fmt.Errorf("cannot jump while a turn is running"))
	}

	c.mu.Lock()
	previousOrigin := c.jumpOrigin
	wasPreview := previousOrigin.active
	c.mu.Unlock()
	if !wasPreview {
		if err := c.Snapshot(); err != nil {
			return c.rewindFail(err)
		}
	}

	tree, err := agent.BuildTurnTree(c.sessionDir, c.SessionPath())
	if err != nil {
		return c.rewindFail(err)
	}

	node := tree.FindNode(branchID, turn)
	if node == nil {
		return c.rewindFail(fmt.Errorf("turn %d not found in branch %s", turn+1, branchID))
	}

	// If this is the tip of the current persisted branch, just stay there.
	currentID := agent.BranchID(c.SessionPath())
	lastNode := tree.LastInBranch(currentID)
	if branchID == currentID && lastNode != nil && lastNode.Turn == turn {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("already at the tip of turn %d", turn+1)})
		return nil
	}

	// Load the source branch's messages through the selected complete turn. The
	// selected node is therefore stable on every reload; new dialogue starts as a
	// child of that node only if the user sends a message from the preview.
	srcPath := ""
	branches, err := c.Branches()
	if err != nil {
		return c.rewindFail(err)
	}
	for _, b := range branches {
		if b.ID == branchID {
			srcPath = b.Path
			break
		}
	}
	if srcPath == "" {
		return c.rewindFail(fmt.Errorf("branch %s not found", branchID))
	}

	targetMeta := previousOrigin.targetMeta
	if path := c.SessionPath(); path != "" {
		if meta, err := agent.EnsureBranchMeta(path); err == nil {
			targetMeta = meta
		}
	}

	loaded, err := agent.LoadSession(srcPath)
	if err != nil {
		return c.rewindFail(err)
	}

	allMsgs := loaded.Snapshot()
	if node.EndMessageIndex > len(allMsgs) {
		return c.rewindFail(fmt.Errorf("turn boundary beyond message count"))
	}

	forked := append([]provider.Message(nil), allMsgs[:node.EndMessageIndex]...)
	sess := agent.NewSession("")
	sess.Messages = forked

	// Switch to an unsaved temporary preview. Repeated node clicks replace this
	// same preview instead of creating files.
	c.executor.SetSession(sess)
	c.mu.Lock()
	c.sessionPath = ""
	c.jumpOrigin = jumpOrigin{
		branchID:     branchID,
		turn:         turn,
		messageIndex: node.MessageIndex,
		endIndex:     node.EndMessageIndex,
		targetMeta:   targetMeta,
		active:       true,
	}
	c.mu.Unlock()
	c.rebindCheckpoints("")

	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("previewing turn %d; send a message to create a branch", turn+1)})
	return nil
}

func (c *Controller) materializeJumpOrigin(name string) error {
	c.mu.Lock()
	origin := c.jumpOrigin
	if !origin.active {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	if c.executor == nil {
		return fmt.Errorf("jump unavailable")
	}
	if c.sessionDir == "" {
		return fmt.Errorf("jump needs session persistence, which is disabled")
	}

	newPath := agent.NewSessionPath(c.sessionDir, c.label)
	if err := c.executor.Session().Save(newPath); err != nil {
		return err
	}
	meta := agent.BranchMeta{
		Name:             strings.TrimSpace(name),
		ParentID:         origin.branchID,
		ForkTurn:         origin.turn,
		ForkMessageIndex: origin.endIndex,
	}
	if source, ok := c.branchMetaForID(origin.branchID); ok {
		meta.Scope = source.Scope
		meta.WorkspaceRoot = source.WorkspaceRoot
		meta.TopicID = source.TopicID
		meta.TopicTitle = source.TopicTitle
	}
	if strings.TrimSpace(origin.targetMeta.TopicID) != "" {
		meta.Scope = origin.targetMeta.Scope
		meta.WorkspaceRoot = origin.targetMeta.WorkspaceRoot
		meta.TopicID = origin.targetMeta.TopicID
		meta.TopicTitle = origin.targetMeta.TopicTitle
	}
	if err := agent.SaveBranchMeta(newPath, meta); err != nil {
		return err
	}

	c.mu.Lock()
	c.sessionPath = newPath
	c.jumpOrigin = jumpOrigin{}
	c.mu.Unlock()
	c.rebindCheckpoints(newPath)
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("created branch from turn %d → %s", origin.turn+1, agent.BranchID(newPath))})
	return nil
}

// MaterializeJumpPreview persists the current temporary turn-tree preview, when
// one is active, so a newly opened topic can be restored after app restart even
// before the user sends another message.
func (c *Controller) MaterializeJumpPreview() error {
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if running {
		return c.rewindFail(fmt.Errorf("cannot materialize turn preview while a turn is running"))
	}
	return c.materializeJumpOrigin("")
}

func (c *Controller) branchMetaForID(branchID string) (agent.BranchMeta, bool) {
	branches, err := agent.ListBranches(c.sessionDir)
	if err != nil {
		return agent.BranchMeta{}, false
	}
	for _, b := range branches {
		if b.ID == branchID {
			return b.BranchMeta, true
		}
	}
	return agent.BranchMeta{}, false
}

func (c *Controller) clearJumpPreviewLocked() {
	c.jumpOrigin = jumpOrigin{}
	c.pendingSessionDisplays = nil
}

// ClearJumpOrigin resets the jump state.
func (c *Controller) ClearJumpOrigin() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clearJumpPreviewLocked()
}
