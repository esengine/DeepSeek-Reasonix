package main

import (
	"context"
	"testing"

	"reasonix/internal/runtimeapi"
)

type remotePluginsRuntime struct {
	*remoteWorkbenchTestRuntime
	catalog runtimeapi.SessionCatalog
}

func (r *remotePluginsRuntime) SessionCatalog(_ context.Context, input runtimeapi.SessionCatalogInput) (runtimeapi.SessionCatalog, error) {
	if input.Session != r.created.Session {
		return runtimeapi.SessionCatalog{}, context.Canceled
	}
	return r.catalog, nil
}

func TestRemotePluginsUsesSafeHostCatalogWithoutLocalPaths(t *testing.T) {
	runtime := &remotePluginsRuntime{
		remoteWorkbenchTestRuntime: newRemoteWorkbenchTestRuntime(),
		catalog: runtimeapi.SessionCatalog{Plugins: []runtimeapi.PluginCatalogItem{
			{ID: "plugin-opaque", Name: "remote-plugin", Enabled: true},
		}},
	}
	target := TargetDescriptor{Kind: TargetRemote, ID: "host_plugins", Label: "Host plugins"}
	app, _ := newRemoteWorkbenchTestApp(t, target, runtime, nil)
	if _, err := app.CreateRemoteWorkspaceSession(RemoteCreateWorkspaceSessionInput{PrimaryDirectoryRef: "dir_primary", TopicTitle: "Remote plugins"}); err != nil {
		t.Fatal(err)
	}

	plugins := app.Plugins()
	if len(plugins) != 1 || plugins[0].Name != "remote-plugin" || !plugins[0].Enabled {
		t.Fatalf("Remote plugins = %#v", plugins)
	}
	if plugins[0].Root != "" || plugins[0].Source != "" || len(plugins[0].CommandDetails) != 0 || len(plugins[0].MCPServerDetails) != 0 {
		t.Fatalf("Remote plugin leaked a Desktop-local detail: %#v", plugins[0])
	}
}
