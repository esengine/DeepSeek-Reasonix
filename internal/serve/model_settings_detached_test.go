package serve

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
)

func TestDetachedModelSettingsRefreshTargetsItsOwnerAndPreservesFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			foreground := control.New(control.Options{ModelRef: "foreground/m"})
			defer foreground.Close()
			path := filepath.Join(t.TempDir(), "background.jsonl")
			old := control.New(control.Options{
				SessionPath: path, ModelRef: "p/m", ModelSettingsRevision: "old",
				ModelSettingsCurrent: func() (string, error) { return "new", nil },
			})
			defer old.Close()
			s := New(foreground, NewBroadcaster(), config.ServeConfig{AuthMode: "none"})
			d := &detachedSession{ctrl: old, path: path, buildOptions: boot.Options{StatsSource: "serve"}}
			s.detached = map[string]*detachedSession{path: d}
			builds := 0
			s.rebuildControllerWithOptions = func(_ context.Context, current *control.Controller, ref string, opts boot.Options) (*control.Controller, error) {
				builds++
				if current != old || opts.SessionDir != old.SessionDir() || ref != "p/m" {
					t.Fatal("rebuild used another runtime's identity")
				}
				if fail {
					return nil, fmt.Errorf("injected background build failure")
				}
				return control.New(control.Options{
					SessionPath: path, ModelRef: ref, Sink: opts.Sink, ModelSettingsRevision: "new",
					ModelSettingsCurrent: func() (string, error) { return "new", nil },
				}), nil
			}
			release, err := s.beforeInboxDispatch(old)
			if release != nil {
				release()
				t.Fatal("the outgoing runtime was admitted after a settings change")
			}
			if s.ctl() != foreground || builds != 1 {
				t.Fatal("background application changed the foreground owner")
			}
			if fail {
				if err == nil || d.ctrl != old {
					t.Fatal("failed background build replaced its owner")
				}
				return
			}
			if !errors.Is(err, control.ErrInboxRuntimeUnpublished) || d.ctrl == old {
				t.Fatalf("replacement was not published: %v", err)
			}
			next := d.ctrl.(*control.Controller)
			defer next.Close()
			release, err = s.beforeInboxDispatch(next)
			if err != nil || release == nil {
				t.Fatalf("replacement was not admitted: %v", err)
			}
			release()
			if builds != 1 {
				t.Fatal("unchanged background settings rebuilt again")
			}
		})
	}
}

func TestDetachedModelSettingsKeepsQueuedOwnerUntilAdmission(t *testing.T) {
	ctrl := control.New(control.Options{SessionPath: filepath.Join(t.TempDir(), "queued.jsonl")})
	defer ctrl.Close()
	ctrl.SetBeforeInboxDispatch(func(*control.Controller) (func(), error) {
		return nil, control.ErrInboxRuntimeUnpublished
	})
	if _, err := ctrl.EnqueueInbox(control.InboxRequest{Submit: "next background run"}); err != nil {
		t.Fatal(err)
	}
	d := &detachedSession{path: ctrl.SessionPath(), ctrl: ctrl}
	s := &Server{detached: map[string]*detachedSession{d.path: d}}
	if !s.detachedHasPendingWork(d) || d.retiring {
		t.Fatal("close-on-idle discarded the pending admission owner")
	}
	if err := ctrl.SetInboxPausedPassive(true); err != nil {
		t.Fatal(err)
	}
	if s.detachedHasPendingWork(d) || !d.retiring {
		t.Fatal("paused durable work prevented owner retirement")
	}
}
