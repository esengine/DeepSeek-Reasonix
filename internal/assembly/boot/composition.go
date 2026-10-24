package boot

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/skill"
	"reasonix/internal/session/control"
	"reasonix/internal/state/agentpreset"
)

func (b *builder) prepareComposition() error {
	if b.opts.Composition == nil && b.opts.AgentComposition == "" {
		return nil
	}
	if b.opts.Composition != nil {
		if err := agentpreset.New(b.roots.Home(), b.root).VerifyBinding(b.opts.Composition); err != nil {
			return err
		}
		b.opts.Composition = agentcomposition.Clone(b.opts.Composition)
	} else {
		store := agentpreset.New(b.roots.Home(), b.root)
		id := b.opts.AgentComposition
		if id == "default" {
			var err error
			id, err = store.Default()
			if err != nil {
				return err
			}
		}
		if id == "builtin:general" {
			return nil
		}
		v, err := store.Get(id)
		if err != nil {
			return err
		}
		b.opts.Composition = b.compositionBinding(v, b.root)
	}
	binding := b.opts.Composition
	if binding.Workspace != "" && binding.Workspace != b.root {
		return fmt.Errorf("%w: workspace changed", agentcomposition.ErrUnavailable)
	}
	p := binding.Resolved.Persona
	b.cfg.Agent.Persona = config.PersonaConfig{Prefix: p.Prefix, Suffix: p.Suffix, IncludeRuntimeContext: &p.IncludeRuntimeContext}
	// Independent MCP connections keep mutable server state out of sibling sessions.
	b.opts.SharedHost = nil
	if binding.Live() {
		return b.prepareLiveComposition()
	}
	return b.prepareCompositionMCP()
}

func compositionAdmits(d agentcomposition.Definition) func(tool.Tool) bool {
	return func(t tool.Tool) bool {
		if m, ok := t.(tool.MCPMetadata); ok {
			return member(d.MCP, m.MCPServerName())
		}
		if e, ok := t.(interface{ ExtensionPackageName() string }); ok {
			return member(d.Extensions, e.ExtensionPackageName())
		}
		if d.Tools.Mode == agentcomposition.Inherit || tool.ReachOf(t) == tool.ReachHostControl {
			return true
		}
		if member(d.Tools, t.Name()) {
			return true
		}
		return false
	}
}

func member(s agentcomposition.Selection, id string) bool {
	if s.Mode == agentcomposition.Inherit {
		return true
	}
	return slices.ContainsFunc(s.Members, func(m agentcomposition.Module) bool { return m.ID == id })
}

func resource[T any](binding *agentcomposition.Binding, kind string) ([]T, error) {
	out := []T{}
	for _, r := range binding.Resources {
		if r.Kind != kind {
			continue
		}
		var v T
		if err := json.Unmarshal(r.Data, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func keepResource(binding *agentcomposition.Binding, kind, id string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	binding.Resources = append(binding.Resources, agentcomposition.Resource{Kind: kind, ID: id, Data: b})
	total := 0
	for _, r := range binding.Resources {
		total += len(r.Data)
	}
	if total > 128<<20 {
		return agentcomposition.Unavailable(id, "Retained resources exceed the session limit.")
	}
	return nil
}

func (b *builder) bindCompositionSkills() error {
	binding := b.opts.Composition
	if binding == nil {
		return nil
	}
	if binding.Live() {
		b.prompt.skillStore.ConfigureDiscoverySource(b.liveSkillSource())
		b.prompt.skillStore.Restrict(func(sk skill.Skill) bool {
			return binding.Resolved.Skills.Allows(sk.SlashName()) && (sk.Plugin == "" || binding.Resolved.Extensions.Allows(sk.Plugin))
		})
		b.prompt.skills = b.prompt.skillStore.List()
		if binding.SystemPrompt != "" {
			b.prompt.prompt = binding.SystemPrompt
		}
		return nil
	}
	var skills []skill.Skill
	if binding.Digest != "" {
		var err error
		skills, err = resource[skill.Skill](binding, "skill")
		if err != nil {
			return err
		}
		if err := b.restoreSkills(skills); err != nil {
			return err
		}
		if binding.SystemPrompt != "" {
			b.prompt.prompt = binding.SystemPrompt
		}
	} else {
		skills = []skill.Skill{}
		for _, sk := range b.prompt.skillStore.CompositionSkills() {
			if !member(binding.Resolved.Skills, sk.SlashName()) || sk.Plugin != "" && !member(binding.Resolved.Extensions, sk.Plugin) {
				continue
			}
			var err error
			sk, err = b.retainSkill(sk)
			if err != nil {
				return compositionResourceError("skill", sk.SlashName(), err)
			}
			if err := keepResource(binding, "skill", sk.SlashName(), sk); err != nil {
				return err
			}
			skills = append(skills, sk)
		}
		if err := verifySelected("skills", binding.Resolved.Skills, skills, func(s skill.Skill) string { return s.SlashName() }); err != nil {
			return err
		}
		binding.Resolved.Skills = selectionOf(skills, func(s skill.Skill) string { return s.SlashName() })
	}
	b.prompt.skillStore = b.prompt.skillStore.Freeze(skills)
	if err := b.prompt.skillStore.ValidateSnapshot(); err != nil {
		return fmt.Errorf("%w: %w", agentcomposition.ErrUnavailable, err)
	}
	b.prompt.skills = b.prompt.skillStore.List()
	return nil
}

func selectionOf[T any](items []T, id func(T) string) agentcomposition.Selection {
	s := agentcomposition.Selection{Mode: agentcomposition.Select, Members: []agentcomposition.Module{}}
	for _, item := range items {
		s.Members = append(s.Members, agentcomposition.Module{ID: id(item)})
	}
	return s
}

func verifySelected[T any](kind string, s agentcomposition.Selection, items []T, id func(T) string) error {
	if s.Mode == agentcomposition.Inherit {
		return nil
	}
	for _, m := range s.Members {
		if kind == "tools" && m.Optional {
			continue
		}
		if !slices.ContainsFunc(items, func(v T) bool { return id(v) == m.ID }) {
			return agentcomposition.Unavailable(kind+":"+m.ID, "Required module is unavailable or was revoked.")
		}
	}
	return nil
}

func (b *builder) finishComposition(ctrl *control.Controller, snap *extension.RuntimeSnapshot) error {
	binding := b.opts.Composition
	if binding == nil {
		return nil
	}
	if snap == nil {
		return fmt.Errorf("%w: runtime snapshot missing", agentcomposition.ErrUnavailable)
	}
	if diagnostics := snap.Diagnostics(); len(diagnostics) > 0 && !binding.Live() {
		return agentcomposition.Unavailable("assembly", strings.Join(diagnostics, "; "))
	}
	if binding.Digest != "" {
		if binding.ToolSchemaDigest != agentcomposition.Digest(b.tools.reg.Schemas()) {
			return fmt.Errorf("%w: tool contract changed", agentcomposition.ErrUnavailable)
		}
	} else {
		if err := verifySelected("tools", binding.Resolved.Tools, b.tools.reg.AllNames(), func(v string) string { return v }); err != nil {
			return err
		}
		if !binding.Live() {
			binding.Resolved.Tools = selectionOf(b.tools.reg.AllNames(), func(v string) string { return v })
		}
		binding.SystemPrompt = snap.SystemPrompt()
		binding.ToolSchemaDigest = agentcomposition.Digest(b.tools.reg.Schemas())
		binding.ToolSurface = []string{}
		for _, schema := range b.tools.reg.Schemas() {
			binding.ToolSurface = append(binding.ToolSurface, schema.Name)
		}
		binding.Seal()
		if err := agentpreset.New(b.roots.Home(), b.root).RetainBinding(binding); err != nil {
			return err
		}
	}
	ctrl.BindComposition(binding)
	if !binding.Live() {
		ctrl.ConfigureCompositionValidation(func() error { return b.validateCompositionAvailability() })
	} else {
		ctrl.ConfigureCompositionValidation(func() error { return b.ext.mgr.ValidateLiveStartup() })
	}
	b.tools.reg.Restrict(func(t tool.Tool) bool {
		if !binding.Live() && tool.ReachOf(t) != tool.ReachHostControl && !member(binding.Resolved.Tools, t.Name()) {
			return false
		}
		if !compositionAdmits(binding.Resolved)(t) {
			return false
		}
		return b.opts.Observe == nil || observeAdmits(b.opts.Observe.Tools)(t)
	})
	if !binding.Live() && b.tools.caps != nil && b.tools.caps.runtime != nil {
		b.tools.caps.runtime.FreezeComposition()
	}
	return nil
}
