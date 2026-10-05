package schedrun

import "errors"

// Callers tell these apart with errors.Is; the wrapping text is display only.
var (
	ErrSpawn           = errors.New("schedrun: the child process could not be started")
	ErrWallLimit       = errors.New("schedrun: the run passed its wall-clock limit and was killed")
	ErrTokenLimit      = errors.New("schedrun: the run passed its token ceiling and was killed")
	ErrCancelled       = errors.New("schedrun: the run was cancelled and killed")
	ErrChildCrashed    = errors.New("schedrun: the child ended without delivering a result")
	ErrProtocol        = errors.New("schedrun: the child broke the stream protocol and was killed")
	ErrParentGone      = errors.New("schedrun: the supervisor went away")
	ErrStartTimeout    = errors.New("schedrun: the supervisor did not release the run")
	ErrRepeatParked    = errors.New("schedrun: the run parked the same kind of request too often")
	ErrExecutorRefused = errors.New("schedrun: the child did not take the run, another executor holds it")
	ErrNoSupervisor    = errors.New("schedrun: this command is started by the supervisor and takes no terminal")
)

// Codes are the typed identities a run's outcome carries in its Report and in
// the result line; most are also an error above where a caller wants to branch.
const (
	CodeWallLimit        = "schedule.wall_limit"
	CodeTokenLimit       = "schedule.token_limit"
	CodeCancelled        = "schedule.cancelled"
	CodeCrashed          = "schedule.child_crashed"
	CodeProtocol         = "schedule.child_protocol"
	CodeSpawn            = "schedule.spawn_failed"
	CodeParentGone       = "schedule.parent_gone"
	CodeRepeatParked     = "schedule.repeat_parked"
	CodeParkLimit        = "schedule.park_limit"
	CodeUnmetered        = "schedule.unmetered"
	CodeRunHeld          = "schedule.run_held"
	CodeRunToken         = "schedule.run_token"
	CodeRunStarted       = "schedule.run_started"
	CodeRunSettled       = "schedule.run_settled"
	CodeDigest           = "schedule.digest_mismatch"
	CodeWorkspaceGone    = "schedule.workspace_gone"
	CodeBuild            = "schedule.build_failed"
	CodePolicy           = "schedule.policy_invalid"
	CodeProviderAuth     = "provider.auth"
	CodeProviderQuota    = "provider.quota"
	CodeRunError         = "schedule.run_error"
	CodeStore            = "schedule.store_error"
	CodeNotFound         = "schedule.not_found"
	CodeModelUnavailable = "schedule.model_unavailable"
)
