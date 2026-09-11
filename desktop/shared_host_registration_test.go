package main

import (
	"context"
	"testing"

	"reasonix/internal/plugin"
)

func TestSharedHostMCPRegistrationLifecycle(t *testing.T) {
	host := plugin.NewHost()
	defer host.Close()

	_, abandoned := beginSharedHostMCPRegistration(context.Background(), host)
	abandoned.rollback()
	if abandoned.commit() {
		t.Fatal("aborted registration must not publish")
	}

	_, published := beginSharedHostMCPRegistration(context.Background(), host)
	if !published.commit() {
		t.Fatal("active registration should publish")
	}
	published.rollback()
	if !published.commit() {
		t.Fatal("committed registration must remain published")
	}

	_, hostless := beginSharedHostMCPRegistration(context.Background(), nil)
	if !hostless.commit() {
		t.Fatal("hostless registration should publish as a no-op")
	}
}
