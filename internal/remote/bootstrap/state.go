package bootstrap

import (
	"encoding/json"
	"path"
	"time"

	"reasonix/internal/store"
)

// ServeState is the JSON record a bootstrapped serve leaves on the remote host
// so a later (re)connect can find and reuse it. Fields use omitempty so an
// older record missing a field still decodes.
type ServeState struct {
	PID       int    `json:"pid"`
	Addr      string `json:"addr"` // 127.0.0.1:<port> on the remote host
	Workspace string `json:"workspace"`
	Version   string `json:"version,omitempty"`
	TokenFile string `json:"token_file"`
	LogFile   string `json:"log_file,omitempty"`
	StartedAt int64  `json:"started_at,omitempty"` // unix seconds
}

// MarshalState renders a ServeState as indented JSON.
func MarshalState(s ServeState) ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// UnmarshalState parses a ServeState record.
func UnmarshalState(data []byte) (ServeState, error) {
	var s ServeState
	if err := json.Unmarshal(data, &s); err != nil {
		return ServeState{}, err
	}
	return s, nil
}

// remoteDir is the ~/.reasonix/remote directory given the resolved remote home.
func remoteDir(home string) string {
	return path.Join(home, ".reasonix", store.RemoteDirName)
}

// pathsFor derives every per-workspace state path from the resolved remote
// home and workspace directory.
func pathsFor(home, workspace string) StatePaths {
	dir := remoteDir(home)
	slug := store.RemoteWorkspaceSlug(workspace)
	return StatePaths{
		Dir:       dir,
		StateJSON: path.Join(dir, store.RemoteServeStateName(slug)),
		TokenFile: path.Join(dir, store.RemoteServeTokenName(slug)),
		LogFile:   path.Join(dir, store.RemoteServeLogName(slug)),
		PortFile:  path.Join(dir, store.RemoteServePortName(slug)),
		PidFile:   path.Join(dir, store.RemoteServePidName(slug)),
		LockDir:   path.Join(dir, store.RemoteServeLockName(slug)),
		LockOwner: path.Join(dir, store.RemoteServeLockName(slug), "owner"),
	}
}

// uploadedBinPath is the fallback location for an uploaded reasonix binary.
func uploadedBinPath(home string) string {
	return path.Join(remoteDir(home), store.RemoteBinDirName, "reasonix")
}

func nowUnix(clock func() time.Time) int64 {
	if clock == nil {
		return 0
	}
	return clock().Unix()
}
