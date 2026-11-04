## 变更

- 窗口顶栏改成设计稿的 48px：面包屑 13px，右侧四个 28px 图标按钮依次是查找、设备访问、主题、工作台；空会话的标志放进 44px 的强调色方块（8b93d8e57）。
- 侧栏底部收成一行图标：用量、文件、工具与权限、扩展、记忆、远程，设置在最右，悬停和读屏都带当前读数；「发送反馈」单独一行带未读数，账号入口移进设置；「工具与权限」读出当前审批方式（8b93d8e57、5a7a1c8aa）。
- 输入框改成设计稿的样子：圆角 15、唯一的阴影；只有运行中或有文件改动时才出现顶部那一行；工具栏是 28px 无边框胶囊，模型名 13px 加粗，发送和停止是 32px 圆形，窄窗口只留图标（cfcdecaec）。
- 对话里你的消息是圆角 15 的浅底气泡；执行过程收成一张卡，每行是图标、动作（读取、编辑、运行命令…）、对象、读数，状态勾在最右，工具原名悬停可见（c62573956）。
- 生成的文件改成单列卡片：小图标、文件名、说明、「打开」和「···」；空会话的背景不再画网格和光晕（7e75a4adf）。
- 工作台照设计稿重画：页签栏 42px、文件树在最前、当前页签像文件夹标签；路径栏最后一段加粗，阅读/编辑/Diff 是分段按钮，保存是主按钮；默认宽 520，可拖到 360–900。命令面板也换成设计稿的样子（2daa590e5）。
- 记忆页按设计稿复核：「没错」「编辑」移到详情标题右侧，详情分「内容」「使用」两组，列表只在快淡忘或已淡忘时写一个词（e1ab56d58）。
- 新样式的字号、字重会跟随设置里的字号和字重；macOS 浏览器里的文字也按设计稿的方式平滑，不再显得偏粗（e9ae8f1f8）。
- 反馈中心的标签、筛选和状态改成设计稿的分段按钮与彩色药丸（c0870f92b）。
- 思考块按设计稿重排（852a19594）。
- 计划只是一张卡：待确认时列出每一步和「开始执行 / 修改计划 / 暂不执行」，开始后同一张卡原地显示第几步、正在做什么，做完变成「做完了」；执行记录挂在卡片下面。卡片滚出视野时，输入框上方才出现一个小页签，写着第几步并用小格子显示进度，展开后可「跳到卡片 ↑」（11b2e23b4）。
- 执行过程里连续读取的文件不再合成「读取了 N 个文件」一行，每次读取单独一行（977c7c6b8）。
- 同时派出的几个子代理在对话里是一张卡：标题写几个并行、几个在跑、几个失败，每个子代理一行，带固定颜色的头像、名字、任务和进度；失败的直接写出原因，可以单独停止或全部停止，点一行在工作台打开；全部交付后收成一行汇总（b1b42c3d8）。
- 底部的子代理读数：有子代理在跑时是带呼吸点的蓝色「N 个子代理在跑」，都结束后变回灰字「N 个子代理 · 都结束了」；悬停弹出的列表和对话里的组卡一样带颜色头像、任务和状态，可以全部停止（68e703888）。

---
<!-- lang:en -->

## Changed

- The window bar is the board's 48px: a 13px breadcrumb and four 28px icon buttons — find, device access, theme, workbench; the empty session's mark sits in a 44px accent tile (8b93d8e57).
- The sidebar foot is one row of icons — usage, files, tools, extensions, memory, remote — with settings at the far end, each naming its current reading on hover and to screen readers; Send feedback keeps its own row with the unread count, and the account entry moves into settings; the tools entry reads the current approval mode (8b93d8e57, 5a7a1c8aa).
- The composer follows the board: a 15px box with the only composer shadow; the top row appears only while a turn runs or files have changed; the toolbar is 28px borderless pills, the model name is 13px semibold, send and stop are 32px circles, and narrow windows keep icons only (cfcdecaec).
- Your messages are 15px-radius tinted bubbles; the execution process is one card whose rows read glyph, verb (Read, Edit, Run command…), target and reading, with the status mark at the right end and the raw tool id on hover (c62573956).
- Delivered files are single-column cards with a small icon, the name, its description, Open and a more button; the empty conversation no longer paints a grid and glow behind it (7e75a4adf).
- The workbench is redrawn to its board: a 42px tab strip with the file tree first and the current tab as a folder tab, a path bar with the file name in bold, read/edit/diff as a segmented control and Save as the primary button; it opens at 520px and drags from 360 to 900. The command palette takes its board look too (2daa590e5).
- The memory page follows its review board: Confirm and Edit sit beside the title, the facts read as content and use, and a list row names its state only when it is fading or forgotten (e1ab56d58).
- Type in the redesigned surfaces follows the text-size and weight settings, and text in a macOS browser is smoothed as the design canvas draws it instead of looking heavier (e9ae8f1f8).
- The feedback center's tabs, filters and statuses take the board's segmented control and filled pills (c0870f92b).
- Reasoning follows its board (852a19594).
- A plan is one card: before it starts it lists every step with Start, Revise and Not now; once started the same card shows which step it is on and what it is doing, and becomes Done at the end, with the execution filed under it. Only while the card is out of view does a tab above the composer show the step and one segment per step, and opened it offers Back to the card (11b2e23b4).
- Consecutive reads in the execution group are no longer one "read N files" line; each read is its own row (977c7c6b8).
- Subagents sent out together are one card in the conversation: how many run in parallel, how many are running or failed, then one row per subagent with a fixed-colour avatar, its name, task and progress; a failure states its reason, each can be stopped alone or all at once, a row opens in the workbench, and once all have delivered the card folds to one summary line (b1b42c3d8).
- The run rail's subagent reading is a blue "N subagents running" with a breathing dot while one runs and grey "N subagents · all finished" after; its list carries the same avatars, tasks and states as the conversation's group and can stop them all (68e703888).
