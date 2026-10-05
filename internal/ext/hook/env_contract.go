// What a hook actually receives from Reasonix, and the check that a hook does
// not read something it never gets. A hook that tests an unset variable is not
// merely broken: the test silently succeeds, so a guard written to block a tool
// call passes every call instead, and nothing in the transcript says so.
package hook

import (
	"os"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"reasonix/internal/base/shellparse"
)

// reasonixEnvPrefix is the namespace Reasonix defines. A reference inside it
// resolves against this contract; anything outside belongs to the user's own
// environment and is none of our business.
const reasonixEnvPrefix = "REASONIX_"

// PayloadDelivery names how a hook receives its event, for diagnostics that
// have to tell a user where to read it instead.
const PayloadDelivery = "the event payload arrives as one line of JSON on stdin"

// pluginHookEnv is the environment Reasonix injects into a plugin hook. It is
// the only definition of that set, so the reference check below cannot drift
// from what a hook is actually handed.
func pluginHookEnv(pluginRoot, pluginName, pluginVersion, reasonixHomeDir, projectRoot string) map[string]string {
	env := map[string]string{
		"REASONIX_PLUGIN_ROOT":    pluginRoot,
		"REASONIX_PLUGIN_NAME":    pluginName,
		"REASONIX_HOME":           reasonixHomeDir,
		"REASONIX_WORKSPACE_ROOT": projectRoot,
		"CLAUDE_PROJECT_DIR":      projectRoot,
		"CLAUDE_PLUGIN_ROOT":      pluginRoot,
	}
	if pluginVersion != "" {
		env["REASONIX_PLUGIN_VERSION"] = pluginVersion
	}
	return env
}

// UndefinedPayloadVars returns the Reasonix-namespaced variables a hook command
// reads that it will not be given, sorted. Config env, the plugin injection and
// the host environment all count as provided; anything left is a reference to
// something that expands to empty at every invocation.
func UndefinedPayloadVars(config HookConfig) []string {
	referenced := reasonixVarRefs(config.Command)
	if len(referenced) == 0 {
		return nil
	}
	var undefined []string
	for name := range referenced {
		if _, ok := config.Env[name]; ok {
			continue
		}
		if _, ok := os.LookupEnv(name); ok {
			continue
		}
		undefined = append(undefined, name)
	}
	slices.Sort(undefined)
	return undefined
}

// UndefinedPayloadVarsForEntry is UndefinedPayloadVars for an inspected entry.
func UndefinedPayloadVarsForEntry(entry Entry) []string {
	return UndefinedPayloadVars(entry.runtimeConfig)
}

// reasonixVarRefs collects the Reasonix-namespaced parameter expansions in a
// command. It reads the shell AST rather than scanning for "$": a $NAME inside
// single quotes is a literal, and no amount of text matching knows that.
func reasonixVarRefs(command string) map[string]struct{} {
	file, err := shellparse.ParseBash(command)
	if err != nil || file == nil {
		return nil
	}
	refs := map[string]struct{}{}
	syntax.Walk(file, func(node syntax.Node) bool {
		exp, ok := node.(*syntax.ParamExp)
		if !ok || exp.Param == nil {
			return true
		}
		if name := exp.Param.Value; strings.HasPrefix(name, reasonixEnvPrefix) {
			refs[name] = struct{}{}
		}
		return true
	})
	return refs
}
