package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// arm is one transition a workspace can undergo, with what it did to the thing
// the model would be sent. Truth is measured, not declared: the block is
// rendered before and after, and owed means those two differ.
type arm struct {
	name    string
	apply   func(e env) error
	comment string
}

var arms = []arm{
	{"api-create", func(e env) error { *e.apiWrites++; return writeSkill(e, "added", "a skill this process wrote") }, "a writer this process controls"},
	{"api-describe", func(e env) error {
		*e.apiWrites++
		return writeSkill(e, "probe-000", "a description this process rewrote")
	}, "same writer, different field"},
	{"api-delete", func(e env) error { *e.apiWrites++; return os.RemoveAll(skillDir(e, "probe-000")) }, "same writer, removal"},
	{"external-create", func(e env) error { return writeSkill(e, "outside", "a skill written past the API") }, "an editor, a script, the write tool"},
	{"external-describe", func(e env) error { return writeSkill(e, "probe-000", "a description rewritten past the API") }, "the common out-of-band edit"},
	{"external-delete", func(e env) error { return os.RemoveAll(skillDir(e, "probe-000")) }, "removal past the API"},
	{"mtime-restored", restoreMtimeChange, "same length, older mtime: git checkout, rsync, touch -r"},
	{"noop-touch", touchOnly, "mtime moved, nothing the model sees changed"},
	{"body-only", bodyOnlyChange, "the body changed; the index does not render it"},
}

func skillDir(e env, name string) string {
	return filepath.Join(e.projectRoot, ".reasonix", "skills", name)
}

func writeSkill(e env, name, description string) error {
	dir := skillDir(e, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "SKILL.md"),
		fmt.Appendf(nil, "---\nname: %s\ndescription: %s\n---\n\nBody paragraph the index never renders.\n", name, description), 0o644)
}

// restoreMtimeChange rewrites the rendered description without moving either
// size or mtime. Nothing exotic produces this: a branch switch restores content
// and stamps it with the mtime the file had.
func restoreMtimeChange(e env) error {
	path := filepath.Join(skillDir(e, "probe-000"), "SKILL.md")
	before, err := os.Stat(path)
	if err != nil {
		return err
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	swapped := strings.Replace(string(old), "probe skill 0, one line of index text.", "PROBE SKILL 0, ONE LINE OF INDEX TEXT.", 1)
	if swapped == string(old) {
		return fmt.Errorf("arm mtime-restored did not change the description; the fixture moved")
	}
	if len(swapped) != len(old) {
		return fmt.Errorf("arm mtime-restored changed the file length (%d -> %d); it must not", len(old), len(swapped))
	}
	if err := os.WriteFile(path, []byte(swapped), 0o644); err != nil {
		return err
	}
	return os.Chtimes(path, before.ModTime(), before.ModTime())
}

func touchOnly(e env) error {
	path := filepath.Join(skillDir(e, "probe-000"), "SKILL.md")
	later := time.Now().Add(time.Hour)
	return os.Chtimes(path, later, later)
}

func bodyOnlyChange(e env) error {
	path := filepath.Join(skillDir(e, "probe-000"), "SKILL.md")
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Replace(string(old), "Body paragraph the index never renders.", "A different body paragraph, longer than the one it replaces.", 1)), 0o644)
}
