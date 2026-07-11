package sandbox

import "reasonix/internal/safeops"

// ── P0-5: 安全删除系统 ──
// 实际实现已移至 internal/safeops 包，避免 sandbox ↔ winsandbox 循环依赖。
// 本文件仅提供向后兼容的 re-export。

// ProtectedPaths 是永远不允许自动删除的路径模式（re-export from safeops）
var ProtectedPaths = safeops.ProtectedPaths

// IsProtected 检查路径是否受保护（re-export from safeops）
func IsProtected(path string) bool {
	return safeops.IsProtected(path)
}

// SafeDelete 安全删除文件（re-export from safeops）
func SafeDelete(path string) error {
	return safeops.SafeDelete(path)
}

// SafeDeleteDir 安全删除目录（re-export from safeops）
func SafeDeleteDir(path string) error {
	return safeops.SafeDeleteDir(path)
}

// SafeDeleteWithQuarantine 隔离删除（re-export from safeops）
func SafeDeleteWithQuarantine(path, quarantineDir string) error {
	return safeops.SafeDeleteWithQuarantine(path, quarantineDir)
}
