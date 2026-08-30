package cli

import "testing"

func TestBuiltinSlashRegistryHasUniqueNamesAndAliases(t *testing.T) {
	seen := map[string]string{}
	for _, spec := range builtinSlashSpecs() {
		if spec.name == "" || spec.name[0] != '/' {
			t.Fatalf("invalid built-in slash name %q", spec.name)
		}
		for _, name := range append([]string{spec.name}, spec.aliases...) {
			if owner, exists := seen[name]; exists {
				t.Fatalf("slash name %q belongs to both %q and %q", name, owner, spec.name)
			}
			seen[name] = spec.name
			if got := canonicalBuiltinSlashCommand(name); got != spec.name {
				t.Fatalf("canonical command for %q = %q, want %q", name, got, spec.name)
			}
		}
	}
}

func TestBuiltinSlashCompletionAndHelpComeFromRegistry(t *testing.T) {
	specs := builtinSlashSpecs()
	completion := builtinSlashItems()
	completionByName := make(map[string]compItem, len(completion))
	for _, item := range completion {
		completionByName[item.label] = item
	}
	visible := 0
	for _, spec := range specs {
		if !spec.hidden {
			visible++
		}
	}
	if len(completion) != visible {
		t.Fatalf("completion items = %d, visible specs = %d", len(completion), visible)
	}
	help := builtinSlashHelpItems()
	helpNames := map[string]bool{}
	for _, item := range help {
		helpNames[item.label] = true
	}
	for _, spec := range specs {
		item, discoverable := completionByName[spec.name]
		if discoverable == spec.hidden {
			t.Fatalf("completion visibility for %q = %v, hidden = %v", spec.name, discoverable, spec.hidden)
		}
		if helpNames[spec.name] != spec.showInHelp {
			t.Fatalf("help visibility for %q = %v, want %v", spec.name, helpNames[spec.name], spec.showInHelp)
		}
		if spec.hidden {
			continue
		}
		if item.label != spec.name || item.insert != spec.insert || item.hint != spec.hint || item.descend != spec.descend {
			t.Fatalf("completion item for %q drifted: %+v", spec.name, item)
		}
	}
}
