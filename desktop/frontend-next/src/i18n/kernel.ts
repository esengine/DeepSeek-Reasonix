import { DeliveryError, HttpError } from "../port/port";
import type { DeliveryFault } from "../port/port";
import { t } from "./index";

// What the kernel says when it refuses, in the language the reader uses.
//
// The kernel does not speak to people: it answers with a code and the pieces a
// sentence needs. That split is deliberate — the same refusal has to reach a
// Chinese window, an English window, a log and a curl, and only the frontend
// knows which of those it is. Wording here, decisions there.
//
// The map goes code → Chinese source text, which then runs through the ordinary
// t(): one translation mechanism for the whole app rather than a second
// catalogue keyed by codes.
// Codes a caller branches on rather than only renders. The table below keeps
// its literal keys — the kernel's parity guard reads this file as text and can
// only see those — so kernel.test.ts holds the two spellings together.
export const PROVIDER_EDIT_DISABLED = "provider.editing_disabled";
// A paired device answers this for every account route, votes included.
export const ACCOUNT_SIGNIN_DISABLED = "account.signin_disabled";
// The write landed and only applying it to the open conversation did not.
export const SAVED_NOT_APPLIED: readonly string[] = ["provider.saved_while_running", "provider.saved_model_unlisted", "runtime.rebuild_failed", "runtime.saved_while_running"];

const SAID: Record<string, string> = {
  "shell.destructive_target": "递归删除目标受保护或超出授权范围；请使用工作区或授权目录内的字面路径",
  "shell.analysis_unknown": "无法确定递归删除范围；请使用字面命令名、路径和解释器内容",
  "shell.delete_sequence": "请拆分命令；递归删除前只能使用字面路径切换目录，不能依赖变量赋值",
  "shell.delete_nonliteral": "请使用授权目录内的字面删除路径，不要使用变量、展开、通配符或未知管道输入",
  "shell.delete_option": "删除选项无法识别；请使用已知选项并提供公共参数的值",
  "shell.syntax_error": "命令语法有误；请修正后重试",
  "shell.parser_unavailable": "主机命令解析器不可用；请在主机恢复后重试",
  "shell.parser_timeout": "主机命令解析超时或被取消；请重试",
  "shell.command_line_too_long": "命令超过主机长度限制；请拆分命令或从文件读取长文本",
  "tool.arguments_invalid": "参数不符合该工具的约定，本次调用没有执行；请按工具说明补全或更正参数后重试",
  "workspace.write_conflict": "另一个会话持有所需的写入范围。本次操作未执行；请结束当前轮次，待该范围释放后再重试",
  // ── 忙：不是出错，是「现在不行」 ─────────────────────────────────
  "plan.decision_stale": "该决定已不符合当前状态：计划在你回答前已发生变更",
  "busy.switch_model": "当前回合正在进行，请先停止或等它结束再切换模型",
  "busy.switch_model_jobs": "回合已结束，但还有 {count} 个后台任务在运行，切换模型会终止它们。请先在「后台任务」里停止，再切换模型",
  "busy.change_effort": "任务正在运行，请先停止再调整推理强度",
  "busy.change_workspace": "任务正在运行，请先停止再切换工作区",
  "busy.reload_extensions": "任务正在运行，请先停止再重载扩展",

  // ── 冲突：有东西挡着 ─────────────────────────────────────────────
  "workspace.has_open_panes": "该文件夹仍有 {n} 个打开的面板，请先关闭再移除",
  "workspace.file_invalid": "文件保存请求格式不正确",
  "workspace.file_changed": "文件已在打开后被其他操作修改，请重新载入",
  "workspace.file_missing": "找不到该文件，它可能已被移动或删除",
  "workspace.file_failed": "文件操作失败，请重试",
  "workspace.path_outside_tree": "该路径不在当前工作区内",
  "workspace.write_outside_scope": "写入目标不在工作区可写范围内。这是文件工具的写入范围，不是操作系统沙箱；要允许写入，请在 设置 → 沙箱 → 额外可写目录 中添加目标文件夹",
  "workspace.network_path_outside_scope": "该路径指向网络上的另一台机器，不在当前工作区内。文件工具只按路径写法拒绝它，不会去访问；工作区本身在网络共享上时，其下的路径可用",
  "workspace.files_failed": "无法读取工作区文件列表",
  "workspace.file_unreadable": "该文件不是可编辑文本或超过大小限制",
  "provider.model_in_use": "该来源正在使用中，请先切换模型再删除",

  // ── 来源：填错了什么 ─────────────────────────────────────────────
  "provider.name_required": "请为该来源填写名称",
  "provider.name_invalid": "名称只能用字母、数字、点、连字符和下划线，以字母或数字开头，最长 64 个字符",
  "provider.name_taken": "已经有名为「{name}」的连接了，换一个名称",
  "provider.config_unreadable": "读不到配置文件，没法安全地选择密钥存放位置，请检查配置后重试",
  "provider.endpoint_required": "请填写接口地址",
  "provider.kind_unsupported": "无法识别「{kind}」这种接入方式",
  "provider.no_models_picked": "请至少选择一个模型",
  "provider.default_not_selected": "默认模型「{model}」不在已选择的模型中",
  "provider.display_name_too_long": "显示名称最多 {max} 个字符",
  "provider.display_name_invalid": "显示名称不能包含换行或其他控制字符",

  // ── 来源：这个协议做不到 ─────────────────────────────────────────
  "provider.no_thinking_param": "该协议不发送思考参数，启用后不会生效",
  "provider.no_continuation": "该协议在轮次之间不保留状态，无需选择续接方式",
  "request.bad_body": "无法解析本次请求的内容，请刷新页面后重试",
  "request.missing_field": "缺少「{field}」",
  "request.not_found": "找不到名为「{name}」的{kind}",
  "project.unknown": "该项目未在当前窗口中打开",
  "workspace.unknown": "该工作区未在当前窗口中打开",
  "busy.session_in_use": "该会话正被其他位置占用：{detail}",
  "busy.session_running": "该会话正在运行，此消息已排入队列",
  "busy.session_active": "这是当前打开的会话，请先切换后再删除",
  "request.bad_value": "「{field}」只能为以下值之一：{allowed}",
  "session.bad_name": "会话名不能是路径",
  "session.bad_path": "无法解析该会话路径",
  "session.open_failed": "打不开这个会话",
  "session.outside_dir": "该路径位于会话目录之外",
  "session.unknown": "没有这个会话",
  "request.method_not_allowed": "该地址不接受此种请求方式",
  "request.bad_content_type": "请求体必须是 application/json",
  "permissions.editing_disabled": "这台服务器未开放权限编辑",
  "permissions.rule_required": "请选择要收回的授权规则",
  "permissions.busy": "任务正在运行，请先停止再收回已记住的授权",
  "permissions.rebuild_unavailable": "当前会话无法重建，授权未更改",
  "project_grants.unavailable": "无法更新当前项目的授权记录，请检查用户目录中的授权文件",
  "sandbox.editing_disabled": "这台服务器未开放沙箱编辑",
  "browser_tools.editing_disabled": "这台服务器未开放内置浏览器设置",
  "roles.editing_disabled": "这台服务器未开放角色编辑",
  "storage.moving_disabled": "这台服务器未开放存储迁移",
  "storage.move_running": "已有一个迁移正在进行，请等待其完成",
  "plugin.bad_name": "这不是有效的插件名称",
  "plugin.not_installed": "该插件未安装",
  "wallpaper.not_base64": "图片数据不是 base64 编码",
  "shell.unavailable_over_http": "HTTP 上不提供 shell 命令",
  "roles.unknown": "不存在「{role}」这个角色",
  "roles.model_unknown": "没有已配置的模型匹配「{model}」",
  "model.decision_only": "「{model}」是决策来源，只服务 system_one 的询问，不能用作对话模型",
  "model.not_decision_source": "「{model}」是对话模型，不是决策来源；决策只能选择「模型服务」里添加的决策来源",
  "roles.override_not_in_user_config": "「{key}」不在用户配置里，可能已被清除，或来自项目配置",
  "shell.editing_disabled": "这台服务器未开放 shell 设置",
  "account.signin_disabled": "这台服务器未开放账号登录",
  "backup.signed_out": "登录账号后才能使用云备份",
  "backup.cloud_unavailable": "暂时连不上备份服务，稍后再试：{detail}",
  "backup.label_too_long": "备注最多 {max} 个字",
  "backup.collect_failed": "读取本机配置时出错：{detail}",
  "backup.preview_failed": "无法和本机配置比对：{detail}",
  "backup.weak_passphrase": "加密口令至少 {min} 个字符",
  "backup.bad_categories": "至少选择一类要备份的内容",
  "backup.cannot_decrypt": "口令不对，或这份备份已损坏",
  "backup.unsupported_format": "这份备份来自更新的版本，请先升级 Studio",
  "backup.malformed": "这份备份的内容无法识别",
  "backup.too_large": "备份超出了大小上限",
  "backup.plan_expired": "预览已失效，请重新打开这份备份",
  "backup.unknown_item": "所选的项目不在这份备份里",
  "backup.not_found": "这份备份已不存在",
  "backup.limit_reached": "备份数量已达上限，请先删除一份旧备份",
  "backup.consent_required": "有会在本机运行程序或改变密钥去向的项目，需要逐项确认后才能恢复",
  "workspace.limit_reached": "项目列表已满（32 个），请先移除一个项目再添加",
  "workspace.changing_disabled": "这台服务器不支持切换工作区",
  "settings.unknown_preset": "不存在该预设",
  "settings.unknown_model": "没有找到这个模型，默认模型没有改动",
  "settings.default_model_brokered": "这个窗格的默认模型由模型所在的那台机器决定，请在本机的窗格里设置",
  "drop.too_many_paths": "本次拖入 {count} 个，最多允许 {limit} 个",
  "attachment.unsupported_image": "这个文件的格式暂不支持（{format}）。支持的图片格式：{supported}。可以先转换格式再添加。",
  "attachment.too_large": "这个文件超过 {limit_mb} MB 的上限，请压缩或拆分后再添加。",
  "attachment.empty": "这个文件是空的（0 字节），没有可添加的内容。",
  "attachment.unreadable": "无法读取这个文件：{detail}。它可能被其他程序占用、已被移动，或在云盘里尚未下载。",
  "attachment.write_failed": "附件未能保存到工作区的 .reasonix/attachments：{detail}。请检查该目录的写入权限和磁盘空间。",
  "complete.line_too_long": "该行过长，无法补全",
  "stream.unsupported": "该连接不支持流式传输",
  "internal.failed": "服务端出现异常，与你的操作无关",
  "provider.bad_context_window": "上下文长度不能是负数；填 0 表示不自动压缩",
  "provider.bad_token_limit": "Token 上限不能是负数",
  "provider.bad_max_output_tokens": "最大输出 Token 不能是负数",
  "provider.bad_idle_timeout": "无响应超时须在 {min} 到 {max} 秒之间；留空使用默认值",
  "provider.bad_reasoning_protocol": "无法识别「{protocol}」这种思考协议",
  "provider.default_effort_not_listed": "默认档位「{level}」不在填写的档位里",
  "provider.model_default_effort_not_listed": "{model} 的默认档位「{level}」不在为它选的档位里",
  "provider.model_effort_unlisted": "{model} 不在已启用的模型里，不能为它单独设置档位",
  "provider.running": "这个模型上的对话正在运行，停止后再删除",
  "session.running": "该会话正在运行，停止后再删除",
  "workspace.running": "这个文件夹里有正在运行的对话，停止后再移除",
  "remote.running": "这台机器上有正在运行的对话，停止后再移除",
  "workspace.none": "还没有文件夹，会话需要在文件夹里打开。请先添加一个文件夹",
  "provider.no_current_model": "当前没有正在使用的模型，无法记录其窗口大小",
  "context.window_after_this_turn": "窗口大小已记录，将在本轮结束后生效",
  "provider.saved_while_running": "已保存。当前对话还有未结束的工作（正在运行、等待你回答或有后台任务），仍按原设置进行；结束后再保存一次即可生效",
  "runtime.saved_while_running": "已保存。当前对话还有未结束的工作（正在运行、等待你回答或有后台任务），仍按原设置进行；结束后再保存一次即可生效",
  "provider.saved_model_unlisted": "已保存。当前对话使用的模型已不在该来源的列表中，切换模型后才会生效",
  "provider.extra_body_null": "额外设置中的「{path}」不能为空值（null）",
  "provider.no_websearch_wire": "该协议不支持由端点自行搜索",

  // ── 来源：连接与授权 ─────────────────────────────────────────────
  "provider.editing_disabled": "这台服务器不允许修改模型来源",
  "browser.open_failed": "打不开这个网页：{error}",
  "browser.network_path": "内置浏览器不会打开指向网络上另一台机器的路径，只按路径写法拒绝，不会去访问",
  "browser.engine_missing": "没有找到可用的浏览器。请安装 Chrome、Edge 或 Chromium，或在配置里用 [browser] executable 指定路径，新会话才会读到",
  "browser.engine_failed": "内置浏览器没能启动，稍后再试一次",
  "browser.profile_busy": "内置浏览器的资料目录正被另一个浏览器占用。关掉其他 Studio 窗口或用同一资料目录的浏览器后再试",
  "notifications.rejected": "通知设置没能保存：{error}",
  "editor.not_installed": "这台机器上没找到 VS Code、Cursor 这类编辑器。装一个，或在配置里用 [desktop] editor 指定路径。",
  "editor.launch_failed": "编辑器没能启动：{error}",
  "editor.no_window": "这个内核没有窗口，打不开本机的编辑器。",
  "workspace.not_listed": "这个文件夹不在当前窗口的项目列表里。",
  "workspace.folder_missing": "项目文件夹已不在磁盘上。",
  "workspace.locate_no_window": "这个内核不在本机，没法在系统文件管理器中显示它的文件。",
  "device.host_only": "这项操作只能在电脑上的窗口里做，已配对的手机做不了。",
  "device.host_rejected": "这个地址不是本机共享的地址，请重新扫码。",
  "device.origin_rejected": "请求来自其他网页，已拒绝。",
  "device.unauthorized": "这台设备还没配对，或已被移除。请在电脑上重新显示二维码并扫码。",
  "device.pairing_invalid": "配对码已失效：可能已被使用、已过期，或已换了新码。请在电脑上重新显示二维码。",
  "device.not_a_device": "这不是一台已配对的设备。",
  "device.misconfigured": "共享入口配置不完整，无法接受连接。",
  "share.closed": "手机访问已关闭，请先开启。",
  "share.cloud_unavailable": "互联网连接暂时不可用，请确认电脑端 Studio 已登录并保持在线。",
  "share.address_rejected": "地址 {ip} 不是本机的局域网地址。",
  "share.listen_failed": "无法在 {ip} 上开启监听：{error}",
  "share.port_in_use": "端口 {port} 已被其他程序占用，请换一个端口，或清空后让系统自动选择。",
  "share.port_out_of_range": "端口需要在 {min}–{max} 之间，或清空后让系统自动选择。",
  "share.port_save_failed": "无法保存端口设置：{error}",
  "share.remember_save_failed": "无法保存配对记忆设置：{error}",
  "share.device_unknown": "没有这台已配对的设备，可能已被移除。",
  "picker.unsupported": "这个系统没有可用的文件夹选择框，请直接填写路径。",
  "picker.failed": "打不开文件夹选择框：{error}",
  "provider.bad_key_slot": "名称「{name}」不能用来存放密钥：密钥槽位由名称推导，而它不能以数字开头。改一个以字母开头的名称即可，密钥本身没有问题。",
  "page.not_built": "这个内核没有带界面，只提供接口",
  "provider.key_missing": "当前模型还没有 API key，请先在设置里添加",
  "provider.key_required": "请填写 API key",
  "provider.key_too_large": "该 key 长度异常，可能粘贴了错误内容",
  "provider.setup_done": "已连接，无需重复配置",
  "provider.setup_failed": "远端配置未完成，请稍后重试",
  "provider.unknown": "没有这个模型连接",
  "provider.no_key_slot": "该连接没有可存放密钥的变量，请先在配置中为它指定 api_key_env",
  "provider.key_unstorable": "该 key 含有无法安全保存的字符组合，请重新复制",
  "provider.key_invalid": "该 key 超长或含换行，请重新复制",
  "provider.credentials_changed": "已保存的密钥在此期间被改动，请重新打开配置再试",
  "provider.activation_failed": "密钥已保存，但连接尚未生效，请重试",
  "provider.test_auth": "服务商拒绝了该 key，请检查是否复制完整",
  "provider.test_timeout": "服务商没有及时应答，请稍后重试",
  "provider.test_unreachable": "连不上服务商，请检查网络或服务地址",
  "provider.test_upstream": "服务商返回了错误，请稍后重试",
  "provider.test_failed": "连接测试未通过",

  "memory.unavailable": "该会话未启用记忆",

  // ── 来源：连不上时卡在哪一步 ─────────────────────────────────────
  // Each of these is a different next move, which is the whole reason the
  // kernel sends a code: "连接失败" would send everyone to the same dead end.
  "provider.probe.address_missing": "尚未填写服务地址",
  "provider.probe.unauthorized": "该 key 未被接受。请检查是否复制完整，或在服务商控制台重新生成",
  "provider.probe.payment_required": "key 有效，但该账户余额不足，请先在服务商处充值",
  "provider.probe.rate_limited": "服务商提示请求过于频繁，请稍后重试",
  "provider.probe.path_not_found": "地址可以连通，但该路径没有模型清单。多数服务的地址需以 /v1 结尾",
  "provider.probe.no_chat_models": "该服务列出了 {count} 个模型，但均不支持对话 —— 它可能仅提供向量或重排能力",
  "provider.probe.upstream_error": "服务商返回错误（HTTP {status}），与填写内容无关，请稍后重试",
  "provider.probe.timeout": "该地址在限定时间内没有响应。请检查网络或代理，或稍后重试",
  "provider.probe.unreachable": "无法连接该地址。请检查网络是否通畅，以及地址是否有误",
  "provider.probe.failed": "检查失败，没有具体原因",
  "provider.probe.decision_path_not_found": "地址可以连通，但没有 System One 决策接口。请填写服务的基地址，不要带 /v1/systemone，Reasonix 会自己在后面追加",
  "provider.probe.decision_not_compatible": "该地址有响应，但返回的不是 System One 的决策结果。请确认这是决策协议服务的基地址",
  "provider.probe.decision_rejected": "服务拒绝了这次决策请求。请核对模型名与服务文档，具体原因见下方服务返回的内容",
  "provider.probe.not_compatible": "该地址有响应，但不是 OpenAI 或 Anthropic 类接口。请确认是否误将网页地址复制过来",

  // ── 推理强度：这个端点给不了 ─────────────────────────────────────
  "effort.not_configurable":
    "{provider} 没说自己有哪些推理强度档位。要有，得在它的配置块里写 reasoning_protocol 或 supported_efforts",
  "effort.unsupported_level": "{provider} 不提供「{level}」档位，可选值为：{levels}",
  "effort.no_provider": "无法识别当前使用的来源，请先切换一次模型",
  "model_mode.unsupported": "当前模型 {model} 不支持「{mode}」模式",

  "prompt_refine.empty": "输入框是空的，先写点内容再优化",
  "prompt_refine.too_long": "内容超过 {max_bytes} 字节，无法优化",
  "prompt_refine.no_model": "当前会话没有可用的模型，无法优化提示词",
  "prompt_refine.timeout": "优化超时，请重试",
  "prompt_refine.no_answer": "模型没有给出结果，请重试",
  "prompt_refine.failed": "优化失败，请重试",
  "prompt_refine.bad_request": "优化请求格式不正确",

  "commit.no_repository": "这个工作区不是 git 仓库，无法提交",
  "commit.nothing_staged": "暂存区是空的，先用 git add 暂存要提交的文件",
  "commit.staged_changed": "暂存区在你确认之后又变了，请重新起草",
  "commit.secrets_staged": "暂存的内容里有疑似密钥的文件，需要你确认后才能提交",
  "commit.empty_message": "提交说明是空的",
  "commit.message_invalid": "提交说明含有无法记录的字符",
  "commit.message_too_long": "提交说明超过 {max_bytes} 字节",
  "commit.identity_missing": "git 没有配置提交者姓名和邮箱（user.name、user.email）",
  "commit.failed": "git 没能记录这次提交",
  "commit.no_model": "当前会话没有可用的模型，无法起草提交说明",
  "commit.timeout": "起草提交说明超时，请重试",
  "commit.no_answer": "模型没有给出提交说明，请重试",
  "commit.git_failed": "读取暂存区失败",
  "commit.bad_request": "提交请求格式不正确",

  // ── 分支切换：composer 的分支菜单拒得有名字 ─────────────────────
  "branch.workspace_busy": "另一个会话正在写入这个工作区，请稍后重试切换分支",
  "branch.jobs_running": "后台任务仍在运行，请先停止再切换分支",
  "branch.turn_running": "任务运行中，分支要等这轮结束再切",
  "branch.no_repository": "这个工作区不是 git 仓库，没有分支可切换",
  "branch.bad_request": "切换分支的请求格式不正确",
  "branch.bad_name": "这不是有效的分支名",
  "branch.unknown": "没有这个名字的本地分支",
  "branch.local_changes": "有未提交的修改会被这次切换覆盖，请先提交或暂存（git stash）",
  "branch.in_use": "该分支已在另一个 worktree 中检出，请先在那边切走",
  "branch.switch_failed": "git 未能完成这次分支切换",

  // ── 会话 ─────────────────────────────────────────────────────────
  "session.disabled": "这台服务器已关闭会话切换",
  "session.pending_cleanup": "该会话正在清理，请稍后再打开",
  "session.in_use": "该会话正被其他位置占用，请先关闭该处",
  // 占用者在另一个进程时，这个窗口里没有可关闭的对象，pid 是唯一可操作的事实。
  "session.in_use_by": "该会话被另一个进程占用（{host} 上的 pid {pid}），结束它之后再删",
  "session.bind_failed": "接管该会话失败，请重新打开窗口",
  "session.has_open_pane": "该会话仍有打开的面板，请先关闭",
  "session.outside_workspace": "该路径不属于任何已知的工作区",
  "hub.no_runtime_open": "尚未打开任何会话",

  // ── 远程 ─────────────────────────────────────────────────────────
  "remote.unreachable": "{host} 上的内核没有应答，连接可能断了",
  // 连得上、也答了，答的是拒绝 —— 和链路断掉是两件事，下一步也不一样。
  "remote.kernel_refused": "{host} 上的内核没有接受这次请求：{detail}",
  // 答了，但答的是「这条路我不认」—— 那台机器上的内核是上一代，没有面板这套
  // 接口。和「被拒绝」分开写：一个是那次请求的事，一个是那台机器该升级了。
  "remote.kernel_too_old": "{host} 上的 reasonix 版本过旧，无法打开面板 —— 请先将该机器上的 reasonix 升级至与本机同一代",
  "remote.not_available": "该内核不负责连接其他机器",
  "remote.name_required": "请为该机器填写名称",
  "remote.host_required": "请填写要连接的地址",
  "remote.bad_port": "这不是有效的端口号",
  "remote.disabled": "该主机已停用，启用后才能连接",
  "remote.has_open_panes": "该机器仍有 {n} 个打开的面板，请先关闭再移除",
  // 主机密钥变了没有「仍然连接」这条路：能绕过的警告等于没有警告。
  "remote.host_key_changed": "{host} 的主机密钥已变更。可能是该机器重装，也可能存在中间人。记录位于 {file} 第 {line} 行，核实前请勿连接。",
  "remote.host_key_rejected": "未接受其指纹，因此没有建立连接",
  "remote.not_connected": "请先在 {host} 上打开一个工作区，才能查询其上的其他内容",
  // 挑目录走的是文件协议，不用那台机器上有内核 —— 所以路径打错和连不上是两件
  // 事，一件改地址栏就好，一件得去看链路。
  "remote.no_such_folder": "{host} 上不存在 {path} 这个目录",
  "remote.folder_unreadable": "{host} 上的 {path} 当前账号无权访问",
  "remote.unsupported_os": "{host} 上无法运行内核 —— SSH 连接正常，但该机器的系统不受支持。支持 Linux、macOS、Windows。",
  "remote.attach_failed": "连接 {host} 失败：{detail}",
  "remote.auth_failed": "{host} 不接受该凭据。请更换密钥，或在设置中填写正确的环境变量名。",

  // ── 壁纸 ─────────────────────────────────────────────────────────
  "wallpaper.unsupported_type": "不支持该图片格式，请改用 PNG、JPEG、WebP、AVIF 或 GIF",
  "wallpaper.empty": "图片内容为空",
  "wallpaper.too_large": "图片过大，请压缩至 {limit} MB 以内",

  // ── 来不及了 ──────────────────────────────────────
  "steer.already_applied": "该条已发送给模型，无法撤回",

  // ── 能力开关：名字、这台机器的存档、以及服务器自己 ───────────────
  "mcp.unavailable": "该服务器未能启动，开关已恢复原状",
  "mcp.switch_not_undone": "该服务器未能启动，且开关未能恢复——重启后将保持刚才设置的状态",
  "mcp.approval_owed": "这个服务要启动的内容在你启用后变了，请先查看命令并重新启用",
  "activation.unavailable": "开关未能保存：其存储文件无法读取或写入",

  // ── 待送达：条目、队列、这份存档各自会拒 ─────────────────────────
  "inbox.not_found": "该条已不在待送达队列中",
  "job.not_running": "这个后台任务已经不在运行了",
  "inbox.invalid_state": "该条当前状态不允许此操作",
  "inbox.paused": "待送达已暂停，请先恢复派发再进行操作",
  "inbox.capacity_items": "待送达条数已达上限，请先发送部分内容",
  "inbox.capacity_bytes": "待送达总字数已达上限，请先发送部分内容",
  "inbox.item_too_large": "该条过长，单条内容有独立的长度上限",
  "inbox.empty": "该条没有正文",
  "inbox.closed": "该会话的待送达已关闭",
  "inbox.schema_readonly": "该待送达由更高版本写入，当前版本只能读取",
  "inbox.idempotency_conflict": "该提交标识已被使用，且当时的内容不同",

  // ── 配置文件本身坏了，以及每一个写设置的面板被它挡住时 ─────────
  "config.unparsed": "配置文件无法读取，因此本次未保存",
  "changes.path_outside_tree": "{path} 不在当前工作树中，无法查看其改动",
  "changes.diff_failed": "无法读取该文件的改动 —— git 未返回结果",
  "trajectory.unreadable": "无法读取本次运行的轨迹记录，因此无法确定其覆盖范围",
  "config.editing_disabled": "这台服务器未开放配置编辑",
  "config.not_repairable": "该文件需手动修改：{detail}",
  "runtime.rebuild_failed": "设置已写入，但运行时未能按新设置重建：{detail}",
  "permissions.rule_unknown_tool": "规则 {rule} 没有对应的工具（{tool}），匹配不到任何调用，因此未保存；命令要写成 Bash(命令:*)",
  "permissions.rejected": "该权限未能保存：{detail}",
  "sandbox.rejected": "沙箱设置未能保存：{detail}",
  "compaction.rejected": "压缩阈值未能保存：{detail}",
  "compaction.no_soft_limit": "本次请求未包含阈值，未做任何修改",
  "browser_tools.save_failed": "内置浏览器设置未能保存：{detail}",
  "browser_tools.no_enabled": "本次请求未说明开关状态，未做任何修改",
  "display_currency.invalid": "不支持这个币种，只能选自动、CNY 或 USD：{detail}",
  "display_currency.save_failed": "费用显示币种未能保存：{detail}",
  "progress_watch.save_failed": "无进展设置未能保存：{detail}",
  "workspace.untrustable": "主目录或磁盘根目录不能整体信任，请打开具体的项目文件夹",
  "workspace.trust_save_failed": "未能记下对此文件夹的信任决定：{detail}",
  "progress_watch.out_of_range": "该数值超出允许范围，未做任何修改：{detail}",
  "mcp.bad_declaration": "无法解析该服务器声明：{detail}",
  "mcp.install_failed": "未能安装该服务器：{detail}",
  "mcp.remove_failed": "未能移除该服务器：{detail}",
  "hooks.rejected": "该钩子未能保存：{detail}",
  "hooks.dry_run_failed": "该钩子未能运行：{detail}",
  "memory.forget_failed": "未能删除该条记忆：{detail}",
  "network.rejected": "网络设置未能保存：{detail}",
  "shell.rejected": "shell 设置未能保存：{detail}",
  "extension.action_failed": "扩展未执行该动作：{detail}",
  "extension.form_rejected": "扩展未接受本次提交：{detail}",
  "plugin.state_unreadable": "无法读取插件清单：{detail}",
  "plugin.toggle_failed": "未能修改该插件的开关：{detail}",
  "plugin.export_failed": "未能导出该插件：{detail}",
  "install.request_unreadable": "无法解析本次安装请求：{detail}",
  "install.failed": "安装失败：{detail}",
  "install.bad_answer": "无法解析安装器的响应：{detail}",
  "market.bad_slug": "这不是有效的社区包名称",
  "market.not_found": "社区市场中没有这个已审核的包",
  "market.unreachable": "无法连接社区市场：{detail}",
  "market.bad_response": "社区市场返回了无法识别的内容",
  "market.filter_unsupported": "社区市场暂不支持只列出已固定内容的包",
  "market.unpinned": "该包的审核版本没有固定内容，需要信任发布者后才能安装",
  "market.bad_source": "该包的审核版本指向本机或不安全的来源，不从市场安装",
  "market.version_changed": "审核版本已更新，请重新查看后再安装",
  "market.content_changed": "来源内容已与审核版本不同，已拒绝安装",
  "market.not_pinnable": "该来源的内容无法固定，已拒绝安装",
  "market.plan_changed": "来源内容在确认后发生了变化，请重新查看",
  "market.not_theme": "这个标为主题的包还会安装主题以外的内容，已拒绝安装",
  "market.signed_out": "登录账号后才能发布或评价",
  "market.email_unverified": "请先在 id.reasonix.io 验证账号邮箱，再发布或评价",
  "market.not_owner": "这个名称已被其他发布者使用，请换一个名称",
  "market.version_exists": "这个版本号已经发布过，请换一个版本号",
  "market.rate_limited": "请求太频繁，请稍等一分钟再试",
  "market.unpublishable": "这个来源审核通过后也无法从市场安装：插件和主题要指向固定到提交的 GitHub 目录（tree/40 位提交号），技能要用 https 地址，MCP 用 https 地址或 npm 包名",
  "market.rejected": "社区市场拒绝了这次提交：{detail}",
  "market.not_yours": "你的发布里没有这个包",
  "market.not_private": "只有私有的包才能提交审核",
  "market.unpreviewed": "未审核的版本要先预览，再按预览时的内容摘要安装",
  "market.own_package": "不能评价自己发布的包",
  "market.bad_vote": "评价只能是赞、踩或撤回",
  "theme.unreadable": "无法读取该主题：{detail}",
  "theme.not_a_pack": "这不是可安装的主题：{detail}",
  "theme.install_failed": "主题未能写入磁盘：{detail}",
  "theme.folder_failed": "无法打开主题目录：{detail}",
  "surface.too_many_slots": "记录的面板位置已达上限（最多 {limit} 个），请先清除一个",
  "sandbox.no_bubblewrap": "本机未安装 bubblewrap（bwrap），命令将不受限制地运行",
  "sandbox.no_sandbox_exec": "本机的 sandbox-exec 不可用，命令将不受限制地运行",
  "sandbox.unsupported_on_windows": "Windows 上尚无操作系统级沙箱，命令将不受限制地运行",
  "sandbox.unsupported_platform": "该平台尚无可用的沙箱后端，命令将不受限制地运行",
  "sandbox.unavailable": "本机没有操作系统沙箱，「关进沙箱」一档无法保存",
  "remote.install_disabled": "{host} 上没有 reasonix，且本机已设置为不自动安装。请将安装方式改回「自动」，或自行在该机器上安装",
  "remote.npm_unavailable": "{host} 上无法运行 npm —— 通常是该机器未安装 Node.js。请安装 Node.js，或将安装方式改为「上传」",
  "remote.npm_outside_path": "npm 安装完成，但安装位置不在登录 shell 的搜索路径中。请在该机器上调整 npm prefix，或将安装方式改为「上传」",
  "remote.platform_mismatch": "本机的 reasonix 不支持 {host} 的平台，也没有对应的官方包可供下载。请将安装方式改为「npm」",
  "remote.no_install_path": "无法在 {host} 上安装 reasonix —— npm、上传、下载均已尝试。请先自行在该机器上安装，再重新连接",
  "remote.binary_not_runnable": "安装到 {host} 上的 reasonix 无法运行。该目录可能挂载了 noexec，也可能传输中断",
  "remote.serve_did_not_start": "{host} 上的 reasonix 已启动，但始终未报告端口。请查看该机器上 ~/.reasonix/remote 下的日志",
  "remote.serve_provider_mismatch": "{host} 上已有一个正在运行的 reasonix serve，它的模型来源（本机代理或该机器自带的密钥）与本次连接的设置不一致。为避免打断它正在做的事，没有替换它。请把该主机的 provider 改成与它一致，或先在该机器上运行 reasonix remote serve stop 再连接",
  "remote.serve_not_attachable": "{host} 上已有一个正在运行的 reasonix serve 占用着该工作区，但无法接入（令牌文件或地址不可读）。请先在该机器上结束那个进程（进程号记在该机器 .reasonix/remote 目录下的 .pid 文件里），再重新连接",
  "wallet.unauthorized": "该供应商拒绝了当前密钥，无法读取余额",
  "wallet.unreachable": "该供应商的余额接口无响应",
  "wallet.unreadable": "无法解析该供应商余额接口返回的内容",
  "feedback.invalid": "反馈内容不符合要求，请检查后重试",
  "feedback.too_large": "反馈内容过大，请缩短文字或减少截图",
  "feedback.rate_limited": "提交太频繁了，请稍后再试",
  "feedback.disabled": "反馈通道暂时关闭，请稍后再试，或直接到 GitHub 提交问题",
  "feedback.duplicate": "相同内容的反馈刚刚提交过了",
  "feedback.bad_token": "反馈服务没有接受本机的反馈身份，请重试",
  "feedback.offline": "无法连接反馈服务，请检查网络后重试，已填内容会保留",
  "feedback.unavailable": "反馈服务暂时无法处理请求，请稍后再试",
  "feedback.internal": "本机保存反馈记录失败，请重试",
  "feedback.busy": "反馈通道今日已满，请明天再试",
  "feedback.image_metadata": "截图无法清除其中的元数据，请换一张或先用截图工具重新导出",
  "feedback.reply_limit": "这份反馈的回复次数已到上限，或回复太频繁了，请稍后再试",
  "feedback.not_replyable": "这份反馈现在不接收回复",
  "feedback.challenge_required": "反馈服务要求额外验证，请稍后再试",

  // ── 远程连接停下来问的那一句 ───────────────────────────────────
  "ask.not_found": "不存在该待回答的问题",
  "ask.stale_epoch": "该回答对应上一次启动的内核，请重新连接",
  "ask.cancelled": "本次连接已结束，该问题无需回答",
  "ask.already_resolved": "该问题已有其他答案",

  // ── 本机通道：这个请求不是 Studio 自己发的 ───────────────────────
  "tray.rejected": "状态图标设置未能保存：{detail}",
  "update.rejected": "本次启动未能记录为健康状态：{detail}",
  "browser_host.bad_frames": "内置浏览器的消息格式不正确，本次回传被丢弃",

  // ── 版本：这个内核背后有没有一个可更新的 Studio ─────────────────
  "studio.no_install": "这个 Studio 不是安装版（从源码启动），没有可以查看或切换的版本",
  "studio.pin_rejected": "版本固定未能保存：{detail}",
  "studio.notes_bad_version": "这不是一个已发布的版本号",
  "studio.notes_absent": "这个版本没有发布更新内容",
  "studio.notes_unreachable": "暂时取不到更新内容，请检查网络后重试",
  "studio.notes_too_large": "这个版本的更新内容超出了允许的大小，已拒绝读取",
  "update.install_running": "已有一个版本切换正在进行，请等待其完成后重试",
  "update.install_rejected": "本次版本切换未能启动：{detail}",
  "update.restart_busy": "有 {n} 项任务正在运行，重启会中断它们",
  "update.nothing_ready": "没有已下载好的版本可供重启安装，请重新点击安装",

  "loopback.host_rejected": "该请求未发往 Studio 监听的地址，已被拒绝",
  "loopback.origin_rejected": "该页面不属于 Studio，无法对其操作",
  "loopback.unauthorized": "缺少本次启动的凭据，请重新打开 Studio 后重试",
  "loopback.misconfigured": "本机通道未建立，请重新打开 Studio 后重试",
  "serve.host_rejected": "该服务未开启认证，只接受发往本机或其监听地址的请求",
  "auth.launch_token_required": "该服务未开启认证，修改与审批需要本次启动的令牌，请用启动时打印的链接打开",
};

/** Reason is what a refused request answers with. `error` is English fallback
 *  for logs and for codes this build has no wording for — never preferred over
 *  a code we do recognise. */
export interface Reason {
  code?: string;
  error?: string;
  params?: Record<string, string | number>;
}

/** say turns a kernel refusal into a sentence. An unknown code degrades to the
 *  kernel's English rather than to a blank — a message nobody translated is
 *  still better than no message. */
export function say(reason: Reason | null | undefined, fallback = ""): string {
  if (!reason) return fallback;
  const zh = reason.code ? SAID[reason.code] : undefined;
  if (zh) return t(zh, reason.params ?? {});
  return reason.error || fallback;
}

/** reason is what a catch block hands to the UI: a coded refusal becomes this
 *  window's language, anything else prints as itself. One call so no display
 *  site has to know which kind it caught. */
export function reason(e: unknown): string {
  if (e instanceof DeliveryError) return t(UNCONFIRMED[e.fault]);
  if (e instanceof HttpError && e.reason) return say(e.reason, e.message);
  // Nothing came back but a status: printing message here would put a path and
  // a number in front of the user. The status is the only identity there is.
  if (e instanceof HttpError && !e.detailed) return t("请求未能送达内核（HTTP {status}）", { status: e.status });
  return e instanceof Error ? e.message : String(e);
}

const UNCONFIRMED: Record<DeliveryFault, string> = {
  kernel_busy: "内核繁忙，这次操作可能没有被收到，重试前请先确认",
  unreachable: "无法连接内核，这次操作可能没有送达，重试前请先确认",
  ui_stalled: "本界面无响应，这次操作可能没有送达内核，重试前请先确认",
};

/** codes is what the parity check reads. */
export const codes = SAID;
