## 变更

- 窗口顶栏改成设计稿的 48px：面包屑 13px，右侧四个 28px 图标按钮依次是查找、设备访问、主题、工作台；空会话的标志放进 44px 的强调色方块（8b93d8e57）。
- 侧栏底部收成一行图标：用量、文件、工具与权限、扩展、记忆、远程，设置在最右，悬停和读屏都带当前读数；「发送反馈」单独一行带未读数，账号入口移进设置；「工具与权限」读出当前审批方式（8b93d8e57、5a7a1c8aa）。
- 输入框改成设计稿的样子：圆角 15、唯一的阴影；只有运行中或有文件改动时才出现顶部那一行；工具栏是 28px 无边框胶囊，模型名 13px 加粗，发送和停止是 32px 圆形，窄窗口只留图标（cfcdecaec）。
- 对话里你的消息是圆角 15 的浅底气泡；执行过程收成一张卡，每行是图标、动作（读取、编辑、运行命令…）、对象、读数，状态勾在最右，工具原名悬停可见（c62573956）。
- 生成的文件改成单列卡片：小图标、文件名、说明、「打开」和「···」；空会话的背景不再画网格和光晕（7e75a4adf）。

---
<!-- lang:en -->

## Changed

- The window bar is the board's 48px: a 13px breadcrumb and four 28px icon buttons — find, device access, theme, workbench; the empty session's mark sits in a 44px accent tile (8b93d8e57).
- The sidebar foot is one row of icons — usage, files, tools, extensions, memory, remote — with settings at the far end, each naming its current reading on hover and to screen readers; Send feedback keeps its own row with the unread count, and the account entry moves into settings; the tools entry reads the current approval mode (8b93d8e57, 5a7a1c8aa).
- The composer follows the board: a 15px box with the only composer shadow; the top row appears only while a turn runs or files have changed; the toolbar is 28px borderless pills, the model name is 13px semibold, send and stop are 32px circles, and narrow windows keep icons only (cfcdecaec).
- Your messages are 15px-radius tinted bubbles; the execution process is one card whose rows read glyph, verb (Read, Edit, Run command…), target and reading, with the status mark at the right end and the raw tool id on hover (c62573956).
- Delivered files are single-column cards with a small icon, the name, its description, Open and a more button; the empty conversation no longer paints a grid and glow behind it (7e75a4adf).
