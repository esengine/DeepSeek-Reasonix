package gitpush

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/platform/gitcmd"
)

var (
	// ErrPlanChanged is a repository that no longer matches what the person
	// confirmed: another commit, another branch, another remote.
	ErrPlanChanged = errors.New("gitpush: the repository changed since the push was previewed")
	// ErrRejected is the remote refusing the update because it is not a
	// fast-forward. A push here is never forced.
	ErrRejected = errors.New("gitpush: the remote rejected the update")
	// ErrAuthRequired is the transport asking for a credential none of the
	// person's configured helpers supplied.
	ErrAuthRequired = errors.New("gitpush: the remote needs authentication")
	// ErrPushFailed is any other failure; the wrapped text is what git said and is
	// for a person to read, not for a program to decide on.
	ErrPushFailed = errors.New("gitpush: the push failed")
)

// sameAs is whether two plans send the same commit to the same place. The counts
// are left out: they move with every fetch and decide nothing about the push.
func (p Plan) sameAs(o Plan) bool {
	return p.Remote == o.Remote && p.Branch == o.Branch && p.UpstreamBranch == o.UpstreamBranch && p.URL == o.URL && p.Head == o.Head
}

// pushTimeout bounds one push: a remote that accepts the connection and never
// answers must not hold the panel.
const pushTimeout = 2 * time.Minute

// Result is a completed push.
type Result struct {
	Plan Plan `json:"plan"`
	// New is true when the branch did not exist on the remote before.
	New            bool   `json:"new"`
	TrackingStatus string `json:"trackingStatus,omitempty"`
}

// Push sends the confirmed plan's branch to its upstream. It re-reads the plan
// and refuses, with ErrPlanChanged, anything that differs from what was
// confirmed. It never forces, and it names exactly one ref.
func Push(ctx context.Context, repo gitcmd.Repo, confirmed Plan) (Result, error) {
	return PushWithScope(ctx, repo, confirmed, nil)
}

func PushWithScope(ctx context.Context, repo gitcmd.Repo, confirmed Plan, scope SourceScope) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	result, shallow, err := pushOnce(ctx, repo, confirmed, scope, false)
	if err != nil && shallow && ctx.Err() == nil && !errors.Is(err, ErrAuthRequired) {
		result, _, err = pushOnce(ctx, repo, confirmed, scope, true)
	}
	return result, err
}

// pushOnce reports whether its transfer rested on the tracking refs' account of
// the remote, which a rewritten or reset destination may not hold.
func pushOnce(ctx context.Context, repo gitcmd.Repo, confirmed Plan, scope SourceScope, whole bool) (Result, bool, error) {
	prepared, cleanup, err := preparePush(ctx, repo, confirmed, scope, whole)
	if err != nil {
		return Result{}, false, err
	}
	defer cleanup()
	result, err := transfer(ctx, repo, prepared, scope)
	return result, prepared.shallow, err
}

func transfer(ctx context.Context, repo gitcmd.Repo, prepared preparedPush, scope SourceScope) (Result, error) {
	sc, plan, raw := prepared.scratch, prepared.plan, prepared.raw
	mark, err := writeAskpass(sc.dir)
	if err != nil {
		return Result{}, err
	}
	cmd := sc.repo.Command(ctx, "push", "--porcelain", "--", raw, "refs/heads/push:refs/heads/"+plan.UpstreamBranch)
	cmd.Env = append(cmd.Env,
		"GIT_ALLOW_PROTOCOL="+allowedProtocols,
		"GIT_ASKPASS="+mark.program,
		"SSH_ASKPASS="+mark.program,
		"SSH_ASKPASS_REQUIRE=force",
		"REASONIX_ASKPASS_MARK="+mark.file,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	status := refStatus(stdout.Bytes(), "refs/heads/"+plan.UpstreamBranch)
	if runErr != nil {
		switch {
		case ctx.Err() != nil:
			return Result{}, ctx.Err()
		case status == '!':
			return Result{}, ErrRejected
		case mark.asked():
			return Result{}, ErrAuthRequired
		}
		return Result{}, fmt.Errorf("%w: %s", ErrPushFailed, tail(stderr.String()))
	}
	return Result{Plan: plan, New: status == '*', TrackingStatus: settlePush(ctx, repo, prepared, scope)}, nil
}

// refStatus is the one-character flag git --porcelain gives the ref's line: ' '
// a fast-forward, '*' a new ref, '=' up to date, '!' rejected. 0 means no line.
func refStatus(porcelain []byte, ref string) byte {
	for line := range bytes.SplitSeq(porcelain, []byte{'\n'}) {
		fields := bytes.Split(line, []byte{'\t'})
		if len(fields) < 2 || len(fields[0]) != 1 {
			continue
		}
		if _, dst, ok := strings.Cut(string(fields[1]), ":"); ok && dst == ref {
			return fields[0][0]
		}
	}
	return 0
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	return s
}

// askpass is a program git and ssh call to ask for a credential. This one
// answers nothing and leaves a mark behind, which is how a push that stopped for
// want of a credential is told from one that failed for another reason.
type askpass struct {
	program string
	file    string
}

func (a askpass) asked() bool {
	_, err := os.Stat(a.file)
	return err == nil
}

func writeAskpass(dir string) (askpass, error) {
	a := askpass{file: filepath.Join(dir, "asked")}
	body, name, perm := "#!/bin/sh\n: > \"$REASONIX_ASKPASS_MARK\"\nexit 1\n", "askpass.sh", os.FileMode(0o700)
	if runtime.GOOS == "windows" {
		body, name, perm = "@echo off\r\ntype nul > \"%REASONIX_ASKPASS_MARK%\"\r\nexit /b 1\r\n", "askpass.cmd", 0o700
	}
	a.program = filepath.Join(dir, name)
	if err := os.WriteFile(a.program, []byte(body), perm); err != nil {
		return askpass{}, err
	}
	return a, nil
}
