package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed reasonix_skills/skills/*
var reasonixSkillsFS embed.FS

//go:embed reasonix_skills/commands/*
var reasonixCommandsFS embed.FS

// extractSkillsToHome copies bundled skills and commands to ~/.reasonix/ so
// Reasonix discovers them at startup. Skips if already extracted.
func extractSkillsToHome() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	reasonixDir := filepath.Join(home, ".reasonix")

	// Extract skills
	skillsDir := filepath.Join(reasonixDir, "skills")
	sentinel := filepath.Join(skillsDir, ".extracted")
	if _, err := os.Stat(sentinel); err == nil {
		return nil // already done
	}
	_ = os.MkdirAll(skillsDir, 0o755)
	if err := copyEmbedFS(reasonixSkillsFS, "reasonix_skills/skills", skillsDir); err != nil {
		return fmt.Errorf("extract skills: %w", err)
	}

	// Extract commands
	cmdsDir := filepath.Join(reasonixDir, "commands")
	if err := os.MkdirAll(cmdsDir, 0o755); err != nil {
		return fmt.Errorf("extract commands: %w", err)
	}
	if err := copyEmbedFS(reasonixCommandsFS, "reasonix_skills/commands", cmdsDir); err != nil {
		return fmt.Errorf("extract commands: %w", err)
	}

	_ = os.WriteFile(sentinel, []byte("1"), 0o644)
	return nil
}

func copyEmbedFS(efs embed.FS, prefix, dest string) error {
	return fs.WalkDir(efs, prefix, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(prefix, path)
		if err != nil || rel == "." {
			return nil
		}
		out := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, err := efs.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, 0o644)
	})
}

// geoPythonFS embeds the Python MCP server sources so they ship inside the
// desktop binary. On first launch the files are extracted to the user's data
// directory so the geocode plugin can find internal.geo.mcp_server without a
// separate source install.
//
//go:embed geo_mcp/*
var geoPythonFS embed.FS

func geoPythonDir(embedsDir string) string {
	return filepath.Join(embedsDir, "geo-mcp")
}

func extractGeoPython(embedsDir string) error {
	// Target: <embedsDir>/geo-mcp/internal/geo/mcp_server/…
	// so PYTHONPATH=<embedsDir>/geo-mcp/ lets `python -m internal.geo.mcp_server` work.
	target := filepath.Join(embedsDir, "geo-mcp", "internal", "geo", "mcp_server")
	sentinel := filepath.Join(target, ".extracted")

	if info, err := os.Stat(sentinel); err == nil && info.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("geo embed mkdir: %w", err)
	}

	err := fs.WalkDir(geoPythonFS, "geo_mcp", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("geo_mcp", path)
		if err != nil || rel == "." {
			return nil
		}
		dest := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := geoPythonFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("geo embed read %s: %w", path, err)
		}
		return os.WriteFile(dest, data, 0o644)
	})
	if err != nil {
		return fmt.Errorf("geo embed extract: %w", err)
	}

	_ = os.WriteFile(sentinel, []byte("1"), 0o644)
	return nil
}
