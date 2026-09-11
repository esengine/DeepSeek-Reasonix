package qq

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestQQIdentifyIntentProfiles(t *testing.T) {
	const shared = 1<<0 | 1<<1 | 1<<12 | 1<<25
	if qqPrivateIdentifyIntents != shared|1<<9 {
		t.Fatalf("private intents = %b, want %b", qqPrivateIdentifyIntents, shared|1<<9)
	}
	if qqPublicIdentifyIntents != shared|1<<30 {
		t.Fatalf("public intents = %b, want %b", qqPublicIdentifyIntents, shared|1<<30)
	}
	if qqPrivateIdentifyIntents&(1<<26) != 0 || qqPublicIdentifyIntents&(1<<26) != 0 {
		t.Fatal("identify profiles contain privileged INTERACTION intent")
	}
}

func TestQQIntentFallbackPreservesPrivateProfileWhenAuthorized(t *testing.T) {
	selected := qqPrivateIdentifyIntents
	var attempts []int
	downgraded, err := connectQQGatewayWithIntentFallback(context.Background(), "token", &selected, func(_ context.Context, _ string, intents int) error {
		attempts = append(attempts, intents)
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if downgraded {
		t.Fatal("authorized private profile was downgraded")
	}
	if selected != qqPrivateIdentifyIntents || len(attempts) != 1 || attempts[0] != qqPrivateIdentifyIntents {
		t.Fatalf("selected = %b, attempts = %v", selected, attempts)
	}
}

func TestQQIntentFallbackDowngradesOnceAndRemembersPublicProfile(t *testing.T) {
	selected := qqPrivateIdentifyIntents
	var attempts []int
	connect := func(_ context.Context, _ string, intents int) error {
		attempts = append(attempts, intents)
		if intents == qqPrivateIdentifyIntents {
			return errQQIdentifyRejected
		}
		return nil
	}

	downgraded, err := connectQQGatewayWithIntentFallback(context.Background(), "token", &selected, connect, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !downgraded || selected != qqPublicIdentifyIntents {
		t.Fatalf("downgraded = %v, selected = %b", downgraded, selected)
	}
	wantAttempts := []int{qqPrivateIdentifyIntents, qqPublicIdentifyIntents}
	if fmt.Sprint(attempts) != fmt.Sprint(wantAttempts) {
		t.Fatalf("attempts = %v, want %v", attempts, wantAttempts)
	}

	attempts = nil
	downgraded, err = connectQQGatewayWithIntentFallback(context.Background(), "token", &selected, connect, nil)
	if err != nil {
		t.Fatal(err)
	}
	if downgraded || len(attempts) != 1 || attempts[0] != qqPublicIdentifyIntents {
		t.Fatalf("second attempt downgraded = %v, attempts = %v", downgraded, attempts)
	}
}

func TestQQIntentFallbackDoesNotReconnectAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	selected := qqPrivateIdentifyIntents
	attempts := 0
	_, err := connectQQGatewayWithIntentFallback(ctx, "token", &selected, func(_ context.Context, _ string, _ int) error {
		attempts++
		cancel()
		return errQQIdentifyRejected
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if attempts != 1 || selected != qqPrivateIdentifyIntents {
		t.Fatalf("attempts = %d, selected = %b", attempts, selected)
	}
}

func TestQQReadyPayloadClassifiesInvalidSession(t *testing.T) {
	err := validateQQReadyPayload(gatewayPayload{Op: opInvalid})
	if !errors.Is(err, errQQIdentifyRejected) {
		t.Fatalf("validateQQReadyPayload() error = %v, want identify rejection", err)
	}
}
