package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"reasonix/internal/config"
	fileencoding "reasonix/internal/fileutil/encoding"
)

type Task struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Prompt          string `json:"prompt"`
	Interval        string `json:"interval"`
	Enabled         bool   `json:"enabled"`
	TopicID         string `json:"topicId,omitempty"`
	LastRunAt       int64  `json:"lastRunAt,omitempty"`
	CreatedAt       int64  `json:"createdAt,omitempty"`
	ApprovalMode    string `json:"approvalMode"`
	TimeWindowStart string `json:"timeWindowStart,omitempty"`
	TimeWindowEnd   string `json:"timeWindowEnd,omitempty"`
}

func main() {
	base := config.MemoryUserDir()
	if base == "" {
		base = "."
	}
	path := filepath.Join(base, "heartbeat-tasks.json")

	// Read existing
	b, _ := fileencoding.ReadFileUTF8(path)
	var data struct {
		Tasks []Task `json:"tasks"`
	}
	if len(b) > 0 {
		_ = json.Unmarshal(b, &data)
	}
	if data.Tasks == nil {
		data.Tasks = []Task{}
	}

	now := time.Now().UnixMilli()
	data.Tasks = append(data.Tasks, Task{
		ID:        "greeting_hello_001",
		Title:     "打个招呼",
		Prompt:    "你好！请用一段友好的话介绍一下你自己，然后用中文打个招呼。",
		Interval:  "2m",
		Enabled:   true,
		CreatedAt: now,
	}, Task{
		ID:        "daily_check_002",
		Title:     "每日检查",
		Prompt:    "检查当前项目的最新改动和状态，总结需要关注的事项。",
		Interval:  "1h",
		Enabled:   true,
		CreatedAt: now,
	})

	out, _ := json.MarshalIndent(data, "", "  ")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Println("Mkdir error:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		fmt.Println("Write error:", err)
		os.Exit(1)
	}
	fmt.Println("Done! Added 2 tasks.")
	fmt.Println("File:", path)
}
