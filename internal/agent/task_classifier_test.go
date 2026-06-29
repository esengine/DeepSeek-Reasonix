package agent

import (
	"context"
	"testing"

	"reasonix/internal/provider"
)

// TestHeuristicClassifier_IsTask 测试启发式分类器
func TestHeuristicClassifier_IsTask(t *testing.T) {
	classifier := newHeuristicClassifier()
	ctx := context.Background()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// 问候语 - 聊天
		{"hello", "hello", false},
		{"hi", "hi", false},
		{"你好", "你好", false},
		{"thanks", "thanks", false},
		{"谢谢", "谢谢", false},
		{"ok", "ok", false},
		{"好的", "好的", false},
		{"收到", "收到", false},
		{"我知道了", "我知道了", false},
		{"先不用", "先不用", false},

		// 明确的任务
		{"fix bug", "fix the bug", true},
		{"create component", "create a component", true},
		{"修复问题", "修复这个问题", true},
		{"run tests", "run tests", true},
		{"看看", "帮我看看这个错误", true},
		{"帮我看下", "帮我看下这个问题", true},
		{"处理下", "处理下这个 issue", true},
		{"排查", "排查一下启动失败", true},
		{"定位", "定位这个异常", true},

		// Conversational acknowledgements that contain task words.
		{"thanks for fixing", "thanks for fixing that!", false},
		{"check later", "I'll check later", false},
		{"test later", "I'll test it later", false},
		{"test was helpful", "that test was helpful", false},
		{"辛苦了", "辛苦了", false},

		// Actionable problem descriptions without imperative verbs.
		{"auth not working", "the auth isn't working", true},
		{"help with login", "can you help with login?", true},
		{"问题严重", "这个问题很严重", true},
		{"卡住了", "页面卡住了", true},
		{"没反应", "按钮点击没反应", true},
		{"不生效", "配置不生效", true},
		{"异常退出", "程序异常退出", true},

		// 文件引用
		{"file reference", "what about @auth.go", true},
		{"python file", "check main.py", true},

		// 边界情况
		{"empty", "", false},
		{"spaces", "   ", false},
		{"question mark", "?", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := classifier.IsTask(ctx, tt.input)
			if err != nil {
				t.Fatalf("IsTask() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("IsTask(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// fakeClassifierProvider 用于测试 LLM 分类器的 mock provider
type fakeClassifierProvider struct {
	reply     string
	streamErr error
}

func (f *fakeClassifierProvider) Name() string { return "fake" }

func (f *fakeClassifierProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	if f.streamErr != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: f.streamErr}
		close(ch)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: f.reply}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// TestLLMClassifier_IsTask 测试 LLM 分类器
func TestLLMClassifier_IsTask(t *testing.T) {
	ctx := context.Background()
	heuristic := newHeuristicClassifier()

	tests := []struct {
		name      string
		input     string
		llmReply  string
		want      bool
		streamErr error
	}{
		{"llm says task", "fix the bug", "task", true, nil},
		{"llm says chat", "hello", "chat", false, nil},
		{"llm error fallback", "fix bug", "", true, context.DeadlineExceeded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := &fakeClassifierProvider{reply: tt.llmReply, streamErr: tt.streamErr}
			classifier := newLLMClassifier(prov, heuristic)

			got, err := classifier.IsTask(ctx, tt.input)
			if err != nil {
				t.Fatalf("IsTask() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("IsTask(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestLLMClassifier_Cache 测试 LLM 分类器缓存
func TestLLMClassifier_Cache(t *testing.T) {
	ctx := context.Background()
	prov := &fakeClassifierProvider{reply: "task"}
	heuristic := newHeuristicClassifier()
	classifier := newLLMClassifier(prov, heuristic)

	// 第一次调用应该调用 LLM
	got1, err1 := classifier.IsTask(ctx, "fix bug")
	if err1 != nil {
		t.Fatalf("IsTask() error = %v", err1)
	}
	if !got1 {
		t.Error("expected task classification")
	}

	// 第二次调用相同输入应该从缓存返回
	got2, err2 := classifier.IsTask(ctx, "fix bug")
	if err2 != nil {
		t.Fatalf("IsTask() error = %v", err2)
	}
	if !got2 {
		t.Error("expected cached task classification")
	}
}
