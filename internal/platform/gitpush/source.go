package gitpush

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

// SourceScope holds the source repository only for preparation and settlement.
// The network transfer uses a self-contained scratch repository.
type SourceScope func(context.Context) (func(), error)

type preparedPush struct {
	scratch *scratch
	plan    Plan
	raw     string
	oldRef  string
	shallow bool
}

func sourceScope(ctx context.Context, scope SourceScope) (func(), error) {
	if scope == nil {
		return func() {}, nil
	}
	return scope(ctx)
}

func preparePush(ctx context.Context, repo gitcmd.Repo, confirmed Plan, scope SourceScope, whole bool) (_ preparedPush, cleanup func(), err error) {
	release, err := sourceScope(ctx, scope)
	if err != nil {
		return preparedPush{}, nil, err
	}
	defer release()
	plan, raw, err := preview(ctx, repo)
	if err != nil {
		return preparedPush{}, nil, err
	}
	if !plan.sameAs(confirmed) {
		return preparedPush{}, nil, ErrPlanChanged
	}
	if plan.Ahead == 0 {
		return preparedPush{}, nil, ErrNothingToPush
	}
	old, err := trackingHead(ctx, repo, plan)
	if err != nil {
		return preparedPush{}, nil, err
	}
	sc, cleanup, err := newScratch(ctx, repo)
	if err != nil {
		return preparedPush{}, nil, err
	}
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	if out, err := sc.repo.Command(ctx, "update-ref", "refs/heads/push", plan.Head).CombinedOutput(); err != nil {
		return preparedPush{}, cleanup, fmt.Errorf("%w: %s", errors.Join(ErrPushFailed, ctx.Err(), err), strings.TrimSpace(string(out)))
	}
	shallow, err := materialize(ctx, repo, sc, plan, old, whole)
	if err != nil {
		return preparedPush{}, cleanup, err
	}
	if err := os.Remove(filepath.Join(sc.repo.CommonDir, "objects", "info", "alternates")); err != nil {
		return preparedPush{}, cleanup, err
	}
	return preparedPush{scratch: sc, plan: plan, raw: raw, oldRef: old, shallow: shallow}, cleanup, nil
}

func trackingRef(plan Plan) string { return "refs/remotes/" + plan.Remote + "/" + plan.UpstreamBranch }

func trackingHead(ctx context.Context, repo gitcmd.Repo, plan Plan) (string, error) {
	head, err := output(ctx, repo.Top(), "rev-parse", "--verify", "--quiet", trackingRef(plan))
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && ctx.Err() == nil {
		return strings.Repeat("0", len(plan.Head)), nil
	}
	return head, err
}

func settlePush(ctx context.Context, repo gitcmd.Repo, p preparedPush, scope SourceScope) string {
	release, err := sourceScope(ctx, scope)
	if err != nil {
		return "not_updated"
	}
	defer release()
	current, err := trackingHead(ctx, repo, p.plan)
	if err != nil {
		return "not_updated"
	}
	if current == p.plan.Head {
		return "updated"
	}
	if current != p.oldRef {
		return "not_updated"
	}
	if err := repo.Top().Command(ctx, "update-ref", "--no-deref", trackingRef(p.plan), p.plan.Head, p.oldRef).Run(); err != nil {
		return "not_updated"
	}
	return "updated"
}
