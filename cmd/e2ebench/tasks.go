package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"

	fileencoding "reasonix/internal/base/fileutil/encoding"
)

func loadTasks(suite string) ([]task, error) {
	tasksDir := filepath.Join(suite, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil, err
	}
	var tasks []task
	hasAnswerMaterial := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(tasksDir, e.Name())
		var t task
		data, err := fileencoding.ReadFileUTF8(filepath.Join(dir, "task.toml"))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if _, err := toml.Decode(string(data), &t); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		t.ID = e.Name()
		t.dir = dir
		if dirExists(filepath.Join(dir, "memory")) {
			hasAnswerMaterial = true
		}
		if t.TimeoutSec == 0 {
			t.TimeoutSec = 240
		}
		tasks = append(tasks, t)
	}
	if hasAnswerMaterial {
		for i := range tasks {
			tasks[i].answerRoot = tasksDir
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

func copyTaskSeed(cfg suiteConfig, t task, work string) error {
	seed := filepath.Join(t.dir, "workdir")
	if !dirExists(seed) {
		return nil
	}
	if err := copyDir(seed, work); err != nil {
		return err
	}
	pinTapeTimes(cfg, work)
	return nil
}
