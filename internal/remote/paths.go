package remote

import "reasonix/internal/config"

// defaultManagedKnownHosts is the Reasonix-managed known_hosts path. It is a
// thin indirection over config so tests can leave HostKeyPolicy.ManagedPath
// empty and still get an isolated file under REASONIX_HOME.
func defaultManagedKnownHosts() string {
	return config.RemoteKnownHostsPath()
}
