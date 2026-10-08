// What the kernel reports mid-turn, in the reader's language. Keyed by the
// notice's code, which travels on the wire beside the kernel's own English —
// so a code with no entry here still reads, just untranslated. That is what
// makes adding them one at a time safe.
export const NOTICE_TEXT: Record<string, string> = {
  checkpoint_recording_failed: "检查点记录失败，工具执行结果未改变",
  empty_final: "这一轮模型没有给出回答，正在让它重说一次",
  executor_handoff: "模型直接给了答案却没动手，正在要求它先用工具",
  verification_stalled: "同一个检查连着几轮都报同样的结果，是否继续由你定",
  workspace_lease: "另一个会话持有重叠的写入范围，取得所需范围后会自动继续",
  workspace_lease_resumed: "这个会话已取得所需的写入范围，现已继续",
  workspace_lease_abandoned: "尚未取得所需的写入范围，这次等待已结束",
  permission_saved: "已记住这项授权，以后同样的操作自动允许，不再询问",
  permission_covered: "已有的授权规则覆盖了这项操作，无需另存",
  permission_save_failed: "授权没能保存，只在本次会话内有效",
  project_programs_awaiting_approval: "这个项目自带的钩子等程序要你批准后才会运行",
  project_program_changed: "这个项目的程序在批准后被改动过，本次没有运行，需要重新批准",
  hook_unevaluable: "有一个钩子无法被评估（匹配串无效、无法启动或载荷无法序列化），为安全起见已拦截这次操作；到钩子设置里修复或移除该钩子",
  suspected_injection: "一条外部内容看起来在向智能体下指令，已提醒它只当资料看待",
  await_user: "等待你的输入",
  default_model_unavailable: "配置里保存的默认模型已不在已配置的供应商中，本次改用第一个可用的模型；在设置里重新选择默认模型即可替换，配置文件未被改动",
  approval_mode_unrecognized: "配置里的默认审批档位本版本不认识，已按每次询问处理；在界面里选一个档位即可替换",
  memory_migration_backup: "旧版记忆文件没能先备份，所以没有迁移，保持原样；详情里有具体原因",
  inbox_recovered: "已恢复 {n} 条未完成的指令。待发送已暂停，请先在输入框上方的队列里查看，再点“继续派发”",
  queue_paused_hold: "待发送已暂停，这条消息已排入队列，点“继续派发”后才会发送",
  unapplied_steer: "引导没有生效：这一轮在处理它之前就结束了。如果仍然需要，请再发送一次：",
  display_currency: "费用显示币种已设为 {mode}",
  compacted: "已压缩",
  compact_declined: "无需压缩：{why}",
  compact_failed: "压缩失败：{why}",
  extension_skipped: "扩展 {ext} 的配套后台程序没有运行，该扩展本次（在 {point}）已被跳过；到「工具与集成」里查看并启动它，或停用该扩展",
  perseveration_loop: "模型卡在重复输出同一段文字，这一轮已停止；可以重试、补充引导，或换一个供应商/模型",
};
