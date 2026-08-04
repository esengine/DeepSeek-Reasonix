package responses

import (
	"testing"
	"time"
)

// TestGrantYearWindow：授权简化（用户 2026-08-04）——第一次确认即 1 年长期：
// 批准后 364 天有效 / 366 天后失效 / 拒绝（未批准）无效。
func TestGrantYearWindow(t *testing.T) {
	now := time.Now()

	p := DefaultPolicy()
	p.Approve(GrantYear, now)
	if !p.IsGranted(now.Add(364 * 24 * time.Hour)) {
		t.Fatal("year grant must be active at day 364")
	}
	if p.IsGranted(now.Add(366 * 24 * time.Hour)) {
		t.Fatal("year grant must expire after day 365")
	}

	// 拒绝：默认策略未批准 → 无效
	d := DefaultPolicy()
	if d.IsGranted(now) {
		t.Fatal("default policy (no grant) must deny web search")
	}

	// Revoke 后失效
	p.Revoke()
	if p.IsGranted(now) {
		t.Fatal("revoked grant must deny")
	}
}

// TestHourlyBudgetGate（用户 2026-08-04 预算上限机制）：1 小时 N 次付费
// 检索上限——AI 失控防护；窗口滚动后重置；预算记录随 MarkWebUsed 计数。
func TestHourlyBudgetGate(t *testing.T) {
	now := time.Now()
	p := DefaultPolicy()
	p.Approve(GrantYear, now)
	p.Frequency = FrequencyMedium
	p.Cooldown = time.Nanosecond // 预算测试与冷却解耦（>0 才生效）
	p.HourlyBudget = 3

	// 3 次内允许
	for i := 0; i < 3; i++ {
		if !p.CanWebSearch(now.Add(time.Duration(i) * time.Minute)) {
			t.Fatalf("call %d must be allowed within budget", i+1)
		}
		p.MarkWebUsed(now.Add(time.Duration(i) * time.Minute))
	}
	// 第 4 次超限
	if p.CanWebSearch(now.Add(3 * time.Minute)) {
		t.Fatal("4th call must be blocked by hourly budget")
	}
	exceeded, used, limit := p.BudgetExceeded(now.Add(3 * time.Minute))
	if !exceeded || used != 3 || limit != 3 {
		t.Fatalf("budget state = (%v,%d,%d)", exceeded, used, limit)
	}
	// 窗口滚动（1 小时后）重置
	if !p.CanWebSearch(now.Add(61 * time.Minute)) {
		t.Fatal("budget must reset after the rolling hour")
	}
}
