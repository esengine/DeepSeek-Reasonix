package serve

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestCloudTasksExposeOnlyBoundedConversationSurface(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	hub := NewHub(HubOptions{})
	runtime := hubRuntime(t, hub, testenv.TempDir(t))

	value, err := hub.CloudTasks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	tasks := value.([]CloudTask)
	if len(tasks) != 1 || tasks[0].ID != runtime.ID {
		t.Fatalf("tasks = %+v", tasks)
	}
	for _, input := range []string{"!rm -rf somewhere", "/model another", strings.Repeat("x", (32<<10)+1)} {
		if err := hub.CloudSubmit(context.Background(), runtime.ID, input, "phone", 2); !errors.Is(err, ErrCloudInputRefused) {
			t.Errorf("CloudSubmit(%q) = %v, want refused", input[:min(len(input), 20)], err)
		}
	}
}

func TestCloudBoundTextKeepsUTF8Valid(t *testing.T) {
	budget := 5
	if got := cloudBoundText("你好世界", &budget); got != "你…" {
		t.Fatalf("cloudBoundText = %q, want one complete rune and ellipsis", got)
	}
}
