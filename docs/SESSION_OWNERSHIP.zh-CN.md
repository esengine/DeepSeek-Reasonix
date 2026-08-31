# 会话所有权、回溯与 worktree 回退

<a href="./SESSION_OWNERSHIP.md">English</a>

Reasonix 如何决定谁可以写会话、冲突如何落盘，以及回溯和工作区隔离如何配合。

## 会话写者

同一会话文件同一时刻只有一个跨进程写者。票据和持有者信息共用一个 session
lease 文件（`.lease.lock`）。生产路径上的 Controller 绑定带 generation 的
`SessionWriter`；重新绑定会使旧 generation 立即失效。旧 `.lease.json` 仅作为
只读兼容来源。

主动路径切换（`new`、`clear`、`fork`、`branch`、`switch`）采用“先准备、后发布”
的交接：前端先取得目标 lease，并给尚未发布的 Session 绑定写权限，然后 Controller
才替换路径。非破坏性切换失败时会保留源 Controller 及其 lease；`clear` 也不会
发布一个没有 lease 的替代 Session。

所有保存仍会获取有界等待的 `.jsonl.lock` 兼容锁。session lease 决定谁可以
长期拥有 transcript；save lock 则让该 owner 与仍受支持的旧版本，以及尚未
接入 lease 协议的一次性 recovery/import 写者保持互斥。

事件日志（`.events.jsonl`）是权威来源。绑定写者的保存对日志尾部（size +
index 的 revision/digest）做 CAS，并用配对的内存 transcript 视图判断
no-op / append / replace。`.jsonl` 仍是兼容投影。

## 冲突

1. 事件日志尾部仍匹配当前写者 → 正常保存。
2. 磁盘已经覆盖本地前缀 → 采用磁盘版本，不建分支。
3. 真正分歧、日志被替换或原会话被删除 → 写入一条由根 branch ID + 当前
   Session 首次 writer generation 决定的稳定 recovery 文件。lease 重绑不会
   改变该 lane；后续冲突更新同一路径，不再嵌套。

## 回溯

- **代码**：恢复 before-image。当前已等于 before 的文件跳过；外部修改拒绝覆盖。
- **对话**：创建新会话分支。父会话 transcript 永不截断。
- **两者**：先分叉，再恢复文件。文件冲突时保留新分支并返回 `partial=true`。

新 checkpoint 写入 `turns/<turn>/meta.json` 和原始字节
`files/NNNN.before`（schema v3）。默认保留最近 100 个回合目录；新 checkpoint
不再把载荷重复写入 blob。旧的 v1/v2 `turn-N.json` 及其 blob 仍可读。

v2 兼容 marker 同时也是 v3 turn 的存活标记。旧版本截断 `turn-N.json` 后，
对应的 v3 目录会被视为 tombstone；再次升级不会让已经删除的未来 checkpoint 复活。

结构化写工具在发布前重新校验存在性、SHA-256 和 mode，不匹配则返回
`ErrFileChanged`。

## Worktree 回退

从消息分叉时可以选择两种工作区策略。**仅分叉对话（共享工作区）**继续使用源工作区，
因此会保留并继续看到当前未提交文件。**隔离 worktree** 则从仓库已提交的 `HEAD`
创建持久的 `reasonix/delivery-*` 分支，把新分叉注册为独立项目，并保持源 checkout
不变。Git worktree 不会复制本地改动，所以组合分叉要求源 checkout 干净；检测到
未提交或未跟踪文件时，Reasonix 会拒绝创建，并提示先 commit/stash，或改用共享分叉。

如果当前目录不是 Git 项目，或环境不满足 worktree 前提，Reasonix 会在共享工作区中
完成会话分叉并明确提示已回退。如果 worktree 创建后，会话创建或标签页挂载失败，
自动清理只会删除分支、`HEAD` 和状态仍与创建结果完全一致的未使用 worktree；一旦
检测到任何变化，就会保留现场以便恢复。成功挂载的 worktree 会作为项目持久注册，
关闭标签页或重启后仍可发现，并且不会被自动删除；使用完毕后需要显式清理 Git
worktree 和对应分支。

Delivery worktree 仍是可选能力。非隔离目录使用 workspace lease（`filelock`）。
路径型写入对祖先兼容锁和目标路径层级分片加 shared 锁、对具体文件分片加
exclusive 锁，且只在该次 tool 期间持有。整区写入会独占精确根锁和对应层级分片，
因此父工作区与直接打开的嵌套仓库能够互斥，而两个会话仍可同时写不同文件（包括同一
仓库）。`bash`/MCP 的写操作只在该命令期间独占整区；若配置的 tool hook 可能写入
未声明路径，任何 tool 调用都会改用整区锁。文件和层级身份都映射到有界锁分片；哈希
碰撞最多让无关工作串行，不会削弱保护。只读 bash 不拿写锁。冲突卡片会说明正在写的
文件或整区。macOS 使用折叠身份协调大小写别名，同时保留原始大小写根锁兼容旧版；
旧版进程仍只认识它打开时的路径拼写，跨拼写共存需要双方都使用新协议。Git 不是运行
前提；对话结束后不继续占锁。需要长期隔离工作树时再用 worktree。
