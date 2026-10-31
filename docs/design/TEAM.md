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
| Receipts | Team tools declare every effect host-owned (`tool.HostStateWriter`): a turn that only managed the team reports no unverified change to the work product. |

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

Delegated work inherits the effective turn ceiling. Strict read-only workers
cannot dispatch team work or mutate team state, including through deferred tools.
Roster, schedule and secret management routes are host-only for paired devices.

委派任务继承当前回合的有效审批上限。严格只读的工作 Agent 不能调度团队工作或修改团队状态，延迟加载工具也遵守此边界。配对设备不能调用仅限主机的名册、定时任务和密钥管理接口。

## 6. Memory and schedules

| Feature | Behaviour |
| --- | --- |
| Memory tiers | Each agent keeps its own memory; `remember_shared` and `recall_shared` reach the team's and a group's. Shared memory rides the turn tail as `team-memory`. |
| Memory review | Off until explicitly enabled. Human-authored new lines and existing memories go to the title model, falling back to the default chat model. Changes are reported in the conversation or group. |
| Schedules | A prompt for an agent or a group: once, daily, weekly, or every 10 minutes to 7 days. A slot missed by more than 2 minutes is skipped, not made up. |
| Who fires them | The Studio host that holds the team's writer lease. Disk tasks are unverified scheduler input, gated by the receiver's mode and an ask ceiling. |
| Browser | Each agent's browser runs on its own profile (`agent-<id>`), in every seat, so one agent's logins are never another's. |
| Usage | Every call from an agent's seat, its memory review included, is recorded under the agent; the usage page lists spend by agent, and the agent's drawer sums its conversations. |

`remember_shared` requires fresh human approval. Automatic review preserves
pinned guidance and checks the observed revision before editing or archiving a
fact. Shutdown cancels and joins reviews before releasing the team's writer lease.

`remember_shared` 每次都需要人工审批。自动整理保留固定指导，修改或归档事实前检查读取时的版本；主机关闭时先取消并等待整理退出，再释放团队写入租约。

## 7. Secrets

An agent asks with `request_secret`, from its own conversation only. The user
answers on a card: the value, and which agents get it. The asker alone is the
default, and a secret already saved under the name keeps the agents it had.

| Rule | Behaviour |
| --- | --- |
| Reaching commands | Explicitly authorized current agents receive `$REASONIX_SECRET_NAME` only in commands whose shell syntax references it. Empty recipients authorize nobody. No tool returns the value. |
| Reaching models | Every built-in adapter masks the request it sends (`provider.MaskOutbound`), and so does every crossing into an extension process (`providerconv`). A model reads `[secret:NAME]`, or `[secret:A,B]` for a value saved under two names. |
| Signed bytes | Kept whole only by the adapter that sends their signature back: Anthropic thinking, Gemini thought steps, thought-signed calls to Gemini endpoints. Every other receiver gets them masked. |
| Freshness | Every snapshot checks source freshness. Decoded records are reused only while file identity, change time, size and mtime remain stable; racing writes are reread. Mask snapshots are rebuilt only for newly held values or labels, and retain every value seen until process exit. |
| Delete and replace | The value stops reaching commands and is kept as retired, so copies already in conversations stay masked. Forgetting retired values is a separate, confirmed action. |
| Writing back | The file tools and their previews refuse text carrying a mark (`secret.masked_write`); so do shell commands (`secret.masked_command`) and MCP calls, proxied ones included (`secret.masked_call`). |
| What is kept | Every tool's result, a command's failure tail and progress, and a background job's log are masked before they are drawn or stored; a value split across two writes is held back until it can be recognised. |

JSON masking inspects decoded string and numeric tokens, including object keys
and duplicate keys. Tool descriptions and schemas cross the same boundary.

JSON with no matching value retains its exact bytes; a prefix containing a
registered value must change to remove it. Output streams refresh their mask
on every write so a newly saved value has no timed cache exemption.

Registering a secret for the first time can also mask matching text in older
conversation history. Those provider-visible bytes change, so the next request
may incur a one-time prompt-cache miss. Clean history bytes remain unchanged.

首次登记密钥也可能遮住历史对话中匹配的内容，改变发给模型的历史字节，因此下一次
请求可能发生一次提示词缓存未命中。没有命中密钥的历史字节保持不变。

JSON 脱敏检查解码后的字符串和数字，包括对象键名及重复键；工具说明和 schema 同样脱敏。没有命中密钥的 JSON 保持原始字节；前缀含有已登记密钥时，必须改变字节以去除它。输出流每次写入都刷新脱敏状态，新保存的密钥不存在定时缓存豁免。

## 8. Known limits

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

Team secret files contain metadata and immutable references to host credential
slots, never new plaintext values. Legacy values migrate only after the private
credential write succeeds. The current global credential backend is an
unencrypted local `.env`; this is not isolation from other same-user programs.

团队密钥文件只保存元数据和宿主凭据槽引用，不再写入新明文值。旧值在凭据成功持久化后
才从团队文件中移除。当前凭据后端是未加密的本机 `.env`，不能隔离同一系统用户的其他
程序；Windows 团队执行仍保持禁用。

Host slots never enter the process environment, including after configuration
reload. Commands receive only explicitly referenced variables found by the shell
parser. Quoted literals, indirect expansion and parse errors grant no variables.
Recipients are explicit current agent ids; an absent list authorizes nobody.

宿主凭据槽在保存及重载时都不会进入进程环境。命令只接收语法树中明确引用的变量；
单引号字面量、间接展开和无法解析的命令不会获得密钥。授权保存具体成员 ID，空名单
不再代表所有成员，也不自动覆盖未来成员。

Terminal outbox records are host input with unverified provenance and an ask
ceiling, not fresh user instructions. Exact `/msg` ids take precedence over all
display-name matches. Older builds must not write the slot-based secret file;
they cannot resolve its values.

终端 outbox 按来源未验证的宿主输入处理，并附带审批上限，不能冒充用户当次指令。
`/msg` 的精确 ID 优先于显示名称；旧版本无法解析凭据槽中的值，不应写入新格式密钥文件。

| Limit | Note |
| --- | --- |
| Windows | Team execution is disabled until the roster and inbox authority can be protected from child-process writes. Ordinary sessions remain available. |
| Secrets at rest | Immutable slots in the unencrypted global credential file, outside team metadata. Enforced macOS/Linux sandboxes deny reads; same-user programs outside that sandbox can still read the file. The form discloses this limit. |
| Secrets in commands | A command can read a value and print it transformed (base64, hex); the mask matches the value, not what was done to it. Give a secret only to agents you trust. |
| Secret scope | "Only Ada" governs which commands receive the variable, in all of Ada's seats, groups included. Session files are not isolated: another agent's commands can read Ada's, where what the user typed is kept as typed. |
| Shared folders | An agent's checks see only its own writes: another agent working in a folder they share can change what those checks rely on without either receipt saying so. |
| Signed bytes elsewhere | A brokered or extension-hosted model that verifies signed bytes holding a value can refuse the turn, since those bytes reach it masked. |
| Short values | Values under 8 bytes are masked only between non-word characters. |
| Memory review | Runs on the title model, which reads up to 3000 characters of each of the user's lines in the turn. |
| Mentions with spaces | A reference token ends at a space, so in "@Ada Lovelace" only "@Ada" is read, and it is looked up as an @file reference rather than taken out as a mention. |
| Terminal | `/team` lists the team and `/msg` queues a message, delivered when a Studio host holding the team ticks. ACP seats no session on the team. |
