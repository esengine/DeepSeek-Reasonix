export const GUIDANCE_QUEUE_MOCK_ITEMS = [
  "先确认发送后输入框为什么残留刚发的消息，再决定修哪里。",
  "保持真实 steer 协议不变，只调整前端乐观队列和按钮状态。",
  "最后补后端 submit 悬挂时的回归测试，确保输入框会立刻释放。",
] as const;

export function browserMockScenarioParam(): string {
  if (typeof window === "undefined" || window.runtime) return "";
  return new URLSearchParams(window.location.search).get("mock")?.trim().toLowerCase() ?? "";
}

export function isGuidanceMockScenario(value: string): boolean {
  return value === "guidance" || value === "guide" || value === "steer";
}
