// Package config provides configuration initialization for Reasonix.
// This file handles auto-generation of default configuration files
// in the relative ./reasonix/ directory when they don't exist.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultConfigDir is the relative directory where Reasonix stores its local configuration.
	DefaultConfigDir = "reasonix"
	
	// ConfigFileName is the name of the main configuration file.
	ConfigFileName = "config.toml"
	
	// SystemPromptFileName is the name of the system prompt file.
	SystemPromptFileName = "system_prompt.txt"
	
	// DeepSeekAPIURL is the official DeepSeek API endpoint.
	DeepSeekAPIURL = "https://api.deepseek.com"
	
	// ExampleAPIKey is a placeholder showing the expected API key format.
	ExampleAPIKey = "sk-your-actual-api-key-here"
)

// LocalConfigPath returns the path to the local config directory relative to the working directory.
func LocalConfigPath() string {
	return DefaultConfigDir
}

// LocalConfigFile returns the full path to the local config.toml file.
func LocalConfigFile() string {
	return filepath.Join(DefaultConfigDir, ConfigFileName)
}

// LocalSystemPromptFile returns the full path to the local system prompt file.
func LocalSystemPromptFile() string {
	return filepath.Join(DefaultConfigDir, SystemPromptFileName)
}

// EnsureLocalConfigExists checks if the local configuration exists and generates defaults if not.
// It only checks for file existence and creates minimal defaults without overwriting existing files.
func EnsureLocalConfigExists() error {
	configDir := LocalConfigPath()
	configFile := LocalConfigFile()
	promptFile := LocalSystemPromptFile()
	
	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", configDir, err)
	}
	
	// Generate config.toml if it doesn't exist
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		if err := generateDefaultConfig(configFile); err != nil {
			return fmt.Errorf("failed to generate default config: %w", err)
		}
	}
	
	// Generate system_prompt.txt if it doesn't exist
	if _, err := os.Stat(promptFile); os.IsNotExist(err) {
		if err := generateDefaultSystemPrompt(promptFile); err != nil {
			return fmt.Errorf("failed to generate default system prompt: %w", err)
		}
	}
	
	return nil
}

// generateDefaultConfig creates a default config.toml file with DeepSeek API settings.
func generateDefaultConfig(path string) error {
	configContent := `# Reasonix Configuration File
# Generated automatically - edit as needed

# Version
config_version = 7

# Default model to use (DeepSeek V2.5)
default_model = "deepseek/deepseek-v2.5"

# Language preference (empty = auto-detect from environment)
language = ""

# Credentials store mode
credentials_store = "auto"

# UI Configuration
[ui]
theme = "auto"
theme_style = ""
shortcut_layout = "classic"
show_reasoning = true
show_turn_usage = true
cursor_shape = "bar"

# CLI Configuration
[cli]
update_channel = ""

# Desktop Configuration
[desktop]
language = "auto"
layout_style = "workbench"
theme = "auto"
theme_style = ""
terminal_theme = "auto"
close_behavior = "background"
display_mode = "standard"
status_bar_style = "text"
default_tool_approval_mode = "yolo"
check_updates = true
telemetry = true
metrics = true
expand_thinking = false
reasoning_display_mode = "auto"
conversation_width = "standard"

# Agent Configuration
[agent]
# Using the PROFESSIONAL tier system prompt optimized for DeepSeek V4 Pro
# Set system_prompt_file = "reasonix/system_prompt.txt" to use custom prompts
system_prompt = ""
system_prompt_file = ""
max_steps = 0
planner_max_steps = 0
auto_plan = "off"

# Billing Configuration
[billing]
display_currency = "USD"

# Notifications Configuration
[notifications]
enabled = false
turn_done = true
approval_request = true
ask_request = true

# Telemetry Configuration
[telemetry]
cli_metrics = "auto"

# Network Configuration
[network]
# DeepSeek API endpoint
# You can customize this if you're using a proxy or mirror
api_base_url = "` + DeepSeekAPIURL + `"

# Providers Configuration
# Add your DeepSeek API key here or set via environment variable DEEPSEEK_API_KEY
[[providers]]
id = "deepseek"
name = "DeepSeek"
base_url = "` + DeepSeekAPIURL + `"
api_key_env = "DEEPSEEK_API_KEY"
# Example API key format: ` + ExampleAPIKey + `
# To set your actual key, either:
# 1. Export DEEPSEEK_API_KEY=sk-your-real-key in your shell
# 2. Add api_key = "sk-your-real-key" here (not recommended for security)

# Tools Configuration
[tools]
auto_approve = []

# Permissions Configuration
[permissions]
allow_network = true
allow_write = true
allow_execute = true

# Sandbox Configuration
[sandbox]
enabled = false

# Environment Configuration
[environment]
enabled = true

# Skills Configuration
[skills]
disabled_skills = []

# Statusline Configuration
[statusline]
enabled = true

# LSP Configuration
[lsp]
enabled = false

# Bot Configuration
[bot]
enabled = false

# Serve Configuration
[serve]
enabled = false

# Secrets Configuration
[secrets]
filter_subprocess_env = false
protect_sensitive_files = false

# Remote Configuration
[remote]
enabled = false
`
	
	if err := os.WriteFile(path, []byte(configContent), 0644); err != nil {
		return err
	}
	
	return nil
}

// generateDefaultSystemPrompt creates a default system_prompt.txt file with all three tiers.
func generateDefaultSystemPrompt(path string) error {
	// Combine all three system prompt tiers with clear separators
	promptContent := `# Reasonix System Prompts
# This file contains three tiers of system prompts for different use cases.
# Uncomment and use the tier that matches your needs.

# =============================================================================
# TIER 1: BASIC - Concise coding assistant (Default)
# =============================================================================
# Use this for simple, focused coding tasks with minimal overhead.

You are Reasonix, a coding agent.
Use the available tools when they help you complete the user's request.
Keep changes focused and responses concise.

# =============================================================================
# TIER 2: DETAILED - Comprehensive workflow guidance
# =============================================================================
# Use this for complex projects requiring thorough explanations and step-by-step guidance.

You are Reasonix, an advanced AI coding assistant powered by DeepSeek models.
Your role is to help users with software development tasks including:
- Writing, reading, and modifying code
- Debugging and troubleshooting issues
- Explaining code and concepts
- Refactoring and improving code quality
- Setting up projects and dependencies
- Running tests and validating changes

Workflow Guidelines:
1. Understand the user's request thoroughly before acting
2. Use appropriate tools for each task (read files, edit, run commands)
3. Explain your reasoning and actions clearly
4. Test changes when possible to ensure they work correctly
5. Keep the user informed about progress and any issues encountered

Always prioritize safety: do not make destructive changes without confirmation, and respect the user's project structure and coding style.

# =============================================================================
# TIER 3: PROFESSIONAL - DeepSeek V4 Pro optimized persona
# =============================================================================
# Use this for production-quality code and expert-level software engineering.

You are Reasonix Pro, an elite software engineering assistant optimized for DeepSeek V4 Pro.

Core Identity: Expert software engineer with deep knowledge across all technology stacks.

Thinking Protocol:
- Think in English, starting with "We need..."
- Analyze problems systematically before proposing solutions
- Consider edge cases, performance implications, and maintainability
- Break down complex tasks into manageable steps

Execution Standards:
- Write production-quality code with proper error handling
- Follow best practices and design patterns appropriate to the language/framework
- Include meaningful comments and documentation
- Ensure changes are minimal, focused, and reversible when possible
- Validate assumptions through testing or reasoning

Communication Style:
- Be precise and technical without unnecessary verbosity
- Provide context for non-obvious decisions
- Anticipate follow-up questions and address them proactively
- Adapt explanation depth to the user's demonstrated expertise level

Tool Mastery:
- Select the most efficient tool for each subtask
- Chain tool calls effectively for complex operations
- Verify tool outputs before proceeding
- Handle tool failures gracefully with fallback strategies

# =============================================================================
# USER DECISION POLICY (Appended to all tiers)
# =============================================================================
# This policy ensures user control over consequential decisions.

User-owned choices: when a consequential decision has no safe, obvious default, call the ask tool so the user can choose. Otherwise proceed with a sensible reversible default. Do not ask in prose when ask is available. In non-interactive runs, state the assumption and take the safest reversible path.

# =============================================================================
# LANGUAGE POLICY (Auto-detection)
# =============================================================================
# Reply in the same language the user is using in their most recent message.

Reply in the same language the user is using in their most recent message: if they write in Chinese answer in Chinese, in English answer in English, and switch whenever they switch. Let this also guide the language you think in. Always keep code, identifiers, file paths, shell commands, and technical terms in their original form — never translate them.
`
	
	if err := os.WriteFile(path, []byte(promptContent), 0644); err != nil {
		return err
	}
	
	return nil
}

// GetActiveSystemPrompt reads the system prompt file and returns the active tier.
// If tier is empty, it returns the BASIC tier by default.
// Supported tiers: "basic", "detailed", "professional"
func GetActiveSystemPrompt(tier string) (string, error) {
	promptFile := LocalSystemPromptFile()
	
	content, err := os.ReadFile(promptFile)
	if err != nil {
		return "", fmt.Errorf("failed to read system prompt file: %w", err)
	}
	
	return parseSystemPromptByTier(string(content), tier)
}

// parseSystemPromptByTier extracts the prompt for a specific tier from the file content.
func parseSystemPromptByTier(content, tier string) (string, error) {
	tier = strings.ToLower(strings.TrimSpace(tier))
	if tier == "" {
		tier = "basic"
	}
	
	lines := strings.Split(content, "\n")
	var currentTier string
	var promptLines []string
	var inTargetTier bool
	
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		
		// Check for tier markers
		if strings.HasPrefix(trimmed, "# TIER 1:") || strings.HasPrefix(trimmed, "# TIER 1 :") {
			currentTier = "basic"
			inTargetTier = (currentTier == tier)
			promptLines = []string{}
			continue
		}
		if strings.HasPrefix(trimmed, "# TIER 2:") || strings.HasPrefix(trimmed, "# TIER 2 :") {
			currentTier = "detailed"
			inTargetTier = (currentTier == tier)
			promptLines = []string{}
			continue
		}
		if strings.HasPrefix(trimmed, "# TIER 3:") || strings.HasPrefix(trimmed, "# TIER 3 :") {
			currentTier = "professional"
			inTargetTier = (currentTier == tier)
			promptLines = []string{}
			continue
		}
		
		// Check for end of tier (next section marker or USER DECISION POLICY)
		if strings.HasPrefix(trimmed, "# ===") && inTargetTier && len(promptLines) > 0 {
			break
		}
		
		// Collect lines for the target tier (skip comments and empty lines at the start)
		if inTargetTier {
			// Skip the initial comment lines and empty lines
			if len(promptLines) == 0 && (strings.HasPrefix(trimmed, "#") || trimmed == "") {
				continue
			}
			// Stop if we hit the next major section
			if strings.HasPrefix(trimmed, "# ===") || 
			   strings.HasPrefix(trimmed, "# USER DECISION") ||
			   strings.HasPrefix(trimmed, "# LANGUAGE POLICY") {
				break
			}
			promptLines = append(promptLines, line)
		}
	}
	
	if len(promptLines) == 0 {
		// Fallback to hardcoded defaults based on tier
		switch tier {
		case "basic":
			return DefaultSystemPrompt, nil
		case "detailed":
			return DetailedSystemPrompt, nil
		case "professional":
			return ProfessionalSystemPrompt, nil
		default:
			return DefaultSystemPrompt, nil
		}
	}
	
	return strings.Join(promptLines, "\n"), nil
}
