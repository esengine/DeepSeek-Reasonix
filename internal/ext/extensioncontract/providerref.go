package extensioncontract

import "strings"

// PluginRefPrefix marks an identifier as owned by an extension package: a
// dependency-graph component id ("plugin/<name>") and a hosted provider ref
// ("plugin/<name>/<model>") both carry it.
const PluginRefPrefix = "plugin/"

// SplitProviderRef splits the ordinary "<name>/<model>" ref. A provider
// addresses a model with exactly one slash, so a bare name or a model
// carrying its own slash is malformed rather than merely unusual.
func SplitProviderRef(ref string) (name, model string, ok bool) {
	name, model, found := strings.Cut(ref, "/")
	if !found || name == "" || model == "" || strings.Contains(model, "/") {
		return "", "", false
	}
	return name, model, true
}

// ValidProviderTarget reports whether ref may be the target of a provider
// replacement claim: an ordinary "<name>/<model>", or the extension-hosted
// "plugin/<pluginID>/<name>/<model>". The kernel enforces this at resolve time
// and the manifest parser must agree before a package installs; pluginpkg
// cannot import the kernel, so both read the rule here.
func ValidProviderTarget(ref string) bool {
	if _, _, ok := SplitProviderRef(ref); ok {
		return true
	}
	rest, ok := strings.CutPrefix(ref, PluginRefPrefix)
	if !ok {
		return false
	}
	pluginID, nameModel, ok := strings.Cut(rest, "/")
	if !ok || pluginID == "" || strings.ContainsAny(pluginID, " \t\n") {
		return false
	}
	_, _, ok = SplitProviderRef(nameModel)
	return ok
}
