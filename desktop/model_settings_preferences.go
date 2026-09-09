package main

import (
	"fmt"
	"reasonix/internal/agent"
	"reasonix/internal/config"
	"strings"
)

func setDefaultModelConfig(c *config.Config, ref string) error {

	resolved, err := selectableDesktopModelRef(c, ref)
	if err != nil {
		return err
	}
	c.DefaultModel = resolved
	return nil
}

func setPlannerModelConfig(c *config.Config, ref string) error {

	if ref != "" {
		resolved, err := selectableDesktopModelRef(c, ref)
		if err != nil {
			return err
		}
		ref = resolved
	}
	c.Agent.PlannerModel = ref
	return nil
}

func setVisionModelConfig(c *config.Config, ref string) error {

	ref = strings.TrimSpace(ref)
	if ref == "" || strings.EqualFold(ref, "auto") {
		c.Agent.VisionModel = strings.ToLower(ref)
		return nil
	}
	resolved, err := selectableDesktopVisionModelRef(c, ref)
	if err != nil {
		return err
	}
	c.Agent.VisionModel = resolved
	return nil
}

func setSubagentModelConfig(c *config.Config, ref string) error {

	ref = strings.TrimSpace(ref)
	if ref != "" {
		resolved, err := selectableDesktopModelRef(c, ref)
		if err != nil {
			return err
		}
		ref = resolved
	}
	c.Agent.SubagentModel = ref
	return nil
}

func setSubagentEffortConfig(c *config.Config, level string) error {

	level = strings.TrimSpace(level)
	if level == "" || level == "auto" {
		c.Agent.SubagentEffort = ""
		return nil
	}
	model := strings.TrimSpace(c.Agent.SubagentModel)
	if model == "" {
		model = c.DefaultModel
	}
	entry, ok := c.ResolveModel(model)
	if !ok {
		return fmt.Errorf("unknown subagent model %q", model)
	}
	effort, err := config.NormalizeEffort(entry, level)
	if err != nil {
		return err
	}
	c.Agent.SubagentEffort = effort
	return nil
}

func setSubagentProfileModelConfig(c *config.Config, name, ref string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		deleteSubagentOverrideAliases(c.Agent.SubagentModels, name)
		return nil
	}
	resolved, err := selectableDesktopModelRef(c, ref)
	if err != nil {
		return err
	}
	if c.Agent.SubagentModels == nil {
		c.Agent.SubagentModels = map[string]string{}
	}
	deleteSubagentOverrideAliases(c.Agent.SubagentModels, name)
	c.Agent.SubagentModels[name] = resolved
	return nil
}

func setSubagentProfileEffortConfig(c *config.Config, name, level string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}

	level = strings.TrimSpace(level)
	if level == "" || level == "auto" {
		deleteSubagentOverrideAliases(c.Agent.SubagentEfforts, name)
		return nil
	}
	// Validate against the model the override will actually apply to:
	// the alias-aware per-name model override first, then the global
	// subagent default, then the session default.
	model := subagentOverrideFor(c.Agent.SubagentModels, name)
	if model == "" {
		model = strings.TrimSpace(c.Agent.SubagentModel)
	}
	if model == "" {
		model = c.DefaultModel
	}
	entry, ok := c.ResolveModel(model)
	if !ok {
		return fmt.Errorf("unknown subagent model %q", model)
	}
	effort, err := config.NormalizeEffort(entry, level)
	if err != nil {
		return err
	}
	if c.Agent.SubagentEfforts == nil {
		c.Agent.SubagentEfforts = map[string]string{}
	}
	deleteSubagentOverrideAliases(c.Agent.SubagentEfforts, name)
	c.Agent.SubagentEfforts[name] = effort
	return nil
}

func setMaxSubagentDepthConfig(c *config.Config, depth int) error {

	c.Agent.MaxSubagentDepth = desktopMaxSubagentDepth(depth)
	return nil
}

func setMaxSubagentConcurrencyConfig(c *config.Config, n int) error {

	total, writers := agent.NormalizeConcurrencyLimits(n, c.Agent.MaxParallelWriters)
	c.Agent.MaxSubagentConcurrency = total
	c.Agent.MaxParallelWriters = writers
	return nil
}

func setMaxParallelWritersConfig(c *config.Config, n int) error {

	total, writers := agent.NormalizeConcurrencyLimits(c.Agent.MaxSubagentConcurrency, n)
	c.Agent.MaxSubagentConcurrency = total
	c.Agent.MaxParallelWriters = writers
	return nil
}
