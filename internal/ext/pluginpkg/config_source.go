package pluginpkg

import (
	"slices"

	"reasonix/internal/contract/config"
)

// The package store is where configuration learns what enabled packages add,
// so it names itself as that source; configuration never parses a package.
func init() { config.SetInstalledPackages(configPackages) }

func configPackages(home string) []config.InstalledPackage {
	installed, _ := LoadInstalled(home)
	out := make([]config.InstalledPackage, 0, len(installed))
	for _, item := range installed {
		pkg := item.Package
		servers := make(map[string]config.PackageMCPServer, len(pkg.Manifest.MCPServers))
		for name, srv := range pkg.Manifest.MCPServers {
			servers[name] = config.PackageMCPServer{
				Type: srv.Type, Command: srv.Command, Args: srv.Args, Env: srv.Env,
				URL: srv.URL, Headers: srv.Headers, AutoStart: srv.AutoStart, Tier: srv.Tier, Load: srv.Load,
			}
		}
		out = append(out, config.InstalledPackage{
			Name:        item.Installed.Name,
			Version:     item.Installed.Version,
			Root:        pkg.Root,
			Warnings:    item.Warnings,
			SkillRoots:  pkg.SkillRoots(),
			AgentRoots:  pkg.AgentRoots(),
			CommandDirs: slices.Concat(pkg.CommandRoots(), pkg.PromptRoots()),
			MCPServers:  servers,
		})
	}
	return out
}
