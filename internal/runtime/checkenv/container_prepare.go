package checkenv

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
)

func (i *containerEnvironment) Prepare(ctx context.Context, request tool.ControlledShellRequest) ([]string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || i.directory != "" || request.Command != i.command || i.command == "" || i.execution == "" || i.call == "" {
		return nil, errors.New("verification invocation is stale or changed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	image, err := containerImageID(i.spec.RuntimeRoot)
	if err != nil {
		return nil, err
	}
	if err := i.freezeSource(ctx, request); err != nil {
		return nil, err
	}
	i.client, err = newContainerClient(ctx, i.directory, i.spec.Client)
	if err == nil {
		err = recoverContainerJournals(ctx, i.spec.StorageRoot, i.directory, i.client)
	}
	if err == nil {
		err = i.prepareImage(ctx, image, request)
	}
	if err != nil {
		return nil, err
	}
	i.argv = i.client.command(ctx, "start", "--attach", "--interactive", i.journal.RunID).Args
	return append([]string(nil), i.argv...), nil
}

func (i *containerEnvironment) freezeSource(ctx context.Context, request tool.ControlledShellRequest) error {
	if !filepath.IsAbs(i.spec.SourceRoot) || !filepath.IsAbs(i.spec.Directory) || !filepath.IsAbs(i.spec.StorageRoot) {
		return ErrUnsupported
	}
	source, err := filepath.EvalSymlinks(i.spec.SourceRoot)
	if err != nil {
		return err
	}
	directory, err := filepath.EvalSymlinks(i.spec.Directory)
	if err != nil {
		return err
	}
	requested, err := filepath.EvalSymlinks(request.Directory)
	if err != nil || requested != directory {
		return errors.Join(ErrUnsupported, err)
	}
	if rel, err := filepath.Rel(source, directory); err != nil || !filepath.IsLocal(rel) {
		return ErrUnsupported
	}
	storage, err := fileutil.CanonicalWritePath(i.spec.StorageRoot)
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(source, storage); err != nil || filepath.IsLocal(rel) {
		return errors.New("verification storage intersects its input")
	}
	denied, err := relativeDenies(source, request.ReadDenies)
	if err != nil {
		return err
	}
	i.spec.SourceRoot, i.spec.Directory, i.spec.StorageRoot = source, directory, storage
	i.policy = treePolicy{source: true, denied: denied}
	i.input = evidence.CheckInputs{Directory: request.Directory, Platform: runtime.GOOS + "/" + runtime.GOARCH,
		CommandDigest: evidence.CheckCommandDigest(i.command), Gaps: []string{"controlled_environment_incomplete"}}
	i.input.Before = evidence.CaptureContentSnapshot(ctx, source, 50_000)
	if !i.input.Before.Complete {
		return errors.New("verification source observation is incomplete")
	}
	if err := os.MkdirAll(storage, 0700); err != nil {
		return err
	}
	i.directory = filepath.Join(storage, i.identity)
	if err := os.Mkdir(i.directory, 0700); err != nil {
		return err
	}
	i.release, err = filelock.TryAcquire(filepath.Join(i.directory, "owner.lock"))
	if err != nil {
		return err
	}
	i.source = filepath.Join(i.directory, "source")
	i.originalDigest, err = freezeTree(ctx, source, i.source, i.policy)
	if err != nil {
		return err
	}
	i.sourceDigest, err = i.spec.applyCapture(ctx, i.source, i.policy)
	i.sourcePath = "/reasonix-source-" + strings.TrimPrefix(i.identity, "check-")
	return err
}
