import { t } from "../i18n";

// The settings page names a tool by what it does, in the interface's language. The
// kernel's description is written for the model and is only the fallback for a tool
// this build has no line for, such as one an MCP server declares.
export function toolBlurb(name: string): string | undefined {
  switch (name) {
    case "advise":
      return t("向更强的模型求助：它读完到目前为止的对话后，用方案、诊断或审查回答你的问题；它自己不执行任何东西。");
    case "ask":
      return t("向你提问：遇到只有你能决定或提供的事时，模型用它来问你。");
    case "await_user":
      return t("把控制权交还给你，任务清单保持未完成，等你给出下一步需要的东西。");
    case "bash":
      return t("在命令行里执行命令并返回输出，用于构建、测试、git、包管理等。");
    case "bash_output":
      return t("读取后台任务新产生的输出。");
    case "best_of_n":
      return t("把一个明确的编码任务并行做 2–4 遍，每遍在独立的 git 工作树里；再由评审比较，只把最好的一份写回工作区。");
    case "browser_act":
      return t("在浏览器页面上执行真实操作，比如点击和输入。");
    case "browser_open":
      return t("打开一个网页并返回页面快照。");
    case "browser_read":
      return t("读取浏览器页面的内容：快照、查找或截图。");
    case "code_index":
      return t("内置的轻量代码符号索引：按名字查函数、类型等符号的位置。");
    case "complete_step":
      return t("记录已批准计划中某一步的完成情况和依据。");
    case "compress":
      return t("压缩当前对话中指定的一段，不删除可见历史；只在你明确要求时使用。");
    case "computer_act":
      return t("操作一个已获准的应用程序：点击、输入、按键、滚动。");
    case "computer_read":
      return t("查看一个已获准的应用程序：列出应用、读取窗口快照或截图。");
    case "conclude_blocked":
      return t("在查清原因后声明任务有一部分按要求做不了，或需要你来决定。");
    case "context_budget":
      return t("报告当前对话在自动压缩前还剩多少上下文空间。");
    case "delete_range":
      return t("用起止文本锚点删除文件里的一整段内容，适合大块删除。");
    case "delete_symbol":
      return t("按名字从 Go 源文件里删除一个函数、方法或类型等符号。");
    case "dispatch_to_project":
      return t("把任务交给你另一个项目里的会话：指定项目并写明要做什么；对方没有等待中的会话时会在那里新开一个。");
    case "open_projects":
      return t("请你把一批文件夹加入项目列表：卡片逐条列出路径，你批准后才会加入并标记为可信，之后才能向它们派发任务。");
    case "docs":
      return t("搜索并阅读内置在本版本里的官方文档。");
    case "edit_file":
      return t("把文件里一处精确的文本替换成另一段，适合定点修改。");
    case "explore":
      return t("在隔离的子代理里做只读的代码库调查，只返回提炼后的结论。");
    case "fleet":
      return t("把 2–64 个子代理任务按依赖关系一起派出，返回摘要和取回完整结果的引用。");
    case "forget":
      return t("按名称删除一条错误或过时的已保存记忆，让它不再载入以后的会话。");
    case "glob":
      return t("按通配符模式查找文件，比如 *.go 或 **/*.test.ts。");
    case "grep":
      return t("用正则表达式搜索文件内容，返回匹配的行；跳过隐藏文件和 .gitignore 忽略的文件。");
    case "history":
      return t("搜索本机保存的历史会话，并读取命中位置前后的消息。");
    case "import_web_image":
      return t("把 web_fetch 取到的网页图片导入工作区的新文件，并校验是 PNG、JPEG、GIF 或 WebP。");
    case "install_skill":
      return t("编写并保存一个新技能，以后的对话可以反复调用。");
    case "install_source":
      return t("规划、安装或卸载技能、MCP 服务器或插件包；先预览，确认后再执行。");
    case "kill_shell":
      return t("终止一个仍在运行的后台任务。");
    case "list_peers":
      return t("列出本机上正在等待消息的其他 Reasonix 会话。");
    case "list_sessions":
      return t("列出已保存的对话，最新的在前，含时间、模型、轮数和预览。");
    case "list_subagents":
      return t("列出当前对话派出的子代理，最新的在前，附取回完整结果所需的引用。");
    case "ls":
      return t("列出目录里的条目：目录带斜杠、文件显示字节数，可递归。");
    case "lsp_definition":
      return t("跳转到符号的定义位置，由语言服务器提供。");
    case "lsp_diagnostics":
      return t("报告某个文件的编译器和代码检查诊断，改完文件后用来确认能通过编译。");
    case "lsp_hover":
      return t("显示符号的类型签名和文档。");
    case "lsp_references":
      return t("列出工作区里对某个符号的所有引用。");
    case "memory":
      return t("搜索、列出并读取本项目已保存的背景记忆；保存新记忆前先用它避免重复。");
    case "move_file":
      return t("移动或重命名文件，必要时创建目标目录。");
    case "multi_edit":
      return t("对同一个文件一次应用多处修改，全部成功才写入，任何一处失败都不会改动文件。");
    case "notebook_edit":
      return t("修改 Jupyter 笔记本（.ipynb）里的一个单元格，可替换或插入。");
    case "parallel_tasks":
      return t("同时派出多个只读子代理任务，全部完成后返回摘要。");
    case "present_files":
      return t("把已有的最终文件以可点击的卡片展示给你，包括命令行生成的文档；路径须在工作区或已授权的文件夹内，打开的是当前文件而不是快照，也不保证内容正确。");
    case "read_file":
      return t("读取文本文件，可指定起始行和行数，输出带行号。");
    case "read_mcp_resource":
      return t("读取已连接的 MCP 服务器自己列出的一份资源（文档、记录、文件）；服务器没列出的地址会被拒绝，返回的是服务器的数据，不是你的指令。");
    case "read_only_skill":
      return t("以只读方式调用一个技能；子代理类技能在只读的隔离会话里运行。");
    case "read_only_task":
      return t("派出一个只读的研究子代理做专项调查，它在隔离会话里只用只读工具。");
    case "read_session":
      return t("按文件名读取一个已保存的对话，返回限长且保护隐私的视图。");
    case "read_skill":
      return t("把技能索引里的一份内联手册读进上下文，但不执行任何东西。");
    case "read_subagent_result":
      return t("按引用读取已完成子代理的完整最终答案。");
    case "read_video":
      return t("查看一段视频：模型能看视频就直接看，否则用 ffmpeg 抽帧。");
    case "recall":
      return t("找回已折叠的对话内容：按压缩摘要里的位置读取，或在折叠区域里搜索。");
    case "remember":
      return t("保存一条长期有效的背景事实，让它跨会话保留。");
    case "research":
      return t("在隔离的子代理里结合网页内容和本地代码做调研，适合既要外部资料又要本地验证的问题。");
    case "review":
      return t("在隔离的子代理里审查待提交的改动，指出正确性、安全、缺测试和隐藏的行为变化；只读。");
    case "run_script":
      return t("运行一小段 Starlark（类 Python）脚本来调用多个工具，几步相互依赖的操作只需一次往返，只返回脚本打印的内容；每次调用都和模型直接调用一样受检查和审批。");
    case "run_skill":
      return t("调用系统提示里技能索引中的一份技能手册。");
    case "security_review":
      return t("在隔离的子代理里对当前分支的改动做安全审查，按严重程度标注；只读。");
    case "send_peer_message":
      return t("向本机另一个 Reasonix 会话发送文字；对方用户批准后它的模型才会读到，发送本身不授予任何权限。");
    case "setting":
      return t("查找并修改你自己的设置；改动前会先征得你同意。");
    case "slash_command":
      return t("按名称调用项目里的斜杠命令，返回展开后的提示文本供本轮执行。");
    case "task":
      return t("派出一个子代理处理一项专项子任务，只返回它的最终答案。");
    case "todo_write":
      return t("记录并更新当前工作的任务清单，每次发送完整清单。");
    case "update_goal":
      return t("汇报当前目标这一轮的处理结果。");
    case "use_capability":
      return t("按稳定的 ID 搜索并调用延后加载的工具、技能和 MCP。");
    case "view_image":
      return t("查看工作区里的一张图片（PNG、JPEG、GIF、WebP），需要支持图片的模型。");
    case "wait":
      return t("等后台任务结束，返回每个任务的状态和最终输出。");
    case "web_fetch":
      return t("获取一个网址并返回文本内容；HTML 会被精简为可读文本。");
    case "web_search":
      return t("搜索网页获取最新信息，返回摘要和来源。");
    case "write_file":
      return t("写入完整的文件内容，覆盖原文件并创建所需目录；只改一部分请优先用 edit_file 或 multi_edit。");
    default:
      return undefined;
  }
}
