// Package safeops provides safe file deletion operations with path protection.
// It is a leaf package with no reasonix-internal dependencies, so it can be
// imported by both sandbox and winsandbox without creating import cycles.
package safeops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ── P0-5: 安全删除系统 ──
// 问题：B1 自动清理在磁盘空间不足时可能误删用户重要文件。
// 方案：删除前检查白名单，拒绝删除受保护路径；删除使用安全删除协议（先移到回收站）。

// ProtectedPaths 是永远不允许自动删除的路径模式
var ProtectedPaths = []string{
	// 用户目录核心
	"Documents", "Desktop", "Downloads", "Pictures", "Videos", "Music",
	".ssh", ".gnupg", ".config", ".aws", ".kube",
	// 开发相关
	".git", ".env", "go.mod", "go.sum", "package.json", "Cargo.toml",
	"Makefile", "CMakeLists.txt", ".solution", "AGENTS.md", "REASONIX.md",
	// 系统关键
	"Windows", "System32", "Program Files", "ProgramData",
	"boot", "efi", "proc", "sys", "dev",
}

// IsProtected 检查路径是否受保护（不允许自动删除）
func IsProtected(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return true // 解析失败的路径视为受保护
	}
	// 根目录永远受保护
	if abs == string(filepath.Separator) || abs == "C:\\" || abs == "C:" {
		return true
	}
	// 盘符根目录受保护
	if len(abs) <= 3 && (strings.HasSuffix(abs, ":\\") || strings.HasSuffix(abs, ":")) {
		return true
	}
	lower := strings.ToLower(abs)
	for _, p := range ProtectedPaths {
		if strings.Contains(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// SafeDelete 安全删除文件：先检查白名单，然后删除
// 返回错误如果路径受保护
func SafeDelete(path string) error {
	if IsProtected(path) {
		return fmt.Errorf("refused to delete protected path: %s", path)
	}
	return os.Remove(path)
}

// SafeDeleteDir 安全删除目录：先检查白名单，然后删除
func SafeDeleteDir(path string) error {
	if IsProtected(path) {
		return fmt.Errorf("refused to delete protected directory: %s", path)
	}
	// 额外检查：目录中是否包含受保护文件
	var hasProtected bool
	filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if IsProtected(p) {
			hasProtected = true
			return filepath.SkipDir
		}
		return nil
	})
	if hasProtected {
		return fmt.Errorf("refused to delete directory %s: contains protected files", path)
	}
	return os.RemoveAll(path)
}

// SafeDeleteWithQuarantine 隔离删除：将文件移到隔离目录而非直接删除
// 隔离目录中的文件可以在 grace period 后被真正删除
func SafeDeleteWithQuarantine(path, quarantineDir string) error {
	if IsProtected(path) {
		return fmt.Errorf("refused to delete protected path: %s", path)
	}
	if quarantineDir == "" {
		// 没有隔离目录时退回到直接删除（但仍检查白名单）
		return SafeDelete(path)
	}
	// 确保隔离目录存在
	if err := os.MkdirAll(quarantineDir, 0700); err != nil {
		return fmt.Errorf("create quarantine dir: %w", err)
	}
	// 生成隔离路径
	base := filepath.Base(path)
	quarantinePath := filepath.Join(quarantineDir, base)
	// 避免冲突
	if _, err := os.Stat(quarantinePath); err == nil {
		quarantinePath = filepath.Join(quarantineDir,
			fmt.Sprintf("%s.%d", base, os.Getpid()))
	}
	// 移动而非删除
	if err := os.Rename(path, quarantinePath); err != nil {
		// 跨盘符时 Rename 失败，退回到直接删除
		return SafeDelete(path)
	}
	return nil
}
