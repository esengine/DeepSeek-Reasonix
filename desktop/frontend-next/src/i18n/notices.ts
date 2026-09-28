// What the kernel reports mid-turn, in the reader's language. Keyed by the
// notice's code, which travels on the wire beside the kernel's own English —
// so a code with no entry here still reads, just untranslated. That is what
// makes adding them one at a time safe.
export const NOTICE_TEXT: Record<string, string> = {
  empty_final: "这一轮模型没有给出回答，正在让它重说一次",
  executor_handoff: "模型直接给了答案却没动手，正在要求它先用工具",
  verification_stalled: "同一个检查连着几轮都报同样的结果，是否继续由你定",
  workspace_lease: "另一个会话正在写这个工作区，等它写完会自动继续",
  workspace_lease_resumed: "工作区空出来了，这个会话已经继续",
  workspace_lease_abandoned: "还没轮到这个会话写，这次等待就结束了",
  permission_saved: "已记住这项授权，以后同样的操作不再询问",
  permission_covered: "已有的授权规则覆盖了这项操作，无需另存",
  permission_save_failed: "授权没能保存，只在本次会话内有效",
  project_programs_awaiting_approval: "这个项目自带的钩子等程序要你批准后才会运行",
  project_program_changed: "这个项目的程序在批准后被改动过，本次没有运行，需要重新批准",
  suspected_injection: "一条外部内容看起来在向智能体下指令，已提醒它只当资料看待",
};
