package main

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	contractPath = ".signpath/contracts/release-signing.yml"
	projectSlug  = "DeepSeek-Reasonix"
	policySlug   = "release-signing"
	repository   = "https://github.com/esengine/DeepSeek-Reasonix.git"
)

var expectedBranches = []string{"main-v2"}

type releaseSigningContract struct {
	Version                 int      `yaml:"version"`
	ProjectSlug             string   `yaml:"project_slug"`
	SigningPolicySlug       string   `yaml:"signing_policy_slug"`
	RepositoryURL           string   `yaml:"repository_url"`
	AllowedBranchNames      []string `yaml:"allowed_branch_names"`
	AllowedBuildDefinitions []string `yaml:"allowed_build_definitions"`
	FingerprintFiles        []string `yaml:"fingerprint_files"`
}

type workflowInfo struct {
	externallyTriggered bool
	directSigning       bool
	calls               []string
}

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fatalf("usage: signpath-contract <validate|fingerprint> [repository-root]")
	}
	root := "."
	if len(os.Args) == 3 {
		root = os.Args[2]
	}
	contract, err := loadAndValidate(root)
	if err != nil {
		fatalf("%v", err)
	}

	switch os.Args[1] {
	case "validate":
		fmt.Println("SignPath release signing contract is valid")
	case "fingerprint":
		fingerprint, err := contractFingerprint(root, contract)
		if err != nil {
			fatalf("fingerprint contract: %v", err)
		}
		fmt.Printf("v1:%x\n", fingerprint)
	default:
		fatalf("unknown command %q", os.Args[1])
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "signpath-contract: "+format+"\n", args...)
	os.Exit(1)
}

func loadAndValidate(root string) (releaseSigningContract, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(contractPath)))
	if err != nil {
		return releaseSigningContract{}, fmt.Errorf("read %s: %w", contractPath, err)
	}
	var contract releaseSigningContract
	if err := yaml.Unmarshal(data, &contract); err != nil {
		return releaseSigningContract{}, fmt.Errorf("parse %s: %w", contractPath, err)
	}
	if err := validateContract(root, contract); err != nil {
		return releaseSigningContract{}, err
	}
	return contract, nil
}

func validateContract(root string, contract releaseSigningContract) error {
	if contract.Version != 1 {
		return fmt.Errorf("%s version is %d, want 1", contractPath, contract.Version)
	}
	if contract.ProjectSlug != projectSlug {
		return fmt.Errorf("project_slug is %q, want %q", contract.ProjectSlug, projectSlug)
	}
	if contract.SigningPolicySlug != policySlug {
		return fmt.Errorf("signing_policy_slug is %q, want %q", contract.SigningPolicySlug, policySlug)
	}
	if contract.RepositoryURL != repository {
		return fmt.Errorf("repository_url is %q, want %q", contract.RepositoryURL, repository)
	}
	if err := requireExactSet("allowed_branch_names", contract.AllowedBranchNames, expectedBranches); err != nil {
		return err
	}
	if len(contract.AllowedBuildDefinitions) == 0 {
		return errors.New("allowed_build_definitions must not be empty")
	}
	for _, value := range append(append([]string{}, contract.AllowedBranchNames...), contract.AllowedBuildDefinitions...) {
		if strings.ContainsAny(value, "*?[") {
			return fmt.Errorf("SignPath allowlists must not contain wildcard %q", value)
		}
	}
	for _, name := range contract.AllowedBuildDefinitions {
		if err := validateRepositoryPath(name); err != nil {
			return fmt.Errorf("invalid allowed build definition %q: %w", name, err)
		}
		if !strings.HasPrefix(name, ".github/workflows/") {
			return fmt.Errorf("allowed build definition %q is outside .github/workflows", name)
		}
	}
	if err := requireUnique("allowed_build_definitions", contract.AllowedBuildDefinitions); err != nil {
		return err
	}
	if len(contract.FingerprintFiles) == 0 {
		return errors.New("fingerprint_files must not be empty")
	}
	seen := make(map[string]bool, len(contract.FingerprintFiles))
	for _, name := range contract.FingerprintFiles {
		if err := validateRepositoryPath(name); err != nil {
			return fmt.Errorf("invalid fingerprint file %q: %w", name, err)
		}
		if seen[name] {
			return fmt.Errorf("duplicate fingerprint file %q", name)
		}
		seen[name] = true
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return fmt.Errorf("fingerprint file %q: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("fingerprint file %q is not a regular file", name)
		}
	}

	reachable, err := discoverTopLevelSigningWorkflows(root)
	if err != nil {
		return err
	}
	if err := requireExactSet("top-level workflows that reach SignPath", reachable, contract.AllowedBuildDefinitions); err != nil {
		return fmt.Errorf("SignPath build-definition drift: %w", err)
	}
	return nil
}

func requireExactSet(name string, got, want []string) error {
	gotCopy := append([]string(nil), got...)
	wantCopy := append([]string(nil), want...)
	sort.Strings(gotCopy)
	sort.Strings(wantCopy)
	if len(gotCopy) != len(wantCopy) {
		return fmt.Errorf("%s is %v, want %v", name, got, want)
	}
	for i := range gotCopy {
		if gotCopy[i] != wantCopy[i] {
			return fmt.Errorf("%s is %v, want %v", name, got, want)
		}
		if i > 0 && gotCopy[i] == gotCopy[i-1] {
			return fmt.Errorf("%s contains duplicate %q", name, gotCopy[i])
		}
	}
	return nil
}

func requireUnique(name string, values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return fmt.Errorf("%s contains duplicate %q", name, value)
		}
		seen[value] = true
	}
	return nil
}

func validateRepositoryPath(name string) error {
	if name == "" || filepath.IsAbs(filepath.FromSlash(name)) {
		return errors.New("path must be non-empty and repository-relative")
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return errors.New("path escapes the repository")
	}
	if filepath.ToSlash(clean) != name {
		return errors.New("path must be normalized with forward slashes")
	}
	return nil
}

func discoverTopLevelSigningWorkflows(root string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	workflows := make(map[string]workflowInfo, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read workflow %s: %w", path, err)
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		name = filepath.ToSlash(name)
		info, err := parseWorkflow(data)
		if err != nil {
			return nil, fmt.Errorf("parse workflow %s: %w", name, err)
		}
		workflows[name] = info
	}

	state := make(map[string]uint8, len(workflows))
	memo := make(map[string]bool, len(workflows))
	var reachesSigning func(string) (bool, error)
	reachesSigning = func(name string) (bool, error) {
		if state[name] == 2 {
			return memo[name], nil
		}
		if state[name] == 1 {
			return false, fmt.Errorf("reusable workflow cycle includes %s", name)
		}
		info, ok := workflows[name]
		if !ok {
			return false, fmt.Errorf("workflow calls missing local workflow %s", name)
		}
		state[name] = 1
		reachable := info.directSigning
		for _, called := range info.calls {
			calledReachable, err := reachesSigning(called)
			if err != nil {
				return false, err
			}
			reachable = reachable || calledReachable
		}
		state[name] = 2
		memo[name] = reachable
		return reachable, nil
	}

	var result []string
	for name, info := range workflows {
		if !info.externallyTriggered {
			continue
		}
		reachable, err := reachesSigning(name)
		if err != nil {
			return nil, err
		}
		if reachable {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result, nil
}

func parseWorkflow(data []byte) (workflowInfo, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return workflowInfo{}, err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return workflowInfo{}, errors.New("workflow root must be a mapping")
	}
	root := document.Content[0]
	on := mappingValue(root, "on")
	jobs := mappingValue(root, "jobs")
	if on == nil || jobs == nil || jobs.Kind != yaml.MappingNode {
		return workflowInfo{}, errors.New("workflow must contain on and jobs mappings")
	}

	info := workflowInfo{externallyTriggered: hasExternalTrigger(on)}
	for i := 1; i < len(jobs.Content); i += 2 {
		job := jobs.Content[i]
		if job.Kind != yaml.MappingNode {
			continue
		}
		if uses := mappingScalar(job, "uses"); strings.HasPrefix(uses, "./.github/workflows/") {
			info.calls = append(info.calls, strings.TrimPrefix(uses, "./"))
		}
		if nodeContains(job, "secrets.SIGNPATH_API_TOKEN") {
			info.directSigning = true
		}
		steps := mappingValue(job, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			if step.Kind != yaml.MappingNode {
				continue
			}
			if strings.HasPrefix(mappingScalar(step, "uses"), "signpath/github-action-submit-signing-request@") {
				info.directSigning = true
			}
		}
	}
	sort.Strings(info.calls)
	return info, nil
}

func nodeContains(node *yaml.Node, text string) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.ScalarNode && strings.Contains(node.Value, text) {
		return true
	}
	for _, child := range node.Content {
		if nodeContains(child, text) {
			return true
		}
	}
	return false
}

func hasExternalTrigger(node *yaml.Node) bool {
	switch node.Kind {
	case yaml.ScalarNode:
		return node.Value != "workflow_call"
	case yaml.SequenceNode:
		for _, child := range node.Content {
			if child.Value != "workflow_call" {
				return true
			}
		}
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value != "workflow_call" {
				return true
			}
		}
	}
	return false
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func mappingScalar(node *yaml.Node, key string) string {
	value := mappingValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

func contractFingerprint(root string, contract releaseSigningContract) ([sha256.Size]byte, error) {
	names := append([]string{contractPath}, contract.FingerprintFiles...)
	sort.Strings(names)
	hash := sha256.New()
	_, _ = io.WriteString(hash, "reasonix-signpath-release-contract-v1\x00")
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return [sha256.Size]byte{}, fmt.Errorf("read %s: %w", name, err)
		}
		writeLengthPrefixed(hash, []byte(name))
		writeLengthPrefixed(hash, data)
	}
	var result [sha256.Size]byte
	copy(result[:], hash.Sum(nil))
	return result, nil
}

func writeLengthPrefixed(writer io.Writer, data []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(data)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write(data)
}
