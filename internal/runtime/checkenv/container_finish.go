package checkenv

import (
	"context"
	"reflect"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/safety/evidence"
)

func (i *containerEnvironment) finish() {
	i.mu.Lock()
	defer i.mu.Unlock()
	cmd, ctx := i.cmd, i.ctx
	if cmd == nil || cmd.ProcessState == nil || !cmd.ProcessState.Exited() || !i.started {
		return
	}
	i.input.Completed = true
	switch {
	case i.cancelled || i.closed || ctx.Err() != nil:
		i.input.Gaps = []string{"controlled_execution_cancelled"}
		return
	case i.scopeErr != nil:
		i.input.Gaps = []string{"controlled_scope_unconfirmed"}
		return
	case cmd.ProcessState.ExitCode() != i.exitCode:
		i.input.Gaps = []string{"controlled_exit_status_changed"}
		return
	}
	i.input.Gaps = []string{"controlled_launch_identity_changed"}
	if !reflect.DeepEqual(cmd.Args, i.argv) || cmd.Path != i.client.executable || cmd.Dir != i.client.config || cmd.Stdin != i.controlRead {
		return
	}
	object, err := i.client.inspect(ctx, i.journal.RunID)
	i.input.Gaps = []string{"controlled_container_configuration_changed"}
	if err != nil || object.State.Running || object.State.Pid != 0 || object.State.ExitCode != i.exitCode ||
		object.Image != i.journal.Image || object.configuration() != i.journal.Configuration {
		return
	}
	var engine containerEngine
	i.input.Gaps = []string{"controlled_engine_identity_changed"}
	if err := i.client.decode(ctx, &engine, "info", "--format", "{{json .}}"); err != nil || engine != i.client.engine {
		return
	}
	launcher, err := fileutil.ReadContentVersion(ctx, i.client.executable)
	i.input.Gaps = []string{"controlled_launcher_changed"}
	if err != nil || launcher != i.client.version {
		return
	}
	expectedEnvironment := digest(struct {
		Environment                  []string
		Engine                       containerEngine
		Image, Source, Configuration string
	}{
		cmd.Environ(), i.client.engine, i.journal.Image, i.sourceDigest, i.journal.Configuration})
	i.input.Gaps = []string{"controlled_launch_environment_changed"}
	if expectedEnvironment != i.input.EnvironmentHash {
		return
	}
	i.input.Gaps = []string{"controlled_input_snapshot_changed"}
	if !i.stableContent(ctx) {
		return
	}
	i.input.Stable, i.input.Gaps = true, nil
	i.input.EnvironmentReceipt = digest(struct{ Identity, Execution, Call, Image, Source, Capture string }{
		i.identity, i.execution, i.call, i.journal.Image, i.sourceDigest, i.spec.captureIdentity()})
	recordOwnedReceipt(i.input, i.execution, i.spec.captureIdentity())
}

func (i *containerEnvironment) stableContent(ctx context.Context) bool {
	i.input.After = evidence.CaptureContentSnapshot(ctx, i.spec.SourceRoot, 50_000)
	if !i.input.After.Complete || i.input.Before.Digest != i.input.After.Digest {
		return false
	}
	copied, err := scanTree(ctx, i.source, treePolicy{})
	if err != nil || digest(copied) != i.sourceDigest {
		return false
	}
	original, err := scanTree(ctx, i.spec.SourceRoot, i.policy)
	return err == nil && digest(original) == i.originalDigest
}
