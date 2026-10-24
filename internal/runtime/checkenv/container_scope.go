package checkenv

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func (i *containerEnvironment) Started(cmd *exec.Cmd) {
	i.mu.Lock()
	if i.started || i.cmd != cmd || cmd.Process == nil {
		i.mu.Unlock()
		return
	}
	i.started = true
	i.mu.Unlock()
	go i.monitor()
}

func (i *containerEnvironment) Cancel() error {
	i.mu.Lock()
	i.cancelled = true
	write, client, run, identity := i.controlWrite, i.client, i.journal.RunID, i.identity
	i.mu.Unlock()
	if write != nil {
		_ = write.Close()
	}
	if client == nil || run == "" {
		return nil
	}
	object, err := client.inspect(context.Background(), run)
	if err != nil {
		return err
	}
	if object.ID != run || object.Config.Labels[containerOwnerLabel] != identity {
		return errors.New("verification cancellation ownership changed")
	}
	_, err = client.run(context.Background(), nil, "kill", "--signal=KILL", run)
	return err
}

func (i *containerEnvironment) WaitScope() error {
	i.mu.Lock()
	started := i.started
	i.mu.Unlock()
	if !started {
		return errors.New("container verification did not start")
	}
	i.waitOnce.Do(func() { close(i.cliWaited) })
	<-i.done
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.scopeErr
}

func (i *containerEnvironment) monitor() {
	err := i.monitorPayload()
	if err != nil {
		_ = i.Cancel()
	}
	stopped := i.waitStopped()
	i.mu.Lock()
	i.scopeErr = errors.Join(err, stopped)
	i.mu.Unlock()
	close(i.done)
}

func (i *containerEnvironment) monitorPayload() error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		body, err := i.client.run(i.ctx, nil, "exec", "--user=0:0", i.journal.RunID, "/usr/bin/cat", "/run/reasonix-control/status")
		if err == nil {
			code, err := strconv.Atoi(strings.TrimSpace(string(body)))
			if err != nil || code < 0 || code > 255 {
				return errors.New("invalid container completion receipt")
			}
			i.mu.Lock()
			i.exitCode = code
			write := i.controlWrite
			i.mu.Unlock()
			if write != nil {
				_ = write.Close()
			}
			return nil
		}
		select {
		case <-i.ctx.Done():
			return i.ctx.Err()
		case <-i.cliWaited:
			return errors.New("container CLI ended without a completion receipt")
		case <-ticker.C:
		}
	}
}

func (i *containerEnvironment) waitStopped() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		object, err := i.client.inspect(ctx, i.journal.RunID)
		if err != nil {
			return err
		}
		if !object.State.Running && object.State.Pid == 0 {
			i.mu.Lock()
			code := i.exitCode
			i.mu.Unlock()
			if object.State.Error != "" || code < 0 || object.State.ExitCode != code {
				return errors.New("container scope did not complete as observed")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
