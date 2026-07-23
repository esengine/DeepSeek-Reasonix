package runtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

func checkpointTurn(id protocol.CheckpointID) (int, error) {
	const prefix = "checkpoint_"
	if !strings.HasPrefix(string(id), prefix) {
		return 0, fmt.Errorf("invalid checkpoint")
	}
	turn, err := strconv.Atoi(strings.TrimPrefix(string(id), prefix))
	if err != nil || turn < 0 {
		return 0, fmt.Errorf("invalid checkpoint")
	}
	return turn, nil
}

func (s *Server) rewindSession(p protocol.SessionRewindParams) (protocol.SessionRewindResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionRewindResult{}, err
	}
	turn, err := checkpointTurn(p.CheckpointID)
	if err != nil {
		return protocol.SessionRewindResult{}, protocol.MustRemoteError(protocol.ErrCheckpointNotFound, protocol.ErrorOptions{Target: &p.Target})
	}
	controller, ok := sess.ctrl.(interface {
		Rewind(int, control.RewindScope) error
	})
	if !ok {
		return protocol.SessionRewindResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	scope := control.RewindBoth
	switch p.Scope {
	case protocol.RewindCode:
		scope = control.RewindCode
	case protocol.RewindConversation:
		scope = control.RewindConversation
	}
	if err := controller.Rewind(turn, scope); err != nil {
		return protocol.SessionRewindResult{}, protocol.MustRemoteError(protocol.ErrCheckpointScopeUnavailable, protocol.ErrorOptions{Target: &p.Target})
	}
	s.mu.Lock()
	sess.updatedAt = time.Now().UnixMilli()
	s.mu.Unlock()
	if err := s.persistSessionRegistry(); err != nil {
		// Rewind has already restored files and/or rewritten the transcript. It
		// cannot be rolled back safely, so keep the successful result authoritative
		// and let a later registry write persist the metadata timestamp.
		s.logRegistryError("persist committed rewind", err)
	}
	s.notifyStateChanged(sess.id)
	return protocol.SessionRewindResult{
		WorkspaceChanged:      p.Scope == protocol.RewindCode || p.Scope == protocol.RewindBoth,
		ConversationRewritten: p.Scope == protocol.RewindConversation || p.Scope == protocol.RewindBoth,
		SnapshotRequired:      true,
	}, nil
}

func (s *Server) forkSession(ctx context.Context, p protocol.SessionForkParams) (protocol.SessionForkResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionForkResult{}, err
	}
	turn, err := checkpointTurn(p.CheckpointID)
	if err != nil {
		return protocol.SessionForkResult{}, protocol.MustRemoteError(protocol.ErrCheckpointNotFound, protocol.ErrorOptions{Target: &p.Target})
	}
	controller, ok := sess.ctrl.(interface {
		ForkSession(int, string) (string, error)
	})
	if !ok {
		return protocol.SessionForkResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	path, err := controller.ForkSession(turn, p.Name)
	if err != nil {
		return protocol.SessionForkResult{}, protocol.MustRemoteError(protocol.ErrCheckpointScopeUnavailable, protocol.ErrorOptions{Target: &p.Target})
	}
	path, err = containedSessionPath(s.sessionDir(), path)
	if err != nil {
		return protocol.SessionForkResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	leases := control.NewSessionLeaseKeeper()
	if err := leases.Rebind(path); err != nil {
		leases.Release()
		return protocol.SessionForkResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	forkCommitted := false
	defer func() {
		if forkCommitted {
			return
		}
		leases.Release()
		if cleanupErr := control.RemoveSessionArtifacts(path); cleanupErr != nil {
			s.logRegistryError("clean failed fork", cleanupErr)
		}
	}()
	loaded, err := agent.LoadSession(path)
	if err != nil {
		return protocol.SessionForkResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	childID := protocol.SessionID("session_" + randomHex(12))
	sink := &sessionSink{server: s, sessionID: childID}
	effort := sess.effort
	childCtrl, err := s.buildController(ctx, sess.model, &effort, sink, sess.tokenMode)
	if err != nil || childCtrl == nil {
		return protocol.SessionForkResult{}, protocol.MustRemoteError(protocol.ErrRuntimeStartFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	childCtrl.AdoptHistory(loaded.Messages, path)
	applyControllerProfile(childCtrl, sess.collaboration, sess.toolApproval)
	now := time.Now().UnixMilli()
	child := &session{
		id: childID, ctrl: childCtrl, leases: leases, model: childCtrl.ModelRef(), effort: sess.effort,
		collaboration: sess.collaboration, tokenMode: sess.tokenMode, toolApproval: sess.toolApproval,
		topicID: protocol.TopicID("topic_" + randomHex(10)), title: strings.TrimSpace(p.Name),
		runtimeEpoch: protocol.RuntimeEpoch("runtime_" + randomHex(12)), createdAt: now, updatedAt: now, sink: sink,
	}
	if child.title == "" {
		child.title = sess.title + " (fork)"
	}
	s.mu.Lock()
	if s.sessions[sess.id] != sess {
		s.mu.Unlock()
		closeRuntimeSession(child)
		return protocol.SessionForkResult{}, protocol.MustRemoteError(protocol.ErrSessionNotFound, protocol.ErrorOptions{})
	}
	s.sessions[childID] = child
	s.mu.Unlock()
	if err := s.persistSessionRegistry(); err != nil {
		s.mu.Lock()
		if s.sessions[childID] == child {
			delete(s.sessions, childID)
		}
		s.mu.Unlock()
		closeRuntimeSession(child)
		return protocol.SessionForkResult{}, protocol.MustRemoteError(protocol.ErrSessionPersistFailed, protocol.ErrorOptions{Target: &p.Target})
	}
	forkCommitted = true
	return protocol.SessionForkResult{
		SourceTarget: p.Target, SourceRuntimeEpoch: sess.runtimeEpoch,
		ChildTarget: s.target(childID), ChildRuntimeEpoch: child.runtimeEpoch,
	}, nil
}

func (s *Server) summarizeSession(p protocol.SessionSummarizeParams) (protocol.OperationStartedResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.OperationStartedResult{}, err
	}
	turn, err := checkpointTurn(p.CheckpointID)
	if err != nil {
		return protocol.OperationStartedResult{}, protocol.MustRemoteError(protocol.ErrCheckpointNotFound, protocol.ErrorOptions{Target: &p.Target})
	}
	controller, ok := sess.ctrl.(interface {
		SummarizeFrom(context.Context, int) error
		SummarizeUpTo(context.Context, int) error
	})
	if !ok {
		return protocol.OperationStartedResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	operationID := protocol.OperationID("operation_" + randomHex(10))
	opCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if sess.currentOp != nil || sess.ctrl.Running() {
		s.mu.Unlock()
		cancel()
		return protocol.OperationStartedResult{}, protocol.MustRemoteError(protocol.ErrSessionBusy, protocol.ErrorOptions{Target: &p.Target})
	}
	sess.currentOp = &protocol.OperationState{OperationID: operationID, Kind: protocol.OperationSummarize}
	sess.operationStop = cancel
	s.mu.Unlock()
	go func() {
		if p.Direction == protocol.SummaryFrom {
			_ = controller.SummarizeFrom(opCtx, turn)
		} else {
			_ = controller.SummarizeUpTo(opCtx, turn)
		}
		s.finishOperation(sess.id, operationID)
	}()
	return protocol.OperationStartedResult{OperationID: operationID, Disposition: "started"}, nil
}

func (s *Server) cancelOperation(p protocol.OperationCancelParams) (protocol.OperationCancelResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.OperationCancelResult{}, err
	}
	s.mu.Lock()
	if sess.currentOp == nil {
		s.mu.Unlock()
		return protocol.OperationCancelResult{}, protocol.MustRemoteError(protocol.ErrOperationNotActive, protocol.ErrorOptions{Target: &p.Target})
	}
	if sess.currentOp.OperationID != p.ExpectedOperationID {
		actual := sess.currentOp.OperationID
		s.mu.Unlock()
		return protocol.OperationCancelResult{}, protocol.MustRemoteError(protocol.ErrOperationMismatch, protocol.ErrorOptions{Target: &p.Target, Expected: string(p.ExpectedOperationID), Actual: string(actual)})
	}
	status := protocol.CancelRequested
	if sess.currentOp.CancelRequested {
		status = protocol.CancelAlreadyRequested
	}
	sess.currentOp.CancelRequested = true
	stop := sess.operationStop
	s.mu.Unlock()
	if stop != nil {
		stop()
	} else {
		sess.ctrl.Cancel()
	}
	return protocol.OperationCancelResult{Status: status, OperationID: p.ExpectedOperationID}, nil
}

func (s *Server) finishOperation(sessionID protocol.SessionID, operationID protocol.OperationID) {
	s.mu.Lock()
	sess := s.sessions[sessionID]
	if sess == nil || sess.currentOp == nil || sess.currentOp.OperationID != operationID {
		s.mu.Unlock()
		return
	}
	if sess.operationStop != nil {
		sess.operationStop()
	}
	sess.operationStop = nil
	sess.currentOp = nil
	sess.updatedAt = time.Now().UnixMilli()
	s.mu.Unlock()
	go func() {
		s.requestMu.Lock()
		defer s.requestMu.Unlock()
		if err := s.persistSessionRegistry(); err != nil {
			s.logRegistryError("persist completed operation", err)
		}
	}()
	s.notifyStateChanged(sessionID)
}

func (s *Server) notifyStateChanged(sessionID protocol.SessionID) {
	type targetNotification struct {
		conn  *rpcwire.Conn
		value protocol.SessionResyncRequired
	}
	s.mu.Lock()
	sess := s.sessions[sessionID]
	if sess == nil {
		s.mu.Unlock()
		return
	}
	notifications := make([]targetNotification, 0, len(s.subs))
	for _, sub := range s.subs {
		if sub.sessionID != sessionID || !sub.active {
			continue
		}
		notifications = append(notifications, targetNotification{conn: sub.conn, value: protocol.SessionResyncRequired{
			SubscriptionID: sub.id, HostEpoch: s.hostEpoch, Target: s.target(sessionID),
			RuntimeEpoch: sess.runtimeEpoch, LastSeq: sub.seq, Reason: protocol.ResyncStateChanged,
		}})
	}
	s.mu.Unlock()
	for _, notification := range notifications {
		_ = notification.conn.Notify(string(protocol.MethodSessionResyncRequired), notification.value)
	}
}

type goalController interface {
	SetGoal(string)
	ResumeGoal() bool
	Goal() string
	GoalStatus() string
}

func protocolGoal(ctrl goalController) (string, protocol.GoalStatus) {
	goal := strings.TrimSpace(ctrl.Goal())
	status := protocol.GoalStatus(ctrl.GoalStatus())
	switch status {
	case protocol.GoalRunning, protocol.GoalComplete, protocol.GoalBlocked, protocol.GoalStopped:
	default:
		if goal != "" {
			status = protocol.GoalRunning
		} else {
			status = ""
		}
	}
	return goal, status
}

func (s *Server) setGoal(p protocol.SessionGoalSetParams) (protocol.SessionGoalSetResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionGoalSetResult{}, err
	}
	controller, ok := sess.ctrl.(goalController)
	if !ok {
		return protocol.SessionGoalSetResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	controller.SetGoal(p.Goal)
	goal, status := protocolGoal(controller)
	s.notifyStateChanged(sess.id)
	return protocol.SessionGoalSetResult{Goal: goal, Status: status}, nil
}

func (s *Server) resumeGoal(p protocol.SessionGoalResumeParams) (protocol.SessionGoalResumeResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionGoalResumeResult{}, err
	}
	controller, ok := sess.ctrl.(goalController)
	if !ok {
		return protocol.SessionGoalResumeResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	resumed := controller.ResumeGoal()
	goal, status := protocolGoal(controller)
	s.notifyStateChanged(sess.id)
	return protocol.SessionGoalResumeResult{Resumed: resumed, Goal: goal, Status: status}, nil
}

func (s *Server) clearGoal(p protocol.SessionGoalClearParams) (protocol.SessionGoalClearResult, error) {
	sess, err := s.sessionForMutation(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.SessionGoalClearResult{}, err
	}
	controller, ok := sess.ctrl.(goalController)
	if !ok {
		return protocol.SessionGoalClearResult{}, protocol.MustRemoteError(protocol.ErrCapabilityUnavailable, protocol.ErrorOptions{})
	}
	controller.SetGoal("")
	s.notifyStateChanged(sess.id)
	return protocol.SessionGoalClearResult{Cleared: true}, nil
}
