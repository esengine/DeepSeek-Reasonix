package workspacelease

import (
	"context"
	"testing"
	"time"
)

// 开关关闭时不再取跨会话写租约、直接放行；默认（开关打开）仍会排队等待持有者。
func TestWithoutWriteSerializationSkipsTheLease(t *testing.T) {
	lockDir := t.TempDir()
	root := t.TempDir()

	// 租约只在有活跃 run 时被持有：没有 run 时 AcquireWrite 拿到就释放。
	holder, err := New(root, lockDir, nil)
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	holder.BeginRun()
	defer holder.EndRun()
	if err := holder.AcquireWrite(context.Background()); err != nil {
		t.Fatalf("holder acquire: %v", err)
	}

	off, err := New(root, lockDir, nil, WithoutWriteSerialization())
	if err != nil {
		t.Fatalf("off owner: %v", err)
	}
	if err := off.AcquireWrite(context.Background()); err != nil {
		t.Fatalf("开关关闭时必须直接放行，实际: %v", err)
	}
	if st := off.State(); st.Acquired || st.Waiting {
		t.Fatalf("开关关闭时不该占租约也不该等待，实际 %+v", st)
	}

	on, err := New(root, lockDir, nil)
	if err != nil {
		t.Fatalf("on owner: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := on.AcquireWrite(ctx); err == nil {
		t.Fatal("默认（开关打开）仍必须等待持有者，实际立即拿到")
	}
}
