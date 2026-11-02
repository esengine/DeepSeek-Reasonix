package remote

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"reasonix/internal/platform/remote/sshtest"
)

func TestKeyboardInteractiveNeverUsesStoredPassword(t *testing.T) {
	answers := make(chan []string, 1)
	srv := sshtest.Start(t, sshtest.Options{KeyboardInteractive: func(_ ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		got, err := challenge("Access", "Use your authenticator", []string{"Challenge:"}, []bool{false})
		if err != nil {
			return nil, err
		}
		answers <- got
		return nil, fmt.Errorf("one-time answer required")
	}})
	host, _ := ResolveHost(nil, "test@"+srv.Addr, nil)
	c, err := New(Options{Host: host, HostKeys: managedOnlyPolicy(t, true), Auth: AuthOptions{DisableAgent: true, Password: func() (string, error) { return "stored-password", nil }}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Start(ctx); err == nil {
		t.Fatal("unattended challenge succeeded")
	}
	select {
	case got := <-answers:
		t.Fatalf("stored credential sent to arbitrary challenge: %q", got)
	default:
	}
}

func TestKeyboardInteractiveRoundsAndReconnect(t *testing.T) {
	var connections atomic.Int32
	srv := sshtest.Start(t, sshtest.Options{KeyboardInteractive: func(_ ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		attempt := connections.Add(1)
		got, err := challenge("Server title", "Exact instruction\nSecond line", []string{"Unclassified first:", "Public identifier:"}, []bool{false, true})
		if err != nil {
			return nil, err
		}
		if !slices.Equal(got, []string{"password", "visible"}) {
			return nil, fmt.Errorf("wrong ordered answers")
		}
		got, err = challenge("Second round", "Fresh challenge", []string{"Unclassified second:"}, []bool{false})
		if err != nil {
			return nil, err
		}
		if !slices.Equal(got, []string{fmt.Sprintf("otp-%d", attempt)}) {
			return nil, fmt.Errorf("reused one-time answer")
		}
		got, err = challenge("Welcome", "No questions", nil, nil)
		if err != nil {
			return nil, err
		}
		if len(got) != 0 {
			return nil, fmt.Errorf("unexpected instruction answer")
		}
		return &ssh.Permissions{}, nil
	}})
	var prompts atomic.Int32
	host, _ := ResolveHost(nil, "test@"+srv.Addr, nil)
	c, err := New(Options{
		Host: host, HostKeys: managedOnlyPolicy(t, true),
		Auth: AuthOptions{DisableAgent: true, Password: func() (string, error) { return "not-a-challenge-answer", nil },
			ChallengePrompt: func(ctx context.Context, q Challenge) (string, error) {
				n := prompts.Add(1)
				if q.Host != host.Label() {
					return "", fmt.Errorf("wrong host")
				}
				switch (n - 1) % 4 {
				case 0:
					if q.Name != "Server title" || q.Instruction != "Exact instruction\nSecond line" || q.Question != "Unclassified first:" || q.Echo || !q.AnswerRequired {
						return "", fmt.Errorf("lost first question metadata: %+v", q)
					}
					return "password", nil
				case 1:
					if q.Question != "Public identifier:" || !q.Echo || !q.AnswerRequired {
						return "", fmt.Errorf("lost echo/order")
					}
					return "visible", nil
				case 2:
					if q.Name != "Second round" || q.Instruction != "Fresh challenge" || q.Question != "Unclassified second:" || q.Echo {
						return "", fmt.Errorf("lost next round")
					}
					return fmt.Sprintf("otp-%d", (n-1)/4+1), nil
				default:
					if q.AnswerRequired || q.Name != "Welcome" || q.Instruction != "No questions" {
						return "", fmt.Errorf("lost instruction-only round")
					}
					return "", nil
				}
			}},
		Keepalive: KeepalivePolicy{Interval: 20 * time.Millisecond, MaxMisses: 1, Timeout: 100 * time.Millisecond},
		Backoff:   BackoffPolicy{Initial: time.Millisecond, Max: 10 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	reconnected := make(chan struct{}, 1)
	var connected atomic.Int32
	c.Subscribe(func(ev StatusEvent) {
		if ev.Status == StatusConnected && connected.Add(1) == 2 {
			reconnected <- struct{}{}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(ctx, "before"); err != nil {
		t.Fatal(err)
	}
	srv.DropConnections()
	select {
	case <-reconnected:
	case <-ctx.Done():
		t.Fatal("did not reconnect with fresh OTP")
	}
	if _, err := c.Exec(ctx, "after"); err != nil {
		t.Fatal(err)
	}
	if prompts.Load() != 8 {
		t.Fatalf("prompt count=%d", prompts.Load())
	}
	if c.opts.Auth.cache.havePw {
		t.Fatal("challenge answers entered password cache")
	}
}

func TestKeyboardInteractiveCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			srv := sshtest.Start(t, sshtest.Options{KeyboardInteractive: func(_ ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
				_, err := challenge("title", "instruction", []string{"arbitrary"}, []bool{false})
				return nil, err
			}})
			host, _ := ResolveHost(nil, "test@"+srv.Addr, nil)
			entered := make(chan struct{})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, err := New(Options{Host: host, HostKeys: managedOnlyPolicy(t, true), Auth: AuthOptions{DisableAgent: true, ChallengePrompt: func(ctx context.Context, _ Challenge) (string, error) {
				close(entered)
				if cancelled {
					<-ctx.Done()
					return "", ctx.Err()
				}
				return "", context.Canceled
			}}})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			result := make(chan error, 1)
			go func() { result <- c.Start(ctx) }()
			<-entered
			if cancelled {
				cancel()
			}
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("cancel succeeded")
				}
			case <-time.After(time.Second):
				t.Fatal("prompt did not cancel")
			}
		})
	}
}

func TestKeyboardInteractiveReconnectCancellationStopsSupervisor(t *testing.T) {
	var prompts atomic.Int32
	srv := sshtest.Start(t, sshtest.Options{KeyboardInteractive: func(_ ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		_, err := challenge("Verify", "Fresh code", []string{"Challenge:"}, []bool{false})
		return &ssh.Permissions{}, err
	}})
	host, _ := ResolveHost(nil, "test@"+srv.Addr, nil)
	c, err := New(Options{Host: host, HostKeys: managedOnlyPolicy(t, true), Auth: AuthOptions{DisableAgent: true, ChallengePrompt: func(context.Context, Challenge) (string, error) {
		if prompts.Add(1) == 1 {
			return "first-code", nil
		}
		return "", context.Canceled
	}}, Backoff: BackoffPolicy{Initial: time.Millisecond, Max: 5 * time.Millisecond}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv.DropConnections()
	select {
	case <-c.done:
	case <-ctx.Done():
		t.Fatal("cancelled reconnect keeps retrying")
	}
	if prompts.Load() != 2 {
		t.Fatalf("prompts=%d, want initial + cancelled reconnect", prompts.Load())
	}
	if ev := c.Status(); ev.Status != StatusStopped || !errors.Is(ev.Err, context.Canceled) {
		t.Fatalf("status=%+v", ev)
	}
}

func TestKeyboardInteractiveJumpHostChallengesStayDistinct(t *testing.T) {
	challenge := func(_ ssh.ConnMetadata, ask ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		answers, err := ask("Verify", "Fresh answer", []string{"Question:"}, []bool{false})
		if err != nil {
			return nil, err
		}
		if len(answers) != 1 || answers[0] == "" {
			return nil, fmt.Errorf("missing answer")
		}
		return &ssh.Permissions{}, nil
	}
	jump := sshtest.Start(t, sshtest.Options{KeyboardInteractive: challenge})
	target := sshtest.Start(t, sshtest.Options{KeyboardInteractive: challenge})
	jumpHost, _ := ResolveHost(nil, "jump@"+jump.Addr, nil)
	targetHost, _ := ResolveHost(nil, "target@"+target.Addr, nil)
	targetHost.ProxyJump = []string{jumpHost.Label()}
	var seen []string
	prompt := func(_ context.Context, q Challenge) (string, error) {
		seen = append(seen, q.Host)
		return "fresh-for-" + q.Host, nil
	}
	c, err := New(Options{Host: targetHost, HostKeys: managedOnlyPolicy(t, true),
		Auth:      AuthOptions{DisableAgent: true, ChallengePrompt: prompt},
		JumpHosts: []JumpHostOptions{{Host: jumpHost, Auth: AuthOptions{DisableAgent: true, ChallengePrompt: prompt}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(seen, []string{jumpHost.Label(), targetHost.Label()}) {
		t.Fatalf("challenge hosts=%q", seen)
	}
	if _, err := c.Exec(ctx, "through-jump"); err != nil {
		t.Fatal(err)
	}
}

func TestKeyboardInteractiveHandshakeTimeoutCancelsPrompt(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{KeyboardInteractive: func(_ ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		_, err := challenge("Verify", "Fresh answer", []string{"Question:"}, []bool{false})
		return nil, err
	}})
	host, _ := ResolveHost(nil, "test@"+srv.Addr, nil)
	entered := make(chan struct{})
	c, err := New(Options{Host: host, HostKeys: managedOnlyPolicy(t, true), DialTimeout: 100 * time.Millisecond,
		Auth: AuthOptions{DisableAgent: true, ChallengePrompt: func(ctx context.Context, _ Challenge) (string, error) {
			close(entered)
			<-ctx.Done()
			return "", ctx.Err()
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Start(ctx); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("error=%v", err)
	}
	select {
	case <-entered:
	default:
		t.Fatal("never reached challenge")
	}
}
