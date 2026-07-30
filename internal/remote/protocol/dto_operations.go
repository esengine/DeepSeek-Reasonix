package protocol

type Invocation struct {
	Name string         `json:"name" validate:"nonempty"`
	Kind InvocationKind `json:"kind"`
}

type SessionSubmitParams struct {
	SessionMutation
	Input            string       `json:"input" validate:"nonempty"`
	DisplayText      string       `json:"displayText"`
	EditedOriginal   string       `json:"editedOriginal,omitempty"`
	Invocations      []Invocation `json:"invocations,omitempty"`
	DeliveryRecovery bool         `json:"deliveryRecovery,omitempty"`
}

func (p SessionSubmitParams) Validate() error {
	if p.DeliveryRecovery && (p.EditedOriginal != "" || len(p.Invocations) != 0) {
		return validationError("deliveryRecovery cannot be combined with editedOriginal or invocations")
	}
	if p.EditedOriginal != "" && len(p.Invocations) != 0 {
		return validationError("editedOriginal cannot be combined with invocations")
	}
	return nil
}

type SessionSubmitResult struct {
	Kind             SubmitKind    `json:"kind"`
	TurnID           TurnID        `json:"turnId,omitempty"`
	OperationID      OperationID   `json:"operationId,omitempty"`
	Operation        OperationKind `json:"operation,omitempty"`
	Effect           SubmitEffect  `json:"effect,omitempty"`
	Target           RuntimeTarget `json:"target"`
	RuntimeEpoch     RuntimeEpoch  `json:"runtimeEpoch"`
	SnapshotRequired bool          `json:"snapshotRequired,omitempty"`
}

func (r SessionSubmitResult) Validate() error {
	switch r.Kind {
	case SubmitTurn:
		if r.TurnID == "" || r.OperationID != "" || r.Operation != "" || r.Effect != "" {
			return validationError("turn result requires only turnId")
		}
		if r.SnapshotRequired {
			return validationError("turn result cannot require a snapshot")
		}
	case SubmitOperation:
		if r.OperationID == "" || r.Operation == "" || r.TurnID != "" || r.Effect != "" {
			return validationError("operation result requires operationId and operation")
		}
		if r.SnapshotRequired {
			return validationError("operation result cannot require a snapshot")
		}
	case SubmitCompleted:
		if r.Effect == "" || r.TurnID != "" || r.OperationID != "" || r.Operation != "" {
			return validationError("completed result requires only effect")
		}
		switch r.Effect {
		case EffectNone:
			if r.SnapshotRequired {
				return validationError("completed result with no effect cannot require a snapshot")
			}
		case EffectRuntimeReplaced, EffectSessionReplaced:
			if !r.SnapshotRequired {
				return validationError("runtime or Session replacement must require a snapshot")
			}
		case EffectStateChanged:
			// A state change normally needs no full refresh. Rewind is the
			// frozen exception: it rewrites state in place without changing the
			// runtime epoch and requires the client to subscribe for a snapshot.
		}
	}
	return nil
}

type TurnSteerParams struct {
	SessionMutation
	ExpectedTurnID TurnID `json:"expectedTurnId"`
	Text           string `json:"text" validate:"nonempty"`
}

type TurnSteerResult struct {
	Accepted bool   `json:"accepted" validate:"true"`
	TurnID   TurnID `json:"turnId"`
}

type TurnCancelParams struct {
	SessionMutation
	ExpectedTurnID TurnID `json:"expectedTurnId"`
}

type TurnCancelResult struct {
	Status CancelStatus `json:"status"`
	TurnID TurnID       `json:"turnId"`
}

type PromptApproveParams struct {
	SessionMutation
	PromptID PromptID       `json:"promptId"`
	Decision PromptDecision `json:"decision"`
}

type PromptResolvedResult struct {
	Resolved bool     `json:"resolved" validate:"true"`
	PromptID PromptID `json:"promptId"`
}

type QuestionAnswer struct {
	QuestionID QuestionID `json:"questionId"`
	Selected   []string   `json:"selected"`
}

type PromptAnswerParams struct {
	SessionMutation
	PromptID PromptID         `json:"promptId"`
	Answers  []QuestionAnswer `json:"answers"`
}

func (p PromptAnswerParams) Validate() error {
	seen := make(map[QuestionID]struct{}, len(p.Answers))
	for _, answer := range p.Answers {
		if _, ok := seen[answer.QuestionID]; ok {
			return validationError("questionId must be unique")
		}
		seen[answer.QuestionID] = struct{}{}
	}
	return nil
}

type SessionNewParams struct{ SessionMutation }

type SessionNewResult struct {
	SourceTarget     RuntimeTarget `json:"sourceTarget"`
	Target           RuntimeTarget `json:"target"`
	RuntimeEpoch     RuntimeEpoch  `json:"runtimeEpoch"`
	Disposition      string        `json:"disposition" validate:"const=created"`
	SnapshotRequired bool          `json:"snapshotRequired" validate:"true"`
}

type SessionClearParams struct{ SessionMutation }

type SessionClearDisposition string

const (
	SessionCleared        SessionClearDisposition = "cleared"
	SessionCleanupPending SessionClearDisposition = "cleanup_pending"
)

type SessionClearResult struct {
	PreviousTarget   RuntimeTarget           `json:"previousTarget"`
	Target           RuntimeTarget           `json:"target"`
	RuntimeEpoch     RuntimeEpoch            `json:"runtimeEpoch"`
	Disposition      SessionClearDisposition `json:"disposition"`
	SnapshotRequired bool                    `json:"snapshotRequired" validate:"true"`
}

type SessionForkParams struct {
	SessionMutation
	CheckpointID CheckpointID `json:"checkpointId"`
	Name         string       `json:"name,omitempty"`
}

type SessionForkResult struct {
	SourceTarget       RuntimeTarget `json:"sourceTarget"`
	SourceRuntimeEpoch RuntimeEpoch  `json:"sourceRuntimeEpoch"`
	ChildTarget        RuntimeTarget `json:"childTarget"`
	ChildRuntimeEpoch  RuntimeEpoch  `json:"childRuntimeEpoch"`
}

type SessionRewindParams struct {
	SessionMutation
	CheckpointID CheckpointID `json:"checkpointId"`
	Scope        RewindScope  `json:"scope"`
}

type SessionRewindResult struct {
	WorkspaceChanged      bool `json:"workspaceChanged"`
	ConversationRewritten bool `json:"conversationRewritten"`
	SnapshotRequired      bool `json:"snapshotRequired" validate:"true"`
}

type SessionCompactParams struct {
	SessionMutation
	Instructions string `json:"instructions,omitempty"`
}

type OperationStartedResult struct {
	OperationID OperationID `json:"operationId"`
	Disposition string      `json:"disposition" validate:"const=started"`
}

type SessionSummarizeParams struct {
	SessionMutation
	CheckpointID CheckpointID     `json:"checkpointId"`
	Direction    SummaryDirection `json:"direction"`
}

type ShellRunParams struct {
	SessionMutation
	Command string `json:"command" validate:"nonempty"`
}

type OperationCancelParams struct {
	SessionMutation
	ExpectedOperationID OperationID `json:"expectedOperationId"`
}

type OperationCancelResult struct {
	Status      CancelStatus `json:"status"`
	OperationID OperationID  `json:"operationId"`
}

type SessionProfileSetParams struct {
	SessionMutation
	Patch ProfilePatch `json:"patch"`
}

type ProfileSetDisposition string

const (
	ProfileUpdated ProfileSetDisposition = "updated"
	ProfileRebuilt ProfileSetDisposition = "rebuilt"
)

type SessionProfileSetResult struct {
	ResolvedProfile       ResolvedProfile       `json:"resolvedProfile"`
	RuntimeEpoch          RuntimeEpoch          `json:"runtimeEpoch"`
	Disposition           ProfileSetDisposition `json:"disposition"`
	AutoResolvedPromptIDs []PromptID            `json:"autoResolvedPromptIds"`
}

type SessionGoalSetParams struct {
	SessionMutation
	Goal string `json:"goal" validate:"nonempty"`
}

type SessionGoalSetResult struct {
	Goal   string     `json:"goal" validate:"nonempty"`
	Status GoalStatus `json:"status"`
}

type SessionGoalResumeParams struct{ SessionMutation }

type SessionGoalResumeResult struct {
	Resumed bool       `json:"resumed"`
	Goal    string     `json:"goal"`
	Status  GoalStatus `json:"status"`
}

type SessionGoalClearParams struct{ SessionMutation }

type SessionGoalClearResult struct {
	Cleared bool `json:"cleared" validate:"true"`
}

type SessionContextParams struct{ RuntimeQuery }

type SessionContextResult struct {
	Context ContextView `json:"context"`
}

type SessionBalanceParams struct{ RuntimeQuery }

type SessionBalanceResult struct {
	Available bool   `json:"available"`
	Display   string `json:"display"`
}

type JobListParams struct {
	RuntimeQuery
	Cursor Cursor `json:"cursor,omitempty"`
	Limit  *int   `json:"limit,omitempty" validate:"min=1,max=1000"`
}

type JobListResult struct {
	Jobs       []JobView `json:"jobs"`
	HasMore    bool      `json:"hasMore"`
	NextCursor Cursor    `json:"nextCursor,omitempty"`
}

type JobCancelParams struct {
	SessionMutation
	JobID JobID `json:"jobId"`
}

type JobCancelResult struct {
	Disposition JobCancelDisposition `json:"disposition"`
}
