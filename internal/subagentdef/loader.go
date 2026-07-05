package subagentdef

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/frontmatter"
)

type LoadOptions struct {
	Scope string
}

func LoadFromFile(path string, opts LoadOptions) (*SubagentDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read subagent file %q: %w", path, err)
	}

	def := &SubagentDefinition{}
	body, err := frontmatter.Decode(string(data), def, frontmatter.DecodeOptions{KnownFields: false})
	if err != nil {
		return nil, fmt.Errorf("parse subagent frontmatter %q: %w", path, err)
	}

	def.Prompt = strings.TrimSpace(body)
	def.SourceFile = path
	def.SourceScope = opts.Scope

	info, err := os.Stat(path)
	if err == nil {
		def.CreatedAt = info.ModTime()
		def.UpdatedAt = info.ModTime()
	}

	def.Normalize()

	if !def.Valid() {
		return nil, fmt.Errorf("subagent definition %q is missing a name", path)
	}

	return def, nil
}

func LoadFromDirectory(dir string, opts LoadOptions) ([]*SubagentDefinition, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read subagent directory %q: %w", dir, err)
	}

	var defs []*SubagentDefinition
	seen := map[string]bool{}

	var walk func(string) error
	walk = func(current string) error {
		entries, err := os.ReadDir(current)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			fullPath := filepath.Join(current, entry.Name())
			if entry.IsDir() {
				if err := walk(fullPath); err != nil {
					return err
				}
				continue
			}
			if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
				continue
			}
			def, err := LoadFromFile(fullPath, opts)
			if err != nil {
				continue
			}
			key := strings.ToLower(def.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			defs = append(defs, def)
		}
		return nil
	}

	_ = entries
	if err := walk(dir); err != nil {
		return nil, err
	}

	return defs, nil
}

func LoadFromJSON(data []byte, opts LoadOptions) (*SubagentDefinition, error) {
	return nil, fmt.Errorf("not implemented")
}

func BuiltinDefinitions() []*SubagentDefinition {
	now := time.Now()
	return []*SubagentDefinition{
		{
			Name:        "explore",
			Description: "A fast, read-only agent optimized for searching and analyzing codebases.",
			Prompt:      "You are a code exploration agent. Your job is to quickly search and understand codebases. Use read-only tools to find files, search for patterns, and understand the code structure. Return concise, well-organized findings.",
			Tools:       []string{"read_file", "grep", "glob", "ls", "code_index"},
			Model:       "",
			SourceScope: "builtin",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			Name:        "plan",
			Description: "A research agent used for gathering context before presenting a plan.",
			Prompt:      "You are a planning research agent. Gather context about the codebase to help plan an approach. Use read-only tools to understand the current state and constraints. Return research findings that will inform the plan.",
			Tools:       []string{"read_file", "grep", "glob", "ls", "code_index"},
			SourceScope: "builtin",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			Name:        "general-purpose",
			Description: "A capable agent for complex, multi-step tasks requiring both exploration and action.",
			Prompt:      "You are a general-purpose coding agent. Handle complex tasks that require both exploring the codebase and making changes. Work methodically and return clear results.",
			SourceScope: "builtin",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			Name:        "code-reviewer",
			Description: "Reviews code for quality, security, and best practices.",
			Prompt:      "You are a senior code reviewer. Analyze the code and provide specific, actionable feedback on quality, security, performance, and best practices. Explain each issue clearly and suggest improvements.",
			Tools:       []string{"read_file", "grep", "glob", "ls", "code_index"},
			Color:       "#4CAF50",
			SourceScope: "builtin",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		{
			Name:        "debugger",
			Description: "Debugging specialist for errors and test failures.",
			Prompt:      "You are an expert debugger. Analyze errors, identify root causes, and provide fixes. Be methodical and verify your hypotheses.",
			Color:       "#FF9800",
			SourceScope: "builtin",
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
}
