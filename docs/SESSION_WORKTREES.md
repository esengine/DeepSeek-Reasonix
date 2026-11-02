---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-10-15
---

# Session worktrees / 会话工作树

## 中文

在有提交记录的 Git 项目中新建会话，点击输入框内底部工具栏的分支名：

1. 在「当前目录 / 独立工作树」中选择「独立工作树」。选择「当前目录」可返回直接使用项目文件夹的模式。
2. 点击「起始分支」，搜索并选择本地分支或已获取的远程分支。
3. 输入任务并发送。Reasonix 此时创建独立目录和自动命名的任务分支，再在其中开始对话。

打开弹层或选择分支不会修改代码，也不会切换原目录的分支。即使起始分支已被其他工作树使用，也可以从它创建独立任务。远程分支列表来自本地 Git 记录；需要更新时先在终端运行 fetch。

工作树使用分支的已提交版本，不包含未提交代码、依赖安装或环境文件。草稿中已上传的附件会复制到新目录，保持引用可用。模型、推理强度、工作模式和显式选择的执行权限随会话保留；依赖目录信任的默认权限按新目录重新判断。

创建或初始化失败时保留草稿和原会话，并撤回尚未交付的工作树。发送请求失败后可以重试，已创建的工作树继续使用。已交付的工作树会保留在工作区列表中，关闭会话不会自动删除代码。

非 Git 目录、尚无提交的仓库、已有对话以及已受管理的工作树不显示新会话的 worktree 配置。已有对话继续使用原来的分支切换功能。此功能隔离代码目录，不隔离端口、数据库或运行环境。

## English

Start a new conversation in a Git project with a commit, then click the branch name in the composer's bottom toolbar:

1. Choose **Isolated worktree** instead of **Current folder**. Select **Current folder** to return to working directly in the project folder.
2. Open **Base branch** and search local or fetched remote-tracking branches.
3. Send the task. Reasonix creates a separate directory and automatically named task branch before submitting the first message there.

Choosing a branch does not check it out in the source folder. A branch used by another worktree is still a valid starting point. Remote branches come from local Git refs; fetch in a terminal to refresh them.

Only committed code is checked out. Uncommitted edits, installed dependencies and environment files are not copied. Uploaded draft attachments retain their relative references in the new directory.

Model, reasoning effort, plan mode and explicitly chosen approval mode are retained; default approval based on folder trust is recalculated for the new directory.

Preparation failures preserve the original conversation and draft and roll back unpublished worktrees. Retrying a refused message reuses the prepared directory. Published worktrees remain in the workspace list and survive closing the conversation.

The control is unavailable in non-Git folders, repositories without commits, existing conversations and already managed worktrees. Existing conversations retain the branch switcher. Worktrees isolate code directories, not ports, databases or runtime environments.
