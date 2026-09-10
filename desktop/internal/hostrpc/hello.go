package hostrpc

import (
	"fmt"
	"maps"

	"reasonix/desktop/internal/instanceidentity"
	"reasonix/internal/extension/rpcwire"
)

// Error codes from docs/DESKTOP_HOST_PROTOCOL.md. Business errors raised by
// an invoked method use CodeBusiness with data.method naming the command.
const (
	CodeBusiness         = -32000
	CodeProtocolMismatch = -32001
	CodeNotReady         = -32002
	CodeContractMismatch = -32003
	CodeBuildMismatch    = -32004
	CodeInstanceMismatch = -32005
)

// Identity is what the service reports about itself in hello and checks
// the shell against. Home is the canonical Reasonix data home.
type Identity struct {
	Version string
	Channel string
	Commit  string
	Home    string
}

// HelloParams is the shell's desktop/hello request.
type HelloParams struct {
	ProtocolVersion int           `json:"protocolVersion"`
	ContractDigest  string        `json:"contractDigest"`
	Build           BuildInfo     `json:"build"`
	Host            HostInfo      `json:"host"`
	Instance        HelloInstance `json:"instance"`
}

// BuildInfo identifies one side's release build.
type BuildInfo struct {
	Version string `json:"version"`
	Channel string `json:"channel"`
	Commit  string `json:"commit"`
}

// HostInfo describes the shell runtime.
type HostInfo struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Chrome   string `json:"chrome"`
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
}

// HelloInstance is the data home the shell was launched for; Dev relaxes
// the build check for unpackaged shells.
type HelloInstance struct {
	Home string `json:"home"`
	Dev  bool   `json:"dev"`
}

// HelloResult is the service's desktop/hello response.
type HelloResult struct {
	ProtocolVersion   int             `json:"protocolVersion"`
	ContractDigest    string          `json:"contractDigest"`
	Service           ServiceInfo     `json:"service"`
	RuntimeGeneration string          `json:"runtimeGeneration"`
	Resources         Resources       `json:"resources"`
	Window            *WindowGeometry `json:"window,omitempty"`
}

// ServiceInfo is the service build plus its process id.
type ServiceInfo struct {
	BuildInfo
	PID int `json:"pid"`
}

// Resources locates the loopback origin serving authorised assets and the
// bearer token the shell's main process attaches to every request.
type Resources struct {
	Origin string `json:"origin"`
	Token  string `json:"token"`
}

// WindowGeometry is the initial main-window geometry the shell creates
// the hidden window with.
type WindowGeometry struct {
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	MinWidth   int     `json:"minWidth"`
	MinHeight  int     `json:"minHeight"`
	Frameless  bool    `json:"frameless"`
	ZoomFactor float64 `json:"zoomFactor"`
}

func mismatch(code int, name, message string, detail map[string]any) error {
	data := map[string]any{"name": name}
	maps.Copy(data, detail)
	return &rpcwire.RPCError{Code: code, Message: message, Data: data}
}

func notReady() error {
	return mismatch(CodeNotReady, "not_ready", "desktop/hello has not completed", nil)
}

// validateHello runs the handshake checks in the order the protocol lists
// them; the first failure is the terminal error the shell displays.
func validateHello(p HelloParams, digest string, id Identity) error {
	if p.ProtocolVersion != ProtocolVersion {
		return mismatch(CodeProtocolMismatch, "protocol_mismatch",
			fmt.Sprintf("shell speaks protocol %d, service speaks %d", p.ProtocolVersion, ProtocolVersion),
			map[string]any{"expected": ProtocolVersion, "got": p.ProtocolVersion})
	}
	if p.ContractDigest != digest {
		return mismatch(CodeContractMismatch, "contract_mismatch",
			"shell and service were built from different desktop contracts",
			map[string]any{"expected": digest, "got": p.ContractDigest})
	}
	dev := p.Instance.Dev || p.Build.Version == "dev" || id.Version == "dev"
	if !dev && p.Build.Version != id.Version {
		return mismatch(CodeBuildMismatch, "build_mismatch",
			fmt.Sprintf("shell build %s does not match service build %s", p.Build.Version, id.Version),
			map[string]any{"expected": id.Version, "got": p.Build.Version})
	}
	shellHome := instanceidentity.CanonicalHome(p.Instance.Home)
	serviceHome := instanceidentity.CanonicalHome(id.Home)
	if shellHome == "" || shellHome != serviceHome {
		return mismatch(CodeInstanceMismatch, "instance_mismatch",
			"shell data home does not match the service data home",
			map[string]any{"expected": serviceHome, "got": shellHome})
	}
	return nil
}
