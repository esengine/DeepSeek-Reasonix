package parity

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var bridgeMethodPattern = regexp.MustCompile(`^\s{2}([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

func TestManifestCoversDesktopBridge(t *testing.T) {
	entries := Entries()
	if err := Validate(entries, readBridgeMethods(t)); err != nil {
		t.Fatalf("frontend AppBindings: %v", err)
	}
	if err := Validate(entries, readWailsAppMethods(t)); err != nil {
		t.Fatalf("Wails *App methods: %v", err)
	}
}

func TestFrontendBridgeMatchesWailsAppMethods(t *testing.T) {
	frontend := readBridgeMethods(t)
	wails := readWailsAppMethods(t)
	if err := validateSameMethods(frontend, wails); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsNewUnclassifiedBridgeMethod(t *testing.T) {
	methods := append(readBridgeMethods(t), "NewUserReachableMethod")
	wantValidationError(t, Entries(), methods, "NewUserReachableMethod is unclassified")
}

func TestValidateRejectsMissingClassification(t *testing.T) {
	entries := Entries()
	removed := entries[0].Method
	entries = entries[1:]
	wantValidationError(t, entries, readBridgeMethods(t), removed+" is unclassified")
}

func TestValidateRejectsDuplicateClassification(t *testing.T) {
	entries := Entries()
	duplicate := entries[0]
	entries = append(entries, duplicate)
	wantValidationError(t, entries, readBridgeMethods(t), duplicate.Method+" is classified more than once")
}

func TestValidateRejectsInvalidCategory(t *testing.T) {
	entries := Entries()
	entries[0].Category = Category("remote-ish")
	wantValidationError(t, entries, readBridgeMethods(t), `invalid category "remote-ish"`)
}

func TestValidateRejectsStaleManifestMethod(t *testing.T) {
	entries := append(Entries(), Entry{Method: "RemovedBridgeMethod", Category: DesktopLocal})
	wantValidationError(t, entries, readBridgeMethods(t), "RemovedBridgeMethod is absent from the bridge")
}

func TestCriticalV1MethodsStayInRemoteSurface(t *testing.T) {
	critical := []string{
		"SubmitToTab",
		"RunShellForTab",
		"SteerForTab",
		"CancelTab",
		"ApproveTab",
		"AnswerQuestionForTab",
		"SetGoalForTab",
		"ResumeGoalForTab",
		"ClearGoalForTab",
		"CompactForTab",
		"NewSessionForTab",
		"ClearSessionForTab",
		"HistoryPageForTab",
		"CheckpointsForTab",
		"RewindForTab",
		"ForkForTab",
		"SummarizeFromForTab",
		"SummarizeUpToForTab",
		"ListSessions",
		"ListTrashedSessions",
		"DeleteSession",
		"DeleteRecoveryCopy",
		"RestoreSession",
		"PurgeTrashedSession",
		"PurgeRecoveryCopy",
		"RenameSession",
		"ScanPromptHistory",
		"ListWorkspaces",
		"SwitchWorkspace",
		"RemoveWorkspace",
		"ContextUsageForTab",
		"BalanceForTab",
		"JobsForTab",
		"ToolResultForTab",
		"MetaForTab",
		"AutoResearchStatus",
		"AutoResearchList",
		"AutoResearchFindings",
		"AutoResearchRecordEvidence",
		"Commands",
		"Capabilities",
		"MCPServers",
		"SkillsSettings",
		"Plugins",
		"SlashArgs",
		"ListDirForTab",
		"SearchFileRefsForTab",
		"ReadFileForTab",
		"WorkspaceChanges",
		"WorkspaceGitHistory",
		"WorkspaceGitCommitDetail",
		"ModelsForTab",
		"SetModelForTab",
		"EffortForTab",
		"SetEffortForTab",
		"SetTokenModeForTab",
		"MemoryForTab",
		"MemorySuggestionsForTab",
		"AcceptMemorySuggestionForTab",
		"AcceptSkillSuggestionForTab",
		"RememberForTab",
		"ForgetForTab",
		"SaveDocForTab",
		"Settings",
		"OpenTopicSession",
		"CreateTopic",
		"RenameTopic",
		"DeleteTopic",
		"TrashTopic",
	}

	byMethod := make(map[string]Category)
	for _, entry := range Entries() {
		byMethod[entry.Method] = entry.Category
	}
	for _, method := range critical {
		category, ok := byMethod[method]
		if !ok {
			t.Errorf("critical V1 bridge method %s is missing from the manifest", method)
			continue
		}
		if category != SharedRuntime && category != HostReadonly {
			t.Errorf("critical V1 bridge method %s is classified as %s", method, category)
		}
	}
}

func wantValidationError(t *testing.T, entries []Entry, methods []string, contains string) {
	t.Helper()
	err := Validate(entries, methods)
	if err == nil {
		t.Fatalf("Validate() succeeded, want error containing %q", contains)
	}
	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("Validate() error = %q, want it to contain %q", err, contains)
	}
}

func readBridgeMethods(t *testing.T) []string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller did not report the test file")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "desktop", "frontend", "src", "lib", "bridge.ts")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open Desktop bridge: %v", err)
	}
	defer f.Close()

	const declaration = "export interface AppBindings {"
	inInterface := false
	var methods []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !inInterface {
			if strings.TrimSpace(line) == declaration {
				inInterface = true
			}
			continue
		}
		if line == "}" {
			break
		}
		match := bridgeMethodPattern.FindStringSubmatch(line)
		if len(match) == 2 {
			methods = append(methods, match[1])
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan Desktop bridge: %v", err)
	}
	if !inInterface {
		t.Fatalf("Desktop bridge declaration %q not found", declaration)
	}
	if len(methods) == 0 {
		t.Fatal("Desktop bridge contains no methods")
	}
	return methods
}

func readWailsAppMethods(t *testing.T) []string {
	t.Helper()
	desktopDir := desktopSourceDir(t)
	entries, err := os.ReadDir(desktopDir)
	if err != nil {
		t.Fatalf("read Desktop source directory: %v", err)
	}

	methods := make(map[string]bool)
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(desktopDir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse Desktop source %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || len(function.Recv.List) != 1 || !ast.IsExported(function.Name.Name) {
				continue
			}
			if receiverTypeName(function.Recv.List[0].Type) == "App" {
				methods[function.Name.Name] = true
			}
		}
	}
	if len(methods) == 0 {
		t.Fatal("Desktop Wails App contains no exported methods")
	}
	out := make([]string, 0, len(methods))
	for method := range methods {
		out = append(out, method)
	}
	sort.Strings(out)
	return out
}

func receiverTypeName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return receiverTypeName(value.X)
	case *ast.IndexExpr:
		return receiverTypeName(value.X)
	case *ast.IndexListExpr:
		return receiverTypeName(value.X)
	default:
		return ""
	}
}

func desktopSourceDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller did not report the test file")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "desktop")
}

func validateSameMethods(frontend, wails []string) error {
	frontendSet := make(map[string]bool, len(frontend))
	for _, method := range frontend {
		frontendSet[method] = true
	}
	wailsSet := make(map[string]bool, len(wails))
	for _, method := range wails {
		wailsSet[method] = true
	}
	var problems []string
	for method := range wailsSet {
		if !frontendSet[method] {
			problems = append(problems, method+" is exported by Wails but absent from frontend AppBindings")
		}
	}
	for method := range frontendSet {
		if !wailsSet[method] {
			problems = append(problems, method+" is present in frontend AppBindings but absent from Wails App")
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("Desktop bridge drift: %s", strings.Join(problems, "; "))
}
