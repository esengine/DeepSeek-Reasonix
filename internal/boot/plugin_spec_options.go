package boot

import (
	"net/http"
	"time"

	"reasonix/internal/mcplaunch"
)

// PluginSpecOptions carries host runtime policy into plugin specifications.
type PluginSpecOptions struct {
	DefaultStartupTimeout time.Duration
	DefaultCallTimeout    time.Duration
	LaunchManager         *mcplaunch.Manager
	ConfigSource          string
	StateHome             string
	WriterRoots           []string
	ForbidReadRoots       []string
	Network               bool
	PackageOwners         map[string]string
	OAuthHTTPClient       *http.Client
}
