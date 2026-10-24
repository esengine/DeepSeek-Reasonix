package delegation

import "testing"

func TestUnifiedChildRetainsProviderRetryPolicy(t *testing.T) {
	for _, retries := range []int{0, 3, -1} {
		task := NewTaskToolWithOptions(TaskToolOptions{ProviderRetries: retries})
		child := task.subagentOptions(t.Context(), 4, nil, 8192, 1, "child", nil)
		if child.ProviderRetries != retries {
			t.Fatalf("retry policy = %d, want %d", child.ProviderRetries, retries)
		}
	}
}
