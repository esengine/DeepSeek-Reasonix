package protocol

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

type Method string

const (
	MethodRemoteInitialize       Method = "remote/initialize"
	MethodRemotePing             Method = "remote/ping"
	MethodRemoteDetach           Method = "remote/detach"
	MethodHostCapabilities       Method = "host/capabilities"
	MethodHostConfigSummary      Method = "host/configSummary"
	MethodWorkspaceBrowse        Method = "workspace/browse"
	MethodWorkspaceOpen          Method = "workspace/open"
	MethodWorkspaceList          Method = "workspace/list"
	MethodWorkspaceClose         Method = "workspace/close"
	MethodWorkspaceChanges       Method = "workspace/changes"
	MethodWorkspaceChangeDetail  Method = "workspace/changeDetail"
	MethodCatalogWorkspace       Method = "catalog/workspace"
	MethodCatalogSession         Method = "catalog/session"
	MethodTopicList              Method = "topic/list"
	MethodTopicCreate            Method = "topic/create"
	MethodTopicRename            Method = "topic/rename"
	MethodTopicDelete            Method = "topic/delete"
	MethodTopicTrash             Method = "topic/trash"
	MethodSessionList            Method = "session/list"
	MethodSessionCreate          Method = "session/create"
	MethodSessionRename          Method = "session/rename"
	MethodSessionClose           Method = "session/close"
	MethodSessionTrashList       Method = "session/trashList"
	MethodSessionTrash           Method = "session/trash"
	MethodSessionRestore         Method = "session/restore"
	MethodSessionPurge           Method = "session/purge"
	MethodSessionSubscribe       Method = "session/subscribe"
	MethodSessionUnsubscribe     Method = "session/unsubscribe"
	MethodSessionHistory         Method = "session/history"
	MethodSessionContent         Method = "session/content"
	MethodSessionEvent           Method = "session/event"
	MethodSessionResyncRequired  Method = "session/resync_required"
	MethodCatalogChanged         Method = "catalog/changed"
	MethodSessionSubmit          Method = "session/submit"
	MethodTurnSteer              Method = "turn/steer"
	MethodTurnCancel             Method = "turn/cancel"
	MethodPromptApprove          Method = "prompt/approve"
	MethodPromptAnswer           Method = "prompt/answer"
	MethodShellRun               Method = "shell/run"
	MethodOperationCancel        Method = "operation/cancel"
	MethodSessionNew             Method = "session/new"
	MethodSessionClear           Method = "session/clear"
	MethodSessionFork            Method = "session/fork"
	MethodSessionRewind          Method = "session/rewind"
	MethodSessionCompact         Method = "session/compact"
	MethodSessionSummarize       Method = "session/summarize"
	MethodSessionProfileSet      Method = "session/profile/set"
	MethodSessionGoalSet         Method = "session/goal/set"
	MethodSessionGoalResume      Method = "session/goal/resume"
	MethodSessionGoalClear       Method = "session/goal/clear"
	MethodSessionContext         Method = "session/context"
	MethodSessionBalance         Method = "session/balance"
	MethodJobList                Method = "job/list"
	MethodJobCancel              Method = "job/cancel"
	MethodComposerSlashArgs      Method = "composer/slashArgs"
	MethodComposerHistory        Method = "composer/history"
	MethodFileList               Method = "file/list"
	MethodFileSearch             Method = "file/search"
	MethodFilePreview            Method = "file/preview"
	MethodGitHistory             Method = "git/history"
	MethodGitCommitDetail        Method = "git/commitDetail"
	MethodMemoryGet              Method = "memory/get"
	MethodMemorySuggestions      Method = "memory/suggestions"
	MethodMemoryRemember         Method = "memory/remember"
	MethodMemoryForget           Method = "memory/forget"
	MethodMemoryDocumentSave     Method = "memory/document/save"
	MethodMemorySuggestionAccept Method = "memory/suggestion/accept"
	MethodSkillSuggestionAccept  Method = "skill/suggestion/accept"
	MethodResearchStatus         Method = "research/status"
	MethodResearchList           Method = "research/list"
	MethodResearchFindings       Method = "research/findings"
	MethodResearchEvidenceRecord Method = "research/evidence/record"

	// Provider Broker (Host → Desktop requests; Desktop → Host notifications).
	MethodBrokerCatalog        Method = "broker/catalog"
	MethodBrokerStreamOpen     Method = "broker/stream/open"
	MethodBrokerStreamCancel   Method = "broker/stream/cancel"
	MethodBrokerStreamChunk    Method = "broker/stream/chunk"
	MethodBrokerStreamEnd      Method = "broker/stream/end"
	MethodBrokerCatalogChanged Method = "broker/catalog-changed"
)

type MethodSpec struct {
	Name       Method
	Direction  Direction
	Class      OperationClass
	ParamsType reflect.Type
	ResultType reflect.Type
}

func request[P, R any](name Method, class OperationClass) MethodSpec {
	return MethodSpec{name, DirectionClientRequest, class, typeOf[P](), typeOf[R]()}
}

func hostRequest[P, R any](name Method, class OperationClass) MethodSpec {
	return MethodSpec{name, DirectionHostRequest, class, typeOf[P](), typeOf[R]()}
}

func notification[P any](name Method) MethodSpec {
	return MethodSpec{name, DirectionHostNotification, ClassHostNotification, typeOf[P](), typeOf[NoResult]()}
}

func clientNotification[P any](name Method, class OperationClass) MethodSpec {
	return MethodSpec{name, DirectionClientNotification, class, typeOf[P](), typeOf[NoResult]()}
}

func typeOf[T any]() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }

var frozenRegistry = []MethodSpec{
	request[InitializeParams, InitializeResult](MethodRemoteInitialize, ClassConnection),
	request[PingParams, PingResult](MethodRemotePing, ClassConnection),
	request[DetachParams, DetachResult](MethodRemoteDetach, ClassConnection),
	request[HostCapabilitiesParams, HostCapabilitiesResult](MethodHostCapabilities, ClassHostQuery),
	request[HostConfigSummaryParams, HostConfigSummaryResult](MethodHostConfigSummary, ClassHostQuery),
	request[WorkspaceBrowseParams, WorkspaceBrowseResult](MethodWorkspaceBrowse, ClassHostQuery),
	request[WorkspaceOpenParams, WorkspaceOpenResult](MethodWorkspaceOpen, ClassHostMutation),
	request[WorkspaceListParams, WorkspaceListResult](MethodWorkspaceList, ClassHostQuery),
	request[WorkspaceCloseParams, WorkspaceCloseResult](MethodWorkspaceClose, ClassHostMutation),
	request[WorkspaceChangesParams, WorkspaceChangesResult](MethodWorkspaceChanges, ClassSessionQuery),
	request[WorkspaceChangeDetailParams, WorkspaceChangeDetailResult](MethodWorkspaceChangeDetail, ClassSessionQuery),
	request[WorkspaceCatalogParams, WorkspaceCatalogResult](MethodCatalogWorkspace, ClassHostQuery),
	request[SessionCatalogParams, SessionCatalogResult](MethodCatalogSession, ClassSessionQuery),
	request[TopicListParams, TopicListResult](MethodTopicList, ClassHostQuery),
	request[TopicCreateParams, TopicCreateResult](MethodTopicCreate, ClassHostMutation),
	request[TopicRenameParams, TopicRenameResult](MethodTopicRename, ClassHostMutation),
	request[TopicDeleteParams, TopicDeleteResult](MethodTopicDelete, ClassHostMutation),
	request[TopicTrashParams, TopicTrashResult](MethodTopicTrash, ClassHostMutation),
	request[SessionListParams, SessionListResult](MethodSessionList, ClassHostQuery),
	request[SessionCreateParams, SessionCreateResult](MethodSessionCreate, ClassHostMutation),
	request[SessionRenameParams, SessionRenameResult](MethodSessionRename, ClassSessionRecordMutation),
	request[SessionCloseParams, SessionCloseResult](MethodSessionClose, ClassSessionMutation),
	request[SessionTrashListParams, SessionTrashListResult](MethodSessionTrashList, ClassHostQuery),
	request[SessionTrashParams, SessionTrashResult](MethodSessionTrash, ClassSessionRecordMutation),
	request[SessionRestoreParams, SessionRestoreResult](MethodSessionRestore, ClassSessionRecordMutation),
	request[SessionPurgeParams, SessionPurgeResult](MethodSessionPurge, ClassSessionRecordMutation),
	request[SessionSubscribeParams, SessionSubscribeResult](MethodSessionSubscribe, ClassSessionQuery),
	request[SessionUnsubscribeParams, SessionUnsubscribeResult](MethodSessionUnsubscribe, ClassConnection),
	request[SessionHistoryParams, HistoryPage](MethodSessionHistory, ClassSessionQuery),
	request[SessionContentParams, SessionContentResult](MethodSessionContent, ClassConnection),
	notification[SessionEvent](MethodSessionEvent),
	notification[SessionResyncRequired](MethodSessionResyncRequired),
	notification[CatalogChanged](MethodCatalogChanged),
	request[SessionSubmitParams, SessionSubmitResult](MethodSessionSubmit, ClassSessionMutation),
	request[TurnSteerParams, TurnSteerResult](MethodTurnSteer, ClassSessionMutation),
	request[TurnCancelParams, TurnCancelResult](MethodTurnCancel, ClassSessionMutation),
	request[PromptApproveParams, PromptResolvedResult](MethodPromptApprove, ClassSessionMutation),
	request[PromptAnswerParams, PromptResolvedResult](MethodPromptAnswer, ClassSessionMutation),
	request[ShellRunParams, OperationStartedResult](MethodShellRun, ClassSessionMutation),
	request[OperationCancelParams, OperationCancelResult](MethodOperationCancel, ClassSessionMutation),
	request[SessionNewParams, SessionNewResult](MethodSessionNew, ClassSessionMutation),
	request[SessionClearParams, SessionClearResult](MethodSessionClear, ClassSessionMutation),
	request[SessionForkParams, SessionForkResult](MethodSessionFork, ClassSessionMutation),
	request[SessionRewindParams, SessionRewindResult](MethodSessionRewind, ClassSessionMutation),
	request[SessionCompactParams, OperationStartedResult](MethodSessionCompact, ClassSessionMutation),
	request[SessionSummarizeParams, OperationStartedResult](MethodSessionSummarize, ClassSessionMutation),
	request[SessionProfileSetParams, SessionProfileSetResult](MethodSessionProfileSet, ClassSessionMutation),
	request[SessionGoalSetParams, SessionGoalSetResult](MethodSessionGoalSet, ClassSessionMutation),
	request[SessionGoalResumeParams, SessionGoalResumeResult](MethodSessionGoalResume, ClassSessionMutation),
	request[SessionGoalClearParams, SessionGoalClearResult](MethodSessionGoalClear, ClassSessionMutation),
	request[SessionContextParams, SessionContextResult](MethodSessionContext, ClassSessionQuery),
	request[SessionBalanceParams, SessionBalanceResult](MethodSessionBalance, ClassSessionQuery),
	request[JobListParams, JobListResult](MethodJobList, ClassSessionQuery),
	request[JobCancelParams, JobCancelResult](MethodJobCancel, ClassSessionMutation),
	request[ComposerSlashArgsParams, ComposerSlashArgsResult](MethodComposerSlashArgs, ClassSessionQuery),
	request[ComposerHistoryParams, ComposerHistoryResult](MethodComposerHistory, ClassHostQuery),
	request[FileListParams, FileListResult](MethodFileList, ClassSessionQuery),
	request[FileSearchParams, FileSearchResult](MethodFileSearch, ClassSessionQuery),
	request[FilePreviewParams, FilePreviewResult](MethodFilePreview, ClassSessionQuery),
	request[GitHistoryParams, GitHistoryResult](MethodGitHistory, ClassSessionQuery),
	request[GitCommitDetailParams, GitCommitDetailResult](MethodGitCommitDetail, ClassSessionQuery),
	request[MemoryGetParams, MemoryGetResult](MethodMemoryGet, ClassSessionQuery),
	request[MemorySuggestionsParams, MemorySuggestionsResult](MethodMemorySuggestions, ClassSessionQuery),
	request[MemoryRememberParams, MemoryRememberResult](MethodMemoryRemember, ClassSessionMutation),
	request[MemoryForgetParams, MemoryForgetResult](MethodMemoryForget, ClassSessionMutation),
	request[MemoryDocumentSaveParams, MemoryDocumentSaveResult](MethodMemoryDocumentSave, ClassSessionMutation),
	request[MemorySuggestionAcceptParams, MemorySuggestionAcceptResult](MethodMemorySuggestionAccept, ClassSessionMutation),
	request[SkillSuggestionAcceptParams, SkillSuggestionAcceptResult](MethodSkillSuggestionAccept, ClassSessionMutation),
	request[ResearchStatusParams, ResearchStatusResult](MethodResearchStatus, ClassSessionQuery),
	request[ResearchListParams, ResearchListResult](MethodResearchList, ClassSessionQuery),
	request[ResearchFindingsParams, ResearchFindingsResult](MethodResearchFindings, ClassSessionQuery),
	request[ResearchEvidenceRecordParams, ResearchEvidenceRecordResult](MethodResearchEvidenceRecord, ClassSessionMutation),
	// Provider Broker: Host requests Desktop resolve streams; Desktop pushes chunks.
	hostRequest[BrokerCatalogParams, BrokerCatalogResult](MethodBrokerCatalog, ClassBrokerRequest),
	hostRequest[BrokerStreamOpenParams, BrokerStreamOpenResult](MethodBrokerStreamOpen, ClassBrokerRequest),
	hostRequest[BrokerStreamCancelParams, BrokerStreamCancelResult](MethodBrokerStreamCancel, ClassBrokerRequest),
	clientNotification[BrokerStreamChunkParams](MethodBrokerStreamChunk, ClassBrokerNotification),
	clientNotification[BrokerStreamEndParams](MethodBrokerStreamEnd, ClassBrokerNotification),
	clientNotification[BrokerCatalogChangedParams](MethodBrokerCatalogChanged, ClassBrokerNotification),
}

var frozenRegistryByName = buildRegistryIndex(frozenRegistry)

func buildRegistryIndex(specs []MethodSpec) map[Method]MethodSpec {
	index := make(map[Method]MethodSpec, len(specs))
	for _, spec := range specs {
		if _, duplicate := index[spec.Name]; duplicate {
			panic("protocol: duplicate method " + string(spec.Name))
		}
		if spec.ParamsType.Kind() != reflect.Struct || spec.ResultType.Kind() != reflect.Struct {
			panic("protocol: method types must be structs: " + string(spec.Name))
		}
		index[spec.Name] = spec
	}
	return index
}

func Registry() []MethodSpec {
	out := append([]MethodSpec(nil), frozenRegistry...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func LookupMethod(name Method) (MethodSpec, bool) {
	spec, ok := frozenRegistryByName[name]
	return spec, ok
}

// DecodeRequestParams applies the registry's exact client-request DTO and the
// same strict decoder used by Router. Bootstrap paths use this before proxying
// initialize so they cannot grow a second JSON or validation implementation.
func DecodeRequestParams(method Method, raw json.RawMessage) (any, error) {
	spec, ok := LookupMethod(method)
	if !ok {
		return nil, fmt.Errorf("protocol: unregistered method %q", method)
	}
	if spec.Direction != DirectionClientRequest {
		return nil, fmt.Errorf("protocol: %q is not a client request", method)
	}
	return decodeAndValidate(raw, spec.ParamsType)
}

// DecodeResult applies the frozen registry's exact result DTO, strict unknown
// field rejection, required-field checks, and semantic validation. Client
// transports use this instead of maintaining a second result switch beside
// the server Router.
func DecodeResult(method Method, raw json.RawMessage) (any, error) {
	spec, ok := LookupMethod(method)
	if !ok {
		return nil, fmt.Errorf("protocol: unregistered method %q", method)
	}
	if spec.Direction != DirectionClientRequest {
		return nil, fmt.Errorf("protocol: %q is not a client request", method)
	}
	return decodeAndValidate(raw, spec.ResultType)
}

// DecodeBrokerResult applies the frozen result DTO for a Host-to-Desktop
// Broker request. It intentionally rejects ordinary Remote requests and both
// notification directions so Broker clients cannot bypass registry direction
// checks while decoding peer responses.
func DecodeBrokerResult(method Method, raw json.RawMessage) (any, error) {
	spec, ok := LookupMethod(method)
	if !ok {
		return nil, fmt.Errorf("protocol: unregistered method %q", method)
	}
	if spec.Direction != DirectionHostRequest {
		return nil, fmt.Errorf("protocol: %q is not a Broker request", method)
	}
	return decodeAndValidate(raw, spec.ResultType)
}

// DecodeNotificationParams applies the same strict decoder and validation
// rules to the three frozen Host notification payloads. Unknown methods and
// client-request methods are rejected rather than being silently ignored by a
// Remote client whose Build ID claims this exact schema.
func DecodeNotificationParams(method Method, raw json.RawMessage) (any, error) {
	spec, ok := LookupMethod(method)
	if !ok {
		return nil, fmt.Errorf("protocol: unregistered method %q", method)
	}
	if spec.Direction != DirectionHostNotification {
		return nil, fmt.Errorf("protocol: %q is not a Host notification", method)
	}
	return decodeAndValidate(raw, spec.ParamsType)
}

// DecodeBrokerRequestParams applies the frozen Host-to-Desktop Broker DTO.
func DecodeBrokerRequestParams(method Method, raw json.RawMessage) (any, error) {
	spec, ok := LookupMethod(method)
	if !ok {
		return nil, fmt.Errorf("protocol: unregistered method %q", method)
	}
	if spec.Direction != DirectionHostRequest {
		return nil, fmt.Errorf("protocol: %q is not a Broker request", method)
	}
	return decodeAndValidate(raw, spec.ParamsType)
}

// DecodeBrokerNotificationParams applies the frozen Desktop-to-Host Broker DTO.
func DecodeBrokerNotificationParams(method Method, raw json.RawMessage) (any, error) {
	spec, ok := LookupMethod(method)
	if !ok {
		return nil, fmt.Errorf("protocol: unregistered method %q", method)
	}
	if spec.Direction != DirectionClientNotification {
		return nil, fmt.Errorf("protocol: %q is not a Broker notification", method)
	}
	return decodeAndValidate(raw, spec.ParamsType)
}

func ValidateRegistry() error {
	clientReq, hostNotif, hostReq, clientNotif := 0, 0, 0, 0
	for _, spec := range frozenRegistry {
		switch spec.Direction {
		case DirectionClientRequest:
			clientReq++
		case DirectionHostNotification:
			hostNotif++
		case DirectionHostRequest:
			hostReq++
		case DirectionClientNotification:
			clientNotif++
		default:
			return fmt.Errorf("method %s has invalid direction %q", spec.Name, spec.Direction)
		}
	}
	// Workbench RuntimeAPI (71 methods from Remote V1 surface) + 6 Provider Broker methods.
	if len(frozenRegistry) != 78 || clientReq != 69 || hostNotif != 3 || hostReq != 3 || clientNotif != 3 {
		return fmt.Errorf("registry count = total=%d clientReq=%d hostNotif=%d hostReq=%d clientNotif=%d, want 78/69/3/3/3",
			len(frozenRegistry), clientReq, hostNotif, hostReq, clientNotif)
	}
	return nil
}
