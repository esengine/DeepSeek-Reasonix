package taskintent

import "testing"

func TestGoalNeedsWriteBudgetMatrix(t *testing.T) {
	// User-reported Chinese bare fault statement must land on the write budget.
	const userReported = "数据模型管理器又出现历史 BUG 了……"
	if !GoalNeedsWriteBudget(userReported) {
		t.Fatalf("GoalNeedsWriteBudget(%q) = false, want write", userReported)
	}
	// Ordinary Delivery must still treat the same bare fault as non-mutation
	// (observable read / evidence, not write-required).
	if NeedsMutation(userReported) {
		t.Fatalf("NeedsMutation(%q) changed; ordinary Delivery must stay non-mutation", userReported)
	}

	writeCases := []string{
		userReported,
		"应用打开设置时崩溃",
		"the auth service crashes on login",
		"parser throws an exception on empty input",
		"fix the crash in a.go",
		"帮我修复wps的崩溃问题",
		"why does it fail and fix it",
		"为什么失败然后修复它",
		"解释失败原因并修复",
	}
	for _, input := range writeCases {
		if !GoalNeedsWriteBudget(input) {
			t.Errorf("Goal write budget missing for %q", input)
		}
	}

	simpleCases := []string{
		"为什么会出现这个 BUG？",
		"只分析原因，不要修改代码。",
		"诊断数据库连接失败原因。",
		"复现并定位问题，但不要修复。",
		"why does this bug happen?",
		"explain the crash without changing code",
		"reproduce the crash and identify the root cause",
		"review only and do not fix anything",
		"hello",
	}
	for _, input := range simpleCases {
		if GoalNeedsWriteBudget(input) {
			t.Errorf("Goal simple budget expected for %q", input)
		}
	}
}

func TestGoalBareFaultDoesNotChangeDeliveryClassification(t *testing.T) {
	// Pin ordinary Delivery consultation/diagnosis so Goal write inference
	// cannot drift the shared delivery gates.
	readonly := []string{
		"为什么会出现这个 BUG？",
		"诊断数据库连接失败原因。",
		"reproduce the crash and identify the root cause",
		"应用打开设置时崩溃",
		"数据模型管理器又出现历史 BUG 了……",
	}
	for _, input := range readonly {
		if NeedsMutation(input) {
			t.Errorf("delivery mutation incorrectly true for %q", input)
		}
	}
	mutation := []string{
		"fix the crash in a.go",
		"为什么失败然后修复它",
	}
	for _, input := range mutation {
		if !NeedsMutation(input) {
			t.Errorf("delivery mutation incorrectly false for %q", input)
		}
	}
}

func TestTaskFaultSignalsSharedWithGoalClassification(t *testing.T) {
	// Shared fault list must keep task recognition and Goal classification
	// aligned for bare problem statements.
	for _, phrase := range []string{"bug", "crash", "崩溃", "异常", "报错", "失败"} {
		input := "something " + phrase + " happened"
		if !taskInputHasFaultSignal(input) {
			t.Errorf("taskInputHasFaultSignal missing %q", phrase)
		}
		if !heuristicInputIsTask(input) && !stringsContainsCJK(phrase) {
			// English bare fault remains a task signal.
			if !heuristicInputIsTask("the service has a " + phrase) {
				t.Errorf("heuristic task signal missing for fault %q", phrase)
			}
		}
	}
}

func stringsContainsCJK(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}
