package cli

import (
	"fmt"
	"os"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
	"reasonix/internal/model/providerbroker"
	"reasonix/internal/platform/remote"
	"reasonix/internal/platform/remote/bootstrap"
	"reasonix/internal/platform/remote/forward"
)

// publishBroker publishes this process's provider broker on the host's
// loopback, so the serve launched there resolves models here. The zero Broker
// answers a host set to resolve its own, or a broker that would not listen:
// neither is a reason to refuse the connection, since it is how every host
// worked before there was a broker. The stop func is never nil.
func publishBroker(client *remote.Client, entry config.RemoteHostEntry) (bootstrap.Broker, func()) {
	none := func() {}
	if entry.ProviderMode() == config.RemoteProviderRemote {
		return bootstrap.Broker{}, none
	}
	local, err := providerbroker.Listen(boot.LiveProviderResolver{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: the provider broker did not start (%v); the remote host will use its own credentials\n", err)
		return bootstrap.Broker{}, none
	}
	bound, err := client.Forwards().Add(forward.Spec{
		Name:       "provider-broker",
		Direction:  forward.Remote,
		BindAddr:   "127.0.0.1:0",
		TargetAddr: local.Addr,
	})
	if err != nil {
		_ = local.Close()
		fmt.Fprintf(os.Stderr, "warning: could not publish the provider broker on the remote (%v); it will use its own credentials\n", err)
		return bootstrap.Broker{}, none
	}
	return bootstrap.Broker{Addr: bound, Token: local.Token}, func() { _ = local.Close() }
}
