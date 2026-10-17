package boot

import (
	"encoding/json"
	"os"
	"path/filepath"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/ext/skill"
	"reasonix/internal/state/agentpreset"
)

func (b *builder) retainSkill(sk skill.Skill) (skill.Skill, error) {
	if sk.Scope == skill.ScopeBuiltin || sk.Path == "" || sk.Path == "(builtin)" {
		return sk.Complete(), nil
	}
	var asset agentpreset.DirectorySnapshot
	var err error
	if filepath.Base(sk.Path) == skill.SkillFile {
		asset, err = agentpreset.CaptureDirectory(filepath.Dir(sk.Path))
	} else {
		var data []byte
		data, err = os.ReadFile(sk.Path)
		asset.Files = []agentpreset.AssetFile{{Path: filepath.Base(sk.Path), Data: data, Mode: 0o600}}
	}
	if err != nil {
		return sk, err
	}
	if err := keepResource(b.opts.Composition, "skill-assets", sk.SlashName(), asset); err != nil {
		return sk, err
	}
	root, err := agentpreset.New(b.roots.Home(), b.root).Materialize(asset)
	if err != nil {
		return sk, err
	}
	sk.Path = filepath.Join(root, filepath.Base(sk.Path))
	return sk.Complete(), nil
}

func (b *builder) restoreSkills(skills []skill.Skill) error {
	for i := range skills {
		for _, r := range b.opts.Composition.Resources {
			if r.Kind != "skill-assets" || r.ID != skills[i].SlashName() {
				continue
			}
			var asset agentpreset.DirectorySnapshot
			if err := jsonResource(r, &asset); err != nil {
				return err
			}
			root, err := agentpreset.New(b.roots.Home(), b.root).Materialize(asset)
			if err != nil {
				return err
			}
			skills[i].Path = filepath.Join(root, filepath.Base(skills[i].Path))
		}
	}
	return nil
}

func jsonResource(r agentcomposition.Resource, v any) error { return json.Unmarshal(r.Data, v) }
