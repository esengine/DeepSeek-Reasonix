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
关闭标签页或重启后仍可发现。新建 allocation 还会在 checkout 旁以 `0600` 权限写入
v1 `metadata.json`，绑定原始 source checkout、目标分支、创建时 `HEAD`、受管
worktree 根和临时分支。旧版本创建且没有该元数据的 worktree 无法使用 Merge-Back，
因为 Reasonix 不会猜测目标分支；界面会保留现场并给出手动合并指引。未知元数据版本
同样按失败关闭处理。

Merge-Back 是“合并、清理分离”的失败原子流程。预检会验证受管路径和仓库身份、精确
分支与 `HEAD`、source 干净且没有进行中的 Git 操作、全部可见或 detached Desktop
活动任务、工作区写租约、integrated terminal、ahead/behind、diff 和冲突。取得双
workspace lease 后，Desktop 会
短暂封闭 turn start 和 controller publication，再为 canonical source/worktree 两个根登记
贯穿 Git 变更的 reservation。项目 runtime owner、新 turn 以及 terminal create/write 都
经过同一 admission；子目录和 symlink 别名受保护，prefix sibling 与无关项目不受影响。
worktree 有未提交改动时默认禁止合并；只有用户显式开启
自动提交才会依次执行暂存和提交，并在新提交上重新做冲突预检。目标分支或 `HEAD`
发生漂移时只刷新确认面板，不会继续执行。确认还会绑定瞬态、NUL-safe 的内容 token，
覆盖每个脏路径的类型、mode、文件内容或 symlink 目标。`git add -A` 后必须证明 token
未变、没有残留的 unstaged/untracked 内容，且自动提交只有原 worktree `HEAD` 这一个父
提交并包含刚记录的 staged tree。source 合并使用 `git merge --no-ff --no-commit`，并把
实际 index tree 与重新计算的 merge-tree 精确绑定；准备前及安装 ref 前都会重新验证
worktree root、Git common-dir、symbolic branch、branch ref、`HEAD`、Git operation 和内容
token。只有这些身份、目标分支、原始 `HEAD`、精确 `MERGE_HEAD` 和 prepared tree 都仍
一致时，才通过无 hook 的 `commit-tree` 创建固定 parents/tree 的提交。单个
`update-ref --stdin` transaction 会同时验证 worktree branch ref，并用原目标 `HEAD` 对
target ref 做 compare-and-swap，避免任一 ref 检查部分生效。提交后还会复核两个 checkout、
commit tree、真实 index tree、parents、refs、干净状态和 Git operations。CAS 前仍由
Reasonix 拥有的准备失败会执行 abort；target ref 漂移、CAS 后 worktree 漂移或无法证明
恢复成功的状态返回 recovery-required，同时保留所有 worktree 资源和外部状态。

合并成功后，Reasonix 先通过正常 Desktop 生命周期切换到记录的 source checkout；
若此时出现更新的前端导航意图，后续关闭和清理会停止并保留资源。否则后端只会在精确
source Tab 仍 active、精确 worktree Tab 仍 idle 时关闭页面和终端。独立、可重试的清理
阶段会在同一临界区内登记 canonical worktree reservation 并扫描可见及 detached
runtime；所有项目 runtime 的创建、恢复、删除/归档 fallback 和重定向都经过同一
admission gate。reservation 按“路径位于受管 worktree 内”匹配，而不是只比较根路径，
因此即使 Git worktree 根已被移除，保存的 symlink、子目录或晚到 runtime 仍不能在检查
后插入。之后还必须证明临时提交已包含在目标分支、身份仍一致且
`git status --ignored` 完全为空。
清理仅使用普通 `git worktree remove` 和 `git branch -d`，绝不强制删除。tracked、
untracked 或 ignored 文件都会阻止清理并向用户列出；合并结果仍然保留，worktree、
分支和文件可供恢复或之后重试清理。

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
