package boot

import (
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
)

// configMigrations is what the pre-load migrations did, held until a sink
// exists to say so. They run before config.Load so this boot reads the
// migrated files.
type configMigrations struct {
	legacy              *config.MigrationResult
	legacyErr           error
	deepSeek            bool
	deepSeekErr         error
	stepLimits          bool
	stepLimitsErr       error
	redactToolOutput    bool
	redactToolOutputErr error
	memoryCompiler      bool
	memoryCompilerErr   error
	multiThreshold      bool
	multiThresholdErr   error
}

func runConfigMigrations(roots config.Roots, root string) configMigrations {
	var m configMigrations
	m.legacy, m.legacyErr = roots.MigrateLegacyIfNeededForRoot(root)
	m.deepSeek, m.deepSeekErr = roots.MigrateLegacyDeepSeekProtocolUserConfig()
	m.stepLimits, m.stepLimitsErr = roots.MigrateLegacyAgentStepLimitsForRoot(root)
	m.redactToolOutput, m.redactToolOutputErr = roots.MigrateLegacyRedactToolOutputForRoot(root)
	m.memoryCompiler, m.memoryCompilerErr = roots.MigrateLegacyMemoryCompilerForRoot(root)
	m.multiThreshold, m.multiThresholdErr = roots.MigrateLegacyMultiThresholdCompactionForRoot(root)
	return m
}

func (m configMigrations) report(sink event.Sink, cfg *config.Config) {
	if m.legacyErr != nil {
		report(sink, event.Event{Level: event.LevelWarn, Text: "Config migration did not complete.", Detail: "config migration from ~/.reasonix failed: " + m.legacyErr.Error()})
	} else if m.legacy != nil {
		report(sink, event.Event{Level: event.LevelInfo, Text: m.legacy.Notice()})
	}
	if m.deepSeek {
		sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  event.LevelInfo,
			Text:   "DeepSeek official access was upgraded to Anthropic Messages.",
			Detail: "Your unmodified legacy OpenAI Chat Completions configuration now uses DeepSeek's recommended Anthropic endpoint with server-side web search. Existing model names and pricing were preserved. The first request starts a new provider cache prefix; later requests rebuild normal prefix-cache reuse.",
		})
	} else if m.deepSeekErr != nil {
		sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  event.LevelWarn,
			Text:   "DeepSeek protocol migration did not complete.",
			Detail: m.deepSeekErr.Error(),
		})
	}
	m.reportStepLimits(sink, cfg)
	reportRetiredKey(sink, m.redactToolOutput, m.redactToolOutputErr,
		"Deprecated redact_tool_output setting was removed.",
		"Deprecated redact_tool_output setting was ignored.",
		"[secrets].redact_tool_output no longer has any effect: ordinary model/tool content and local session/job artifacts now preserve their original text. Explicit diagnostics and reasonix doctor redact-sessions still redact credential values.",
		" The old key could not be removed: ")
	reportRetiredKey(sink, m.memoryCompiler, m.memoryCompilerErr,
		"Deprecated memory_compiler setting was removed.",
		"Deprecated memory_compiler setting was ignored.",
		"The Memory v5 execution compiler has been removed from Reasonix: [agent].memory_compiler no longer has any effect, user turns are never replaced by compiled execution contracts, and no compiler state is written. Old transcripts containing compiled turns still display normally.",
		" The old key could not be removed: ")
	reportRetiredKey(sink, m.multiThreshold, m.multiThresholdErr,
		"上下文维护已简化为单一自动压缩阈值。",
		"Deprecated multi-threshold compaction keys were ignored.",
		"Context maintenance now follows compact_ratio of the model window by default (0.85). Set context_soft_limit_tokens only when an earlier fixed boundary is wanted. soft_compact_ratio, tool_result_snip_ratio, compact_force_ratio, cold_resume_prune, and context_editing were removed from config.",
		" The old keys could not be removed: ")
}

func (m configMigrations) reportStepLimits(sink event.Sink, cfg *config.Config) {
	if m.stepLimits || cfg.IgnoredLegacyAgentStepLimits() {
		level := event.LevelInfo
		text := "Deprecated agent step limits were removed."
		detail := "[agent].max_steps and planner_max_steps are no longer used; Reasonix now manages interactive progress automatically. " +
			"Use the CLI --max-steps flag for a one-off run."
		if m.stepLimitsErr != nil {
			level = event.LevelWarn
			text = "Deprecated agent step limits were ignored."
			detail += " The old keys were ignored but could not be removed: " + m.stepLimitsErr.Error()
		}
		sink.Emit(event.Event{
			Kind:   event.Notice,
			Level:  level,
			Text:   text,
			Detail: detail,
		})
	} else if m.stepLimitsErr != nil {
		report(sink, event.Event{Level: event.LevelWarn, Text: "Deprecated agent step-limit migration did not complete.", Detail: m.stepLimitsErr.Error()})
	}
}

// reportRetiredKey says a removed setting is gone, or that it is still on disk
// because removing it failed.
func reportRetiredKey(sink event.Sink, removed bool, err error, removedText, ignoredText, detail, unremovable string) {
	if !removed && err == nil {
		return
	}
	level, text := event.LevelInfo, removedText
	if err != nil {
		level, text = event.LevelWarn, ignoredText
		detail += unremovable + err.Error()
	}
	report(sink, event.Event{Level: level, Text: text, Detail: detail})
}
