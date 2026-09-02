package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/topicstate"
)

func topicTitlesPath(workspaceRoot string) string {
	if workspaceRoot == "" {
		return filepath.Join(desktopConfigDir(), "global", topicTitlesFile)
	}
	return filepath.Join(workspaceRoot, ".reasonix", topicTitlesFile)
}

func topicTitleSourcesPath(workspaceRoot string) string {
	if workspaceRoot == "" {
		return filepath.Join(desktopConfigDir(), "global", topicTitleSourcesFile)
	}
	return filepath.Join(workspaceRoot, ".reasonix", topicTitleSourcesFile)
}

func topicCreatedAtsPath(workspaceRoot string) string {
	if workspaceRoot == "" {
		return filepath.Join(desktopConfigDir(), "global", topicCreatedAtsFile)
	}
	return filepath.Join(workspaceRoot, ".reasonix", topicCreatedAtsFile)
}

func topicAutoTitleMetaPath(workspaceRoot string) string {
	if workspaceRoot == "" {
		return filepath.Join(desktopConfigDir(), "global", topicAutoTitlesFile)
	}
	return filepath.Join(workspaceRoot, ".reasonix", topicAutoTitlesFile)
}

func legacyTopicPaths(workspaceRoot string) [4]string {
	return [4]string{
		topicTitlesPath(workspaceRoot), topicTitleSourcesPath(workspaceRoot),
		topicCreatedAtsPath(workspaceRoot), topicAutoTitleMetaPath(workspaceRoot),
	}
}

func legacyTopicFilesExist(workspaceRoot string) bool {
	for _, path := range legacyTopicPaths(workspaceRoot) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func topicScopeKind(workspaceRoot string) string {
	if strings.TrimSpace(workspaceRoot) == "" {
		return "global"
	}
	return "project"
}

func topicStateErrorType(err error) string {
	if err == nil {
		return "none"
	}
	var future *topicstate.FutureSchemaError
	if errors.As(err, &future) {
		return "future_schema"
	}
	if topicstate.IsCorruptionError(err) {
		return "corruption"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, os.ErrPermission) {
		return "permission"
	}
	return "io"
}
