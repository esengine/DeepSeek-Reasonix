package control

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/i18n"
	"reasonix/internal/memory"
)

const memoryCommandUsage = "usage: /memory [recall|revisions <id-or-name>|restore <id-or-name> <revision>|archived|recover <archive-path>|instructions]"

// MemoryCompletionData returns stable references for structured /memory
// completion. IDs come first because they remain unambiguous if a fact is
// renamed or the same slug exists in multiple scopes.
func MemoryCompletionData(set *memory.Set) (refs, archives []string) {
	if set == nil {
		return []string{}, []string{}
	}
	seen := map[string]bool{}
	for _, fact := range set.Store.ListAll() {
		for _, ref := range []string{fact.ID, fact.Name} {
			ref = strings.TrimSpace(ref)
			if ref == "" || seen[ref] {
				continue
			}
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	for _, archived := range set.Store.ListArchived() {
		if path := strings.TrimSpace(archived.Path); path != "" {
			archives = append(archives, path)
		}
	}
	if refs == nil {
		refs = []string{}
	}
	if archives == nil {
		archives = []string{}
	}
	return refs, archives
}

// MemoryCommandText executes one session /memory management command and returns
// its diagnostic text. Both Submit-based frontends and the chat TUI use this
// function so reads and explicit recovery mutations follow one protocol.
func MemoryCommandText(api MemoryControl, input string) string {
	if api == nil {
		return i18n.M.ListMemoryNone
	}
	subcommand, rest := parseMemoryCommand(input)
	switch subcommand {
	case "", "list", "summary":
		return RenderMemorySummary(api.Memory(), time.Now().UTC())
	case "recall":
		if rest != "" {
			return "usage: /memory recall"
		}
		return renderMemoryRecall(api.LastMemoryRecall())
	case "revisions":
		ref, err := singleMemoryArgument(rest)
		if err != nil {
			return "usage: /memory revisions <id-or-name>"
		}
		return renderMemoryRevisions(api, ref)
	case "restore":
		ref, revision, err := parseMemoryRestore(rest)
		if err != nil {
			return "usage: /memory restore <id-or-name> <revision>"
		}
		restored, err := api.RestoreMemory(ref, revision)
		if err != nil {
			return "memory restore: " + err.Error()
		}
		return fmt.Sprintf("restored %s as revision=%d id=%s scope=%s",
			restored.Name, restored.Revision, restored.ID, memory.NormalizeFactScope(string(restored.Scope)))
	case "archived", "archives":
		if rest != "" {
			return "usage: /memory archived"
		}
		return renderMemoryArchives(api.Memory())
	case "recover":
		archivePath, err := singleMemoryArgument(rest)
		if err != nil {
			return "usage: /memory recover <archive-path>"
		}
		restored, err := api.RestoreArchivedMemory(archivePath)
		if err != nil {
			return "memory recover: " + err.Error()
		}
		return fmt.Sprintf("recovered %s as revision=%d id=%s scope=%s",
			restored.Name, restored.Revision, restored.ID, memory.NormalizeFactScope(string(restored.Scope)))
	case "instructions":
		if rest != "" {
			return "usage: /memory instructions"
		}
		return renderMemoryInstructions(api.Memory())
	default:
		return "unknown /memory subcommand " + subcommand + "\n" + memoryCommandUsage
	}
}

func parseMemoryCommand(input string) (subcommand, rest string) {
	input = strings.TrimSpace(input)
	if after, ok := strings.CutPrefix(input, "/memory"); ok {
		input = strings.TrimSpace(after)
	}
	if input == "" {
		return "", ""
	}
	if at := strings.IndexAny(input, " \t"); at >= 0 {
		return strings.ToLower(input[:at]), strings.TrimSpace(input[at+1:])
	}
	return strings.ToLower(input), ""
}

func singleMemoryArgument(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("missing argument")
	}
	if len(input) >= 2 {
		if (input[0] == '"' && input[len(input)-1] == '"') ||
			(input[0] == '\'' && input[len(input)-1] == '\'') {
			input = strings.TrimSpace(input[1 : len(input)-1])
		}
	}
	if input == "" {
		return "", fmt.Errorf("missing argument")
	}
	return input, nil
}

func parseMemoryRestore(input string) (string, int, error) {
	fields := strings.Fields(input)
	if len(fields) != 2 {
		return "", 0, fmt.Errorf("expected reference and revision")
	}
	revision, err := strconv.Atoi(fields[1])
	if err != nil || revision <= 0 {
		return "", 0, fmt.Errorf("revision must be positive")
	}
	return fields[0], revision, nil
}

// RenderMemorySummary renders the zero-configuration overview shown by bare
// /memory. It intentionally uses ListAll so project/global collisions remain
// observable instead of being hidden by the legacy name-based merge.
func RenderMemorySummary(set *memory.Set, now time.Time) string {
	if set == nil {
		return i18n.M.ListMemoryNone
	}
	facts := set.Store.ListAll()
	archives := set.Store.ListArchived()
	if len(set.Docs) == 0 && len(facts) == 0 && len(archives) == 0 {
		return i18n.M.ListMemoryNone
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var b strings.Builder
	b.WriteString("memory\n")
	if len(set.Docs) > 0 {
		b.WriteString("\ninstructions (low -> high precedence)\n")
		for index, doc := range set.Docs {
			fmt.Fprintf(&b, "  precedence=%d scope=%s path=%s\n", index+1, doc.Scope, doc.Path)
			if strings.TrimSpace(doc.Directory) != "" {
				fmt.Fprintf(&b, "    directory=%s\n", doc.Directory)
			}
			for _, imported := range doc.Imports {
				fmt.Fprintf(&b, "    import=%s\n", imported.Path)
			}
		}
	}
	if len(facts) > 0 {
		b.WriteString("\n" + i18n.M.ListMemorySaved + "\n")
		for _, fact := range facts {
			fmt.Fprintf(&b, "  [%s](%s.md)\n", memoryDisplayTitle(fact.Title, fact.Name), fact.Name)
			fmt.Fprintf(&b, "    id=%s\n", fact.ID)
			fmt.Fprintf(&b, "    revision=%d scope=%s type=%s freshness=%s\n",
				fact.Revision,
				memory.NormalizeFactScope(string(fact.Scope)), memory.NormalizeType(string(fact.Type)),
				memory.FreshnessFor(fact, now))
			if description := memoryOneLine(fact.Description); description != "" {
				fmt.Fprintf(&b, "    description=%s\n", description)
			}
		}
	}
	if len(archives) > 0 {
		b.WriteByte('\n')
		b.WriteString(renderMemoryArchives(set) + "\n")
	}
	if len(facts) > 0 || len(archives) > 0 {
		for _, dir := range memoryStoreDirs(set.Store) {
			fmt.Fprintf(&b, "  stored under %s\n", dir)
		}
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(i18n.M.MemoryEditHint))
	b.WriteString("\n")
	b.WriteString(memoryCommandUsage)
	return strings.TrimRight(b.String(), "\n")
}

func renderMemoryRecall(recall memory.RecallResult) string {
	if strings.TrimSpace(recall.Query) == "" && len(recall.Hits) == 0 && recall.Suppressed == "" {
		return "last memory recall: none"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "last memory recall\n  query=%s\n", memoryOneLine(recall.Query))
	fmt.Fprintf(&b, "  budget=%d/%d omitted=%d", recall.UsedChars, recall.CharBudget, recall.Omitted)
	if recall.Suppressed != "" {
		fmt.Fprintf(&b, " suppressed=%s", memoryOneLine(recall.Suppressed))
	}
	b.WriteByte('\n')
	for _, hit := range recall.Hits {
		fact := hit.Memory
		fmt.Fprintf(&b, "  id=%s revision=%d scope=%s type=%s freshness=%s score=%.3f\n",
			fact.ID, fact.Revision, memory.NormalizeFactScope(string(fact.Scope)),
			memory.NormalizeType(string(fact.Type)), hit.Freshness, hit.Score)
		fmt.Fprintf(&b, "    name=%s reason=%s\n", fact.Name, memoryOneLine(hit.Reason))
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderMemoryRevisions(api MemoryControl, ref string) string {
	set := api.Memory()
	if set == nil {
		return i18n.M.ListMemoryNone
	}
	active, ok := set.Store.Read(ref)
	if !ok {
		return fmt.Sprintf("memory revisions: memory %q not found", ref)
	}
	revisions := append([]memory.Memory{active}, api.MemoryRevisions(active.ID)...)
	sort.SliceStable(revisions, func(i, j int) bool {
		return revisions[i].Revision > revisions[j].Revision
	})
	var b strings.Builder
	fmt.Fprintf(&b, "memory revisions id=%s name=%s\n", active.ID, active.Name)
	for index, revision := range revisions {
		status := "history"
		if index == 0 && revision.Revision == active.Revision {
			status = "active"
		}
		fmt.Fprintf(&b, "  revision=%d %s updated=%s scope=%s type=%s description=%s\n",
			revision.Revision, status, formatMemoryTime(revision.UpdatedAt),
			memory.NormalizeFactScope(string(revision.Scope)), memory.NormalizeType(string(revision.Type)),
			memoryOneLine(revision.Description))
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderMemoryArchives(set *memory.Set) string {
	if set == nil {
		return i18n.M.ListMemoryNone
	}
	archives := set.Store.ListArchived()
	if len(archives) == 0 {
		return "archived memories: none"
	}
	var b strings.Builder
	b.WriteString(i18n.M.ListMemoryArchived + "\n")
	for _, archived := range archives {
		fmt.Fprintf(&b, "  [%s](%s)\n", memoryDisplayTitle(archived.Title, archived.Name), archived.Path)
		fmt.Fprintf(&b, "    id=%s\n", archived.ID)
		fmt.Fprintf(&b, "    revision=%d scope=%s type=%s archived=%s\n", archived.Revision,
			memory.NormalizeFactScope(string(archived.Scope)), memory.NormalizeType(string(archived.Type)),
			formatMemoryTime(archived.ArchivedAt))
		if description := memoryOneLine(archived.Description); description != "" {
			fmt.Fprintf(&b, "    description=%s\n", description)
		}
	}
	b.WriteString("recover with /memory recover <archive-path>")
	return strings.TrimRight(b.String(), "\n")
}

func renderMemoryInstructions(set *memory.Set) string {
	if set == nil || (len(set.Docs) == 0 && len(set.InstructionDiagnostics) == 0) {
		return "instructions: none"
	}
	var b strings.Builder
	b.WriteString("instructions (low -> high precedence)\n")
	for index, doc := range set.Docs {
		fmt.Fprintf(&b, "  precedence=%d scope=%s path=%s\n", index+1, doc.Scope, doc.Path)
		if strings.TrimSpace(doc.Directory) != "" {
			fmt.Fprintf(&b, "    directory=%s\n", doc.Directory)
		}
		for _, imported := range doc.Imports {
			fmt.Fprintf(&b, "    import=%s source=%s\n", imported.Path, imported.SourcePath)
		}
	}
	if len(set.InstructionDiagnostics) > 0 {
		b.WriteString("diagnostics\n")
		for _, diagnostic := range set.InstructionDiagnostics {
			source := diagnostic.SourcePath
			if source == "" {
				source = diagnostic.Path
			}
			if diagnostic.Line > 0 {
				source += ":" + strconv.Itoa(diagnostic.Line)
			}
			fmt.Fprintf(&b, "  code=%s source=%s path=%s message=%s\n",
				diagnostic.Code, source, diagnostic.Path, memoryOneLine(diagnostic.Message))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func memoryStoreDirs(store memory.Store) []string {
	var dirs []string
	seen := map[string]bool{}
	for _, dir := range []string{store.Dir, store.GlobalDir} {
		dir = strings.TrimSpace(dir)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	return dirs
}

func formatMemoryTime(value time.Time) string {
	if value.IsZero() {
		return "unknown"
	}
	return value.UTC().Format(time.RFC3339)
}

func memoryDisplayTitle(title, name string) string {
	if title = memoryOneLine(title); title != "" {
		return title
	}
	return strings.ReplaceAll(name, "-", " ")
}

func memoryOneLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
