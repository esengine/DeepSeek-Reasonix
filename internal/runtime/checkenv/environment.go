// Package checkenv owns bounded host verification environments and their
// process-local receipts. Ordinary shell observations never mint these receipts.
package checkenv

import (
	"errors"

	"reasonix/internal/contract/tool"
)

var ErrUnsupported = errors.New("controlled verification environment unavailable")

// Spec comes from the host's resolved configuration. RuntimeRoot is a declared
// offline root filesystem, not an implicit bind of the running host's root.
type Spec struct {
	RuntimeRoot string
	StorageRoot string
	SourceRoot  string
	Directory   string
	Client      ClientSpec
	capture     *capturedInput
}

type ClientSpec struct {
	Executable  string
	Environment []string
}

func (s ClientSpec) Clone() ClientSpec {
	s.Environment = append([]string(nil), s.Environment...)
	return s
}

var _ tool.ControlledShell = (*Invocation)(nil)
