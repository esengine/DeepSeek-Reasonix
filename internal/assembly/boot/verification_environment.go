package boot

import (
	"path/filepath"

	"reasonix/internal/runtime/checkenv"
)

func (w roleWiring) verificationEnvironment(in delegationInputs) *checkenv.Spec {
	if w.cfg.Agent.VerificationRuntime == "" {
		return nil
	}
	root := in.opts.WorkspaceRepo.WorkTree
	if root == "" {
		root = in.root
	}
	return &checkenv.Spec{RuntimeRoot: w.cfg.Agent.VerificationRuntime, StorageRoot: filepath.Join(w.roots.DeliveryWorktreeDir(), "verification-v1"),
		SourceRoot: root, Directory: in.root, Client: in.opts.VerificationClient}
}
