package writeclaim

import (
	"path/filepath"
	"testing"
)

// 开关默认打开：整区声明与任何声明冲突（上游行为）；关闭后整区声明不再冲突，
// 而普通路径声明之间的冲突规则完全不变 —— 这正是「一处收口覆盖全部整区来源」的语义。
func TestWholeWorkspaceGateOnlyRelaxesWholeClaims(t *testing.T) {
	root := t.TempDir()
	whole, err := WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatalf("whole claim: %v", err)
	}
	file := WritePathSet{Paths: []string{filepath.Join(root, "a.txt")}}
	nested := WritePathSet{Paths: []string{filepath.Join(root, "a.txt", "b")}}

	t.Cleanup(func() { SetSerializeWholeWorkspace(true) })

	SetSerializeWholeWorkspace(true)
	if !whole.Overlaps(file) || !file.Overlaps(whole) {
		t.Fatal("开关打开时：整区声明必须仍然与路径声明冲突（上游行为）")
	}

	SetSerializeWholeWorkspace(false)
	if whole.Overlaps(file) || file.Overlaps(whole) {
		t.Fatal("开关关闭时：整区声明不应再与任何声明冲突")
	}
	if !file.Overlaps(nested) {
		t.Fatal("开关关闭不得削弱普通路径声明的父子冲突判定")
	}
}
