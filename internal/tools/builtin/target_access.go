package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"syscall"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

// CodeReadForbidden identifies a target denied by the workspace read policy.
const CodeReadForbidden = "workspace.read_forbidden"

// Overwrite refusals retain the read-policy cause while naming the write action.
const (
	CodeOverwriteReadForbidden    = "workspace.overwrite_read_forbidden"
	CodeOverwriteReadOutsideScope = "workspace.overwrite_read_outside_scope"
)

const overwriteReadAccessDeniedMessage = "Cannot overwrite target file: permission to read existing contents is required."

// TargetAccessCheck binds one path policy for Agent admission. Existing tool
// path resolvers also work through path-bound subagent wrappers.
func (w Workspace) TargetAccessCheck() tool.TargetAccessCheck {
	roots := w.WriteRoots
	if len(roots) == 0 && w.Dir != "" {
		roots = []string{w.Dir}
	}
	netRoots := networkRoots(w.Dir, w.WriteRoots, w.ReadRoots)
	roots = append(realRoots(roots), networkSpellings(append([]string{w.Dir}, roots...))...)
	readRoots, forbidden := realRoots(w.ReadRoots), realRoots(w.ForbidReadRoots)
	return func(ctx context.Context, target tool.Tool, args json.RawMessage) error {
		var paths []string
		if tool.WritesNamedPaths(target) {
			writer, ok := target.(tool.WritePathResolver)
			if !ok {
				return fmt.Errorf("cannot resolve file targets for %s", target.Name())
			}
			var err error
			paths, err = writer.WritePaths(args)
			if errors.Is(err, syscall.ENOTDIR) {
				return nil
			}
			if err != nil && (!errors.Is(err, fileutil.ErrAmbiguousPath) || len(paths) == 0) {
				return err
			}
			if len(paths) == 0 {
				return fmt.Errorf("no file targets resolved for %s", target.Name())
			}
			for _, path := range paths {
				if err := confinePreview(roots, w.SessionGuard, w.ManagedConfig, w.SessionTemp, path); err != nil {
					return err
				}
			}
		} else if reader, ok := target.(tool.ReadTargeter); ok {
			if path := reader.ReadTarget(args); path != "" {
				if err := w.checkReadNetworkPath(path, args, netRoots); err != nil {
					return err
				}
				paths = []string{path}
			}
		}
		if reader, ok := target.(tool.ReadPathResolver); ok {
			var err error
			paths, err = reader.ReadPaths(ctx, args)
			if err != nil && (!errors.Is(err, fileutil.ErrAmbiguousPath) || len(paths) == 0) {
				return err
			}
		}
		for _, path := range paths {
			if err := checkReadTargetAccess(readRoots, forbidden, path); err != nil {
				if formatter, ok := target.(tool.ReadRefusalFormatter); ok {
					err = formatter.FormatReadRefusal(err)
				}
				return err
			}
		}
		return nil
	}
}

func checkReadTargetAccess(readRoots, forbidden []string, path string) error {
	if readOutsideScope(readRoots, path) {
		return tool.Refusal{Code: CodeReadOutsideScope, Message: readOutsideScopeMessage(path, readRoots)}
	}
	if confineRead(forbidden, path) {
		return tool.Refusal{Code: CodeReadForbidden, Message: readForbiddenMessage(path)}
	}
	return nil
}

// readOutsideScopeMessage names the target and the folders this session may
// read. It depends only on the policy and the path, never on whether the file
// exists, so it tells the model what to do without saying anything about the
// disk.
func readOutsideScopeMessage(path string, readRoots []string) string {
	if len(readRoots) == 0 {
		return fmt.Sprintf("Cannot read `%s`: it is outside the folders this session may read. Read a path inside the workspace, or ask the user to add the folder.", path)
	}
	return fmt.Sprintf("Cannot read `%s`: it is outside the folders this session may read (%s). Read a path inside one of them, or ask the user to add the folder.", path, strings.Join(readRoots, ", "))
}

// readForbiddenMessage says the path is on the read-forbidden list without
// naming which entry matched: that list is a protection the user configured.
func readForbiddenMessage(path string) string {
	return fmt.Sprintf("Cannot read `%s`: it is on this session's forbidden-read list, a protection the user configured, so it cannot be read here.", path)
}

func checkOverwriteReadAccess(readRoots, forbidden []string, path string) error {
	return overwriteReadRefusal(checkReadTargetAccess(readRoots, forbidden, path))
}

func overwriteReadRefusal(err error) error {
	if err == nil {
		return nil
	}
	var refusal tool.Refusal
	if !errors.As(err, &refusal) {
		return err
	}
	switch refusal.Code {
	case CodeReadForbidden:
		refusal.Code = CodeOverwriteReadForbidden
	case CodeReadOutsideScope:
		refusal.Code = CodeOverwriteReadOutsideScope
	default:
		return err
	}
	refusal.Message = overwriteReadAccessDeniedMessage
	return refusal
}

func (w Workspace) checkReadNetworkPath(path string, args json.RawMessage, roots []string) error {
	resolved := ResolvedPath{Path: path}
	var params struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(args, &params) == nil {
		if bound, ok := w.ReadPaths.Resolve(params.Path); ok && bound.Path == path {
			resolved = bound
		}
	}
	return resolved.refuseNetwork(roots)
}
