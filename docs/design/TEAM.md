---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-26
---

# The user's team of agents

## 1. Purpose

Studio keeps a team of long-lived agents beside the user's projects.

- Each agent has a name, a role, standing instructions, a look and its own conversation.
- A main agent (Chief) creates and manages the others, and agents hand work to each other.
- Group chats let several agents take turns on one thread.

| ID | Principle |
| --- | --- |
| T1 | A message between agents carries no authority. The receiver runs under its own approval mode, held to the strictest mode on the chain that handed the work on. |
| T2 | Identity is dynamic state. It rides the turn tail as standing blocks; the system prompt and tool block are byte-identical across every agent session. |
| T3 | Judgements read structure. Members pass or hand on a turn with tools; the user's mentions are member ids. Nothing parses prose for `[PASS]` or `@Name`. |
| T4 | How a turn ended is the host's word. A failed, stopped or interrupted turn answers with that status and its code. |
| T5 | Permission modes, main agents, relay limits and deletion are the user's alone, through the window. No tool reaches them. |

Rosters admit at most 32 agents and 16 groups. Older larger rosters remain readable
and may shrink, but cannot grow further. Host frames escape the shared host-tag
vocabulary in untrusted text.

Persistent role, instruction, initial-model and
nonempty folder changes require fresh human approval for the exact change;
approval does not grant permission to edit another agent.

名册最多新增至 32 个 Agent、16 个群组；旧的超限名册仍可读取和缩减，不能继续扩张。
不可信文本中的宿主标签统一转义。持久角色、指令、初始模型及非空目录变更必须
获得针对本次内容的人工批准；批准不代替对目标 Agent 的编辑权限。

## 2. Layers

| Package | Role |
| --- | --- |
| `internal/state/teamroster` | Leaf store: roster (agents, groups), exchange book, group logs, round state. Atomic writes, revision CAS, a writer lease per process. |
| `internal/session/team` | Service: messaging and the handoff tree, group rounds and timers, user administration, deterministic rendering. Drives sessions only through `Panes`. |
| `internal/session/control` | The seat a session speaks for: standing blocks, team tools' caller context, delivery into the inbox, settlement at turn end, the approval ceiling and the `team_change` card. |
| `internal/frontend/serve` | `Panes` over hub runtimes (background seats, idle close, live cap), the `/team` routes and view, and keeping the team out of the project tree. |
| `desktop/frontend-next/src/ui/team` | Sidebar block, agent and group dialogs, group chat stage, origin rows in the transcript. |

## 3. Messaging

| Step | What happens |
| --- | --- |
| Send | `message_agent` opens an exchange whose parent is the exchange the caller is serving, then queues a framed `<agent-message>` as a host turn in the receiver's inbox. |
| Refuse | Work handed upstream on its own branch (`team.upstream`), a chain six deep, sixteen unanswered, twelve a minute, eight in one turn. |
| Answer | When the receiver's turn ends, the controller reports what it served and read. With work still out, the answer is held; otherwise `<agent-reply>` goes to the sender's seat. |
| Read | A reply counts as returned only once the sender's turn has read it, so an answer never leaves before the replies it depends on. |

On restart, queued, delivered and waiting-for-children exchanges are interrupted
before seats resume. The controller must not replay work for settled exchanges.

重启时，排队、已投递和等待子任务的交换均先结算为中断；会话不得重新执行已结算任务。

## 4. Group rounds

| Rule | Behaviour |
| --- | --- |
| Who answers | The mentioned members, else everyone or the admin by the group's mode. Members speak one at a time. |
| Handing on | `hand_turn` puts named members next and counts as a relay. |
| Relay limit | At 3 × members by default, agent calls park behind a Continue the user gives. |
| Timers | A speaker silent for 240 s is missed; a tool running 15 s moves the member off the order and its answer posts late. |
| Restart | The turn in flight is cut and the rest are parked. |

## 5. Approvals

| Change | Approval |
| --- | --- |
| A turn delivered by the team | Gated at the stricter of the session's mode and the chain's ceiling; a forged ceiling can only lower it. |
| A model choosing an agent's folder | Fresh `team_change` card every time; no posture, grant, hook or rule answers it. |
| A model changing role, instructions or an unused initial model | Fresh `team_change` card; Main/self authorization is checked before asking and again before writing. |
| Main agent, permission, relay limit, deletion | User only, through `/team` routes. |

## 6. Known limits

Only a new private default agent folder receives automatic trust. Assigning an
existing or custom folder requires its ordinary workspace trust decision first;
roster metadata alone cannot grant trust. Sidebar history caches validate file
identity and change time, including replacements that preserve size and mtime.

只有宿主新建的 Agent 默认私有目录可自动信任。指定已有或自定义目录时，须先通过普通
工作区确认信任；名册字段本身不授权。侧栏缓存检查文件身份和变更时间，包含同长度且恢复
修改时间的替换。

Team execution is disabled on Windows until disk-backed roster and inbox
authority can be protected from child processes. Ordinary sessions remain
available. Plan-mode delegation carries a read-only ceiling even when the
receiver's persistent permission mode is YOLO.

Windows 暂不运行团队会话，直到名册与 inbox 权限记录能隔离子进程修改；普通会话仍可使用。
计划阶段的团队委派携带只读上限，不受接收方持久 YOLO 设置影响。

Automatic memory review requires explicit opt-in. It sends human-authored new
lines and existing memory to the title model, or the default chat model when no
title model is configured. Each run incurs model usage.

Agent replies are not new evidence; credential-bearing or invalid change
batches publish nothing.

自动整理记忆默认关闭。开启后将用户的新消息与已有记忆发送给标题模型；未配置时使用
默认聊天模型，每次会产生模型用量。Agent 回复不作为新证据；包含凭据或无效条目的
变更批次不会写入。整理结果在对应会话或群聊中显示。

Local schedule files do not authenticate their author. Scheduled input is
labelled as scheduler-origin content and carries an ask ceiling; it is never a
fresh user command. Older scheduled group posts are read with scheduler provenance.

An older build may reject the new round cause and should not write these records.

本地定时文件不能证明作者身份。任务明确标注调度来源并附带审批上限，不能冒充用户
当次指令。历史定时群消息也按调度来源读取；旧版本可能不识别新增的调度轮次类型，
不应写入这些记录。

| Limit | Note |
| --- | --- |
| Live origin rows | The transcript draws "message from X" when it is rebuilt from history; the live event stream does not carry the origin yet. |
| Windows | No OS sandbox bounds what an agent's shell can write, so the roster file is protected only by being outside workspace write roots on macOS and Linux. |
| Memory, browser, usage | Agent and group memory scopes, per-agent browser profiles and usage by agent are not built. |
| Terminal | The TUI and ACP do not seat sessions on the team. |
