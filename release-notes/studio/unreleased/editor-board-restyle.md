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
- 工作台里的子代理页签带它的颜色头像；顶部能「停止子代理」，主会话交给它的任务单独放在最上面，底部可以「回到对话里那一组」（95ec30ace）。
- 后台任务结束后不再消失：浮层分「在跑」和「已结束」两段，每行写用时、命令和最后一行输出，结束的还写退出码；停止要再点一次确定，结束的可以「清除」。底部读数在跑时是蓝色，有失败时变红，直到你打开看过（b4b7abd35）。
- 后台任务的「看输出」改为在工作台里开一个页签，不再用弹窗盖住对话：顶部写用时或「跑完了，退出 N」，在跑时可以停止；默认跟着最新输出，往上翻就停住，点箭头回到最新（bd9839a59）。
- 执行过程里，放到后台跑的命令写成「放到后台运行」，同一行跟着任务实时更新：在跑时写已跑多久，并说明不用等它、可以「看输出」；跑完后原地变成用时和退出码（8db18c129）。
- 对话右边的消息导航条按设计稿重做：横线按每条消息在整段对话里的位置铺满整列，不再挤在中间；灰底竖条标出现在看到的那一段；改过文件的消息横线长一点，星标是橙色。悬停时指着的那条和邻居像程序坞一样变长，左边的卡片写「你的第n条 · 改了n个文件」和两行内容；挤不下的连成一段，悬停展开带编号和星标的列表；还有更早没加载的消息时，顶上显示条数，点击加载；Ctrl ↑↓ 逐条走时当前那条有焦点框。点一条会平滑滚过去，那条消息闪一下。至少 3 条你的消息才显示（c91a653e9）。
- 「我的反馈」改成左右两栏：左边列表只看标题、类别、日期和一个词的状态（已收到 / 已登记 #n / 处理中 / 已在 vX 修复 / 下个版本修复 / 暂不做 / 重复），在等你回复的标橙排最前，有新回复的加粗带黑点；右边是这条反馈的一页：进度、完整往来和一直打开的回复框。标题栏放标签、「反馈等级 n · 已采纳 n 条」（点开看等级、账号与贡献榜）和关闭；说明文字挪到列表最下面一行小字（f2ddee747）。
- 回复里的「思考」按设计稿统一：思考中标题写「思考中 · 3.2 秒」并有一道扫光；展开时推理长到 6 行后往上滚，最新一行带光标；想完后标题淡入换成「思考 6.2 秒 · 214 字」，自动展开的会自动收起；停下或出错时标题变橙，写「思考到一半停下了 · 3.1 秒」，并说明已经想到的内容可以展开看（d33758664）。
- 手动展开或收起的「思考」会按会话记住，刷新后保持原样；回答流式输出时光标贴在最后一个字后面，每秒硬切闪一次（85f2679a2）。
- 「执行过程」按设计稿补齐动效：收起时标题写正在做的那一步和秒数，带转圈和淡扫光；操作数变化时往上滚；做完后标题写「✓ 4 项操作 · 38 秒」并收起。每一行右侧写结果（48 行、2 处、+12 −3、退出 1）；运行中转圈，完成时同一位置弹出勾；失败时抖一下，原因放在行下的红框里，可点「看完整输出」；跑了一会儿还没输出时显示「还没有输出 · 停止这一条」；新步骤依次淡入上浮（37981d0ff）。
- 思考结束时，旧标题淡出、新标题在同一位置淡入；执行过程里运行中的那一行从命令真正开始的时间计秒，重新打开也不会从零算起（caa509712）。
- 工作台页签栏按设计稿调整：资源管理器固定在最左并显示改动数，新开的标签显示为「+ 新标签」，右端可直接关闭工作台；新标签页改为靠左的「资源管理器 / 文件… / 浏览器」三项加「最近打开」列表；文件页底部新增状态栏，显示语言、光标所在行和「未保存」（a7786999c）。
- 工作台新增「改动」页：所有改动的文件排在一页，顶部写分支、文件数和增删行数，可「暂存全部 / 提交… / 推送…」；每个文件一个头，能勾选暂存、打开文件、丢弃（会再问一次），下面是带新旧行号的差异；新文件和未跟踪的文件默认收起。点输入框上方的「n 个文件已更改」会打开这一页（f04b06e3c）。
- 资源管理器的「改动 / 提交历史 / 文件」三段都能折叠；点「改动」标题进改动页；提交历史默认展开，右边写分支，每条显示短哈希、说明和时间，只有历史分叉时才画分支线（02f5a4432）。
- 文件页工具栏最右边改为「…」，打开和文件树右键同一份菜单（在系统文件管理器中显示、复制路径、复制引用、加入对话）；文件页的 Diff 改为新旧两列行号、保留 @@ 段落标题，和改动页一致（f7252b773）。
- 工作台页签：未保存的文件在关闭按钮的位置显示圆点，悬停才出现关闭；模型正在操作的网页在名字后面有蓝点；Go、Python、表格文件图标分色；放不下的页签折成「+n」列表。新开的页签展开进来、关闭时收窄消失、切换时内容淡入（d988b81a4）。
- 关闭有未保存内容的文件页签时会先问「保存 / 不保存 / 取消」；工作台打开时从右边滑入，用右上角关闭时滑出（89a079158）。

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
- A subagent's workbench tab carries its avatar, can stop the run from its head, shows the task the main session handed it at the top, and goes back to its group in the conversation from its foot (95ec30ace).
- Background jobs no longer vanish when they end: the list shows running jobs above ended ones, each with its duration, command and last line of output, an ended one with its exit status; stopping asks again, and ended jobs can be cleared. The rail reading is blue while one runs and stays red after a failure until you open it (b4b7abd35).
- A background job's output opens as a workbench tab instead of a dialog over the conversation: its head reads the run time or "Finished, exit N", a running job can be stopped there, and the output follows the newest line until you scroll up (bd9839a59).
- In the execution group a command sent to the background reads "Sent to the background", and its row follows the job live: how long it has run, with a note that the turn goes on and a way to its output, then how long it took and its exit status once it ends (8db18c129).
- The message rail beside the conversation follows the Msg-Rail board: marks sit at each message's place in the whole conversation and fill the column instead of bunching in the middle; a grey band shows the stretch on screen; a message that changed files draws longer and stars are orange. Hovering grows the pointed line and its neighbours and opens a card with "Your message n · changed n files" and two lines of the message; crowded marks join into segments that list their messages with numbers and stars; unloaded earlier messages show as a count at the top that loads them; Ctrl+Up/Down rings the mark it is on. A press glides to the message and flashes it. The rail appears from three of your messages (c91a653e9).
- My feedback is two columns: the list shows each report's title, category, day and where it stands in a word, with a question waiting on you in orange and first, and new replies in bold with a dot; the right is that report's page with its progress, the whole exchange and a reply box that is always open. The title bar carries the tabs, "Feedback level n · n adopted" (opening level, account and leaderboard) and close; the explanations moved to small print under the list (f2ddee747).
- Thinking in a reply follows the board: while live the title reads "Thinking · 3.2s" with a light sweeping across it; opened, the reasoning grows to six lines and then scrolls with the caret on the newest line; when done the title fades to "Thought 6.2s · 214 chars" and a block that opened on its own folds again; a turn stopped or failed mid-thought turns the title amber, "Stopped while thinking · 3.1s", and says what was thought is inside (d33758664).
- A thought you opened or closed stays that way after a reload, per conversation; while an answer streams its caret sits right after the last word and blinks once a second (85f2679a2).
- The activity group moves as the board draws it: folded while running, the title names the step under way and its seconds, with a spinner and a faint sweep; the action count rolls up as it grows; when done it reads "✓ 4 actions · 38s" and folds. Each row says what it came to on the right (48 lines, 2 matches, +12 −3, exit 1); a running step spins where its tick then pops; a failure shakes once and shows its reason in a red box with "See full output"; a step with no output for a while offers "Stop this one"; new rows fade up one after another (37981d0ff).
- When a thought finishes, its running title fades out as the settled one fades in; a running step counts its seconds from when it really started, so reopening does not restart the clock (caa509712).
- The workbench tab strip follows the board: the explorer is the fixed first tab with the change count, a new tab shows as "+ New tab", and the strip ends with a close button; the new tab lists Explorer / Find a file… / Browser and then recently opened files; a status line under the editor shows the language, the cursor line and "Unsaved" (a7786999c).
- The workbench has a Changes page: every changed file on one page under the branch, the file count and +/− lines, with Stage all / Commit… / Push…; each file has a header to stage it, open it or discard its change (asking once more), and its diff numbered before and after; new and untracked files start folded. The "n files changed" pill above the composer opens it (f04b06e3c).
- The explorer's Changes / History / Files sections each fold; the Changes title opens the changes page; History is open from the start with the branch on the right and each commit as its short hash, subject and age, drawing lanes only when history forks (02f5a4432).
- The file toolbar ends in "…", opening the same menu as the tree's right-click (reveal, copy path, copy reference, add to conversation); the file page's Diff numbers each line before and after and keeps the @@ headers, as the changes page does (f7252b773).
- Workbench tabs: an unsaved file shows a dot in place of its close button until hovered; the page the model works on keeps a blue dot after its name; Go, Python and sheet icons are coloured apart; tabs that do not fit fold into "+n". New tabs grow in, closing ones narrow away, and switching fades the page in (d988b81a4).
- Closing a file tab with unsaved text asks first: Save / Don't save / Cancel; the workbench slides in from the right when opened and out when closed from its own button (89a079158).
