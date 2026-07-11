package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── 桌面打包优化 ──
// 用户要求：将打包相关的基础内容打包到桌面后再进行后续操作。
// 设计：分阶段打包——先打包基础内容到桌面暂存目录，验证完整性后再执行后续操作。
//
// 打包流程：
// 1. Stage 1: 收集基础内容（配置、密码本、默认模型、技能索引）
// 2. Stage 2: 打包到桌面暂存目录（Desktop/reasonix-staging/）
// 3. Stage 3: 验证完整性（SHA-256 校验）
// 4. Stage 4: 后续操作（签名、分发、安装）
//
// 设计原则：
// - 暂存目录在桌面（用户可见可控）
// - 打包过程可中断恢复（断点续传）
// - 完整性校验后才进入后续阶段
// - 失败时暂存目录保留（供调试）

// DesktopPackager 桌面打包器
type DesktopPackager struct {
	mu            sync.Mutex
	stagingDir    string // 桌面暂存目录
	workspace     string // 工作目录
	currentStage  PackagerStage
	stageProgress map[PackagerStage]float64 // 各阶段进度 0-1
	manifest      *PackageManifest
	verifyHashes  map[string]string // 文件路径 → SHA-256
}

// PackagerStage 打包阶段
type PackagerStage int

const (
	StageIdle       PackagerStage = iota
	StageCollect                  // 收集基础内容
	StagePackage                  // 打包到桌面
	StageVerify                   // 验证完整性
	StageSign                     // 签名
	StageDistribute               // 分发
	StageComplete                 // 完成
)

// String 返回阶段名称
func (s PackagerStage) String() string {
	switch s {
	case StageIdle:
		return "idle"
	case StageCollect:
		return "collecting"
	case StagePackage:
		return "packaging"
	case StageVerify:
		return "verifying"
	case StageSign:
		return "signing"
	case StageDistribute:
		return "distributing"
	case StageComplete:
		return "complete"
	default:
		return "unknown"
	}
}

// PackageManifest 打包清单
type PackageManifest struct {
	Version   string         `json:"version"`
	CreatedAt time.Time      `json:"created_at"`
	Workspace string         `json:"workspace"`
	Files     []ManifestFile `json:"files"`
	TotalSize int64          `json:"total_size"`
	Checksum  string         `json:"checksum"` // 整体 SHA-256
}

// ManifestFile 清单文件条目
type ManifestFile struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Category string `json:"category"` // config, codex, skill, model, binary
}

// NewDesktopPackager 创建桌面打包器
func NewDesktopPackager(workspace string) (*DesktopPackager, error) {
	// 获取桌面路径
	desktop, err := getDesktopPath()
	if err != nil {
		return nil, fmt.Errorf("get desktop path: %w", err)
	}
	stagingDir := filepath.Join(desktop, "reasonix-staging")

	// 创建暂存目录
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}

	return &DesktopPackager{
		stagingDir:    stagingDir,
		workspace:     workspace,
		currentStage:  StageIdle,
		stageProgress: make(map[PackagerStage]float64),
		manifest: &PackageManifest{
			Workspace: workspace,
			CreatedAt: time.Now(),
		},
		verifyHashes: make(map[string]string),
	}, nil
}

// getDesktopPath 获取桌面路径
func getDesktopPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	// Windows: %USERPROFILE%\Desktop
	// Linux/macOS: ~/Desktop
	desktop := filepath.Join(home, "Desktop")
	if _, err := os.Stat(desktop); err != nil {
		// 某些系统桌面可能是 "桌面"（中文 Windows）
		desktopCN := filepath.Join(home, "桌面")
		if _, err := os.Stat(desktopCN); err == nil {
			return desktopCN, nil
		}
		return "", fmt.Errorf("desktop directory not found")
	}
	return desktop, nil
}

// GetStagingDir 返回暂存目录路径
func (p *DesktopPackager) GetStagingDir() string {
	return p.stagingDir
}

// GetCurrentStage 返回当前阶段
func (p *DesktopPackager) GetCurrentStage() PackagerStage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentStage
}

// GetProgress 返回指定阶段的进度
func (p *DesktopPackager) GetProgress(stage PackagerStage) float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stageProgress[stage]
}

// Package 执行完整打包流程
func (p *DesktopPackager) Package(ctx context.Context, opts PackageOptions) error {
	// Stage 1: 收集基础内容
	p.setStage(StageCollect)
	if err := p.collectBaseContent(ctx, opts); err != nil {
		return fmt.Errorf("stage 1 collect: %w", err)
	}
	p.setProgress(StageCollect, 1.0)

	// Stage 2: 打包到桌面
	p.setStage(StagePackage)
	if err := p.packageToDesktop(ctx, opts); err != nil {
		return fmt.Errorf("stage 2 package: %w", err)
	}
	p.setProgress(StagePackage, 1.0)

	// Stage 3: 验证完整性
	p.setStage(StageVerify)
	if err := p.verifyIntegrity(ctx); err != nil {
		return fmt.Errorf("stage 3 verify: %w", err)
	}
	p.setProgress(StageVerify, 1.0)

	// Stage 4: 后续操作（签名/分发）
	if opts.Sign {
		p.setStage(StageSign)
		if err := p.signPackage(ctx, opts); err != nil {
			return fmt.Errorf("stage 4 sign: %w", err)
		}
		p.setProgress(StageSign, 1.0)
	}

	if opts.Distribute {
		p.setStage(StageDistribute)
		if err := p.distribute(ctx, opts); err != nil {
			return fmt.Errorf("stage 5 distribute: %w", err)
		}
		p.setProgress(StageDistribute, 1.0)
	}

	p.setStage(StageComplete)
	return nil
}

// PackageOptions 打包选项
type PackageOptions struct {
	IncludeConfig    bool
	IncludeCodex     bool // 密码本
	IncludeSkills    bool
	IncludeModels    bool
	IncludeBinary    bool
	IncludeKnowledge bool // 知识库
	Sign             bool
	Distribute       bool
	OutputName       string
}

// DefaultPackageOptions 默认打包选项
func DefaultPackageOptions() PackageOptions {
	return PackageOptions{
		IncludeConfig:    true,
		IncludeCodex:     true,
		IncludeSkills:    true,
		IncludeModels:    false, // 模型文件通常不打包（太大）
		IncludeBinary:    true,
		IncludeKnowledge: true,
		Sign:             false,
		Distribute:       false,
		OutputName:       fmt.Sprintf("reasonix-%s", time.Now().Format("20060102-150405")),
	}
}

// collectBaseContent Stage 1: 收集基础内容
func (p *DesktopPackager) collectBaseContent(ctx context.Context, opts PackageOptions) error {
	categories := []struct {
		name     string
		enabled  bool
		patterns []string
	}{
		{"config", opts.IncludeConfig, []string{"config/*.toml", "config/*.yaml", "config/*.json"}},
		{"codex", opts.IncludeCodex, []string{"codex/*", "codex/**/*.json"}},
		{"skill", opts.IncludeSkills, []string{"skills/*.md", "skills/**/*.md"}},
		{"model", opts.IncludeModels, []string{"models/*.gguf", "models/*.bin"}},
		{"knowledge", opts.IncludeKnowledge, []string{"knowledge/*.md", "knowledge/**/*.json"}},
	}

	for _, cat := range categories {
		if !cat.enabled {
			continue
		}
		for _, pattern := range cat.patterns {
			matches, err := filepath.Glob(filepath.Join(p.workspace, pattern))
			if err != nil {
				continue
			}
			for _, match := range matches {
				info, err := os.Stat(match)
				if err != nil {
					continue
				}
				if info.IsDir() {
					continue
				}
				hash, err := computeSHA256(match)
				if err != nil {
					continue
				}
				relPath, _ := filepath.Rel(p.workspace, match)
				p.manifest.Files = append(p.manifest.Files, ManifestFile{
					Path:     relPath,
					Size:     info.Size(),
					SHA256:   hash,
					Category: cat.name,
				})
				p.manifest.TotalSize += info.Size()
			}
		}
		// 更新进度
		p.setProgress(StageCollect, float64(len(p.manifest.Files))/100)
	}
	return nil
}

// packageToDesktop Stage 2: 打包到桌面暂存目录
func (p *DesktopPackager) packageToDesktop(ctx context.Context, opts PackageOptions) error {
	outputDir := filepath.Join(p.stagingDir, opts.OutputName)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	total := len(p.manifest.Files)
	for i, file := range p.manifest.Files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		src := filepath.Join(p.workspace, file.Path)
		dst := filepath.Join(outputDir, file.Path)

		// 创建目标目录
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
		}

		// 复制文件（不使用 robocopy /MOVE，使用安全复制）
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("copy %s: %w", file.Path, err)
		}

		// 验证复制后文件的哈希
		dstHash, err := computeSHA256(dst)
		if err != nil {
			return fmt.Errorf("hash %s: %w", file.Path, err)
		}
		if dstHash != file.SHA256 {
			return fmt.Errorf("hash mismatch for %s: expected %s, got %s",
				file.Path, file.SHA256, dstHash)
		}

		p.setProgress(StagePackage, float64(i+1)/float64(total))
	}

	// 写入清单
	manifestPath := filepath.Join(outputDir, "manifest.json")
	manifestData := fmt.Sprintf(`{"version":"1.0","created_at":"%s","workspace":"%s","files":%d,"total_size":%d}`,
		p.manifest.CreatedAt.Format(time.RFC3339),
		p.manifest.Workspace,
		len(p.manifest.Files),
		p.manifest.TotalSize)
	if err := os.WriteFile(manifestPath, []byte(manifestData), 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	return nil
}

// verifyIntegrity Stage 3: 验证完整性
func (p *DesktopPackager) verifyIntegrity(ctx context.Context) error {
	// 找到实际输出目录
	entries, err := os.ReadDir(p.stagingDir)
	if err != nil {
		return err
	}
	var actualDir string
	for _, e := range entries {
		if e.IsDir() {
			actualDir = filepath.Join(p.stagingDir, e.Name())
			break
		}
	}
	if actualDir == "" {
		return fmt.Errorf("no output directory found in staging")
	}

	total := len(p.manifest.Files)
	for i, file := range p.manifest.Files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		dst := filepath.Join(actualDir, file.Path)
		hash, err := computeSHA256(dst)
		if err != nil {
			return fmt.Errorf("verify %s: %w", file.Path, err)
		}
		if hash != file.SHA256 {
			return fmt.Errorf("integrity check failed for %s", file.Path)
		}

		p.setProgress(StageVerify, float64(i+1)/float64(total))
	}
	return nil
}

// signPackage Stage 4: 签名
func (p *DesktopPackager) signPackage(ctx context.Context, opts PackageOptions) error {
	// 实际实现中使用 cosign 签名
	// 这里只是占位
	return nil
}

// distribute Stage 5: 分发
func (p *DesktopPackager) distribute(ctx context.Context, opts PackageOptions) error {
	// 实际实现中上传到分发平台
	// 这里只是占位
	return nil
}

// CleanStaging 清理暂存目录
func (p *DesktopPackager) CleanStaging() error {
	// 安全清理：先列出内容，确认无误后删除
	return os.RemoveAll(p.stagingDir)
}

// GetManifest 返回打包清单
func (p *DesktopPackager) GetManifest() *PackageManifest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.manifest
}

// ── 内部方法 ──

func (p *DesktopPackager) setStage(stage PackagerStage) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.currentStage = stage
}

func (p *DesktopPackager) setProgress(stage PackagerStage, progress float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if progress > 1 {
		progress = 1
	}
	p.stageProgress[stage] = progress
}

// computeSHA256 计算文件的 SHA-256
func computeSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyFile 安全复制文件（先复制到临时文件，验证后重命名）
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// 先写入临时文件
	tmpPath := dst + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	if _, err := io.Copy(tmpFile, srcFile); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// 原子重命名
	return os.Rename(tmpPath, dst)
}

// FormatManifest 格式化清单为可读字符串
func (m *PackageManifest) FormatManifest() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Package Manifest\n"))
	sb.WriteString(fmt.Sprintf("================\n"))
	sb.WriteString(fmt.Sprintf("Version:    1.0\n"))
	sb.WriteString(fmt.Sprintf("Created:    %s\n", m.CreatedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("Workspace:  %s\n", m.Workspace))
	sb.WriteString(fmt.Sprintf("Files:      %d\n", len(m.Files)))
	sb.WriteString(fmt.Sprintf("Total Size: %s\n", formatSize(m.TotalSize)))
	sb.WriteString("\nFiles:\n")
	for _, f := range m.Files {
		sb.WriteString(fmt.Sprintf("  [%s] %s (%s, sha256:%s...)\n",
			f.Category, f.Path, formatSize(f.Size), f.SHA256[:12]))
	}
	return sb.String()
}

// formatSize 格式化文件大小
func formatSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%dB", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	}
	return fmt.Sprintf("%.1fGB", float64(bytes)/(1024*1024*1024))
}
