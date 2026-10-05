package appupdate

import (
	"context"
	"testing"

	"reasonix/internal/base/testenv"
)

type stubOwner struct{}

func (stubOwner) PrepareForUpdate(context.Context) error    { return nil }
func (stubOwner) RelaunchAfterUpdate(context.Context) error { return nil }
func (stubOwner) EndApplication(context.Context)            {}

// The gate is a nil interface, not a nil pointer inside one. Returning
// *capability here would make serve's `Update == nil` false, register the
// routes, and hand a kernel that owns no application the power to retire
// somebody's rollback material -- with nothing failing to say so.
func TestNothingOwningTheApplicationYieldsNoCapabilityAtAll(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	if got := New(Options{Running: "v1.0.0"}); got != nil {
		t.Fatalf("New(nil) = %#v, want a nil Capability", got)
	}
}

// A launch that did not boot from an update has nothing to retire. It says so
// by succeeding: an application cannot vouch for a replacement it is not
// running, and refusing here would make every ordinary launch look broken.
func TestALaunchThatDidNotBootFromAnUpdateRetiresNothing(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	host := New(Options{Owner: stubOwner{}, Running: "v1.0.0"})
	if host == nil {
		t.Fatal("an owned application got no capability")
	}
	if err := host.AcknowledgeLaunchHealth(); err != nil {
		t.Fatalf("AcknowledgeLaunchHealth: %v", err)
	}
}
