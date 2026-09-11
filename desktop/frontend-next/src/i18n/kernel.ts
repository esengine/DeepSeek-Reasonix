import { HttpError } from "../port/port";
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
const SAID: Record<string, string> = {
  // ── 忙：不是出错，是「现在不行」 ─────────────────────────────────
  "plan.decision_stale": "该决定已不符合当前状态：计划在你回答前已发生变更",
  "busy.switch_model": "任务正在运行，请先停止再切换模型",
  "busy.change_effort": "任务正在运行，请先停止再调整推理强度",
  "busy.change_workspace": "任务正在运行，请先停止再切换工作区",
  "busy.reload_extensions": "任务正在运行，请先停止再重载扩展",

  // ── 冲突：有东西挡着 ─────────────────────────────────────────────
  "workspace.has_open_panes": "该文件夹仍有 {n} 个打开的面板，请先关闭再移除",
  "provider.model_in_use": "该来源正在使用中，请先切换模型再删除",
  "hub.too_many_panes": "最多同时打开 {max} 个面板，请先关闭一个",

  // ── 来源：填错了什么 ─────────────────────────────────────────────
  "provider.name_required": "请为该来源填写名称",
  "provider.name_invalid": "名称只能包含字母、数字、点、连字符和下划线",
  "provider.endpoint_required": "请填写接口地址",
  "provider.kind_unsupported": "无法识别「{kind}」这种接入方式",
  "provider.no_models_picked": "请至少选择一个模型",
  "provider.default_not_selected": "默认模型「{model}」不在已选择的模型中",

  // ── 来源：这个协议做不到 ─────────────────────────────────────────
  "provider.no_thinking_param": "该协议不发送思考参数，启用后不会生效",
  "request.bad_body": "无法解析本次请求的内容，请刷新页面后重试",
  "request.missing_field": "缺少「{field}」",
  "request.not_found": "找不到名为「{name}」的{kind}",
  "project.unknown": "该项目未在当前窗口中打开",
  "busy.session_in_use": "该会话正被其他位置占用：{detail}",
  "busy.session_running": "该会话正在运行，此消息已排入队列",
  "busy.session_active": "这是当前打开的会话，请先切换后再删除",
  "request.bad_value": "「{field}」只能为以下值之一：{allowed}",
  "session.bad_name": "会话名不能是路径",
  "session.bad_path": "无法解析该会话路径",
  "session.open_failed": "打不开这个会话",
  "session.outside_dir": "该路径位于会话目录之外",
  "request.method_not_allowed": "该地址不接受此种请求方式",
  "request.bad_content_type": "请求体必须是 application/json",
  "permissions.editing_disabled": "这台服务器未开放权限编辑",
  "sandbox.editing_disabled": "这台服务器未开放沙箱编辑",
  "roles.editing_disabled": "这台服务器未开放角色编辑",
  "storage.moving_disabled": "这台服务器未开放存储迁移",
  "storage.move_running": "已有一个迁移正在进行，请等待其完成",
  "plugin.bad_name": "这不是有效的插件名称",
  "plugin.not_installed": "该插件未安装",
  "wallpaper.not_base64": "图片数据不是 base64 编码",
  "shell.unavailable_over_http": "HTTP 上不提供 shell 命令",
  "roles.unknown": "不存在「{role}」这个角色",
  "roles.model_unknown": "没有已配置的模型匹配「{model}」",
  "shell.editing_disabled": "这台服务器未开放 shell 设置",
  "account.signin_disabled": "这台服务器未开放账号登录",
  "workspace.changing_disabled": "这台服务器不支持切换工作区",
  "settings.unknown_preset": "不存在该预设",
  "drop.too_many_paths": "本次拖入 {count} 个，最多允许 {limit} 个",
  "complete.line_too_long": "该行过长，无法补全",
  "stream.unsupported": "该连接不支持流式传输",
  "internal.failed": "服务端出现异常，与你的操作无关",
  "provider.bad_context_window": "上下文长度不能是负数；填 0 表示不自动压缩",
  "provider.bad_reasoning_protocol": "无法识别「{protocol}」这种思考协议",
  "provider.no_current_model": "当前没有正在使用的模型，无法记录其窗口大小",
  "context.window_after_this_turn": "窗口大小已记录，将在本轮结束后生效",
  "provider.extra_body_null": "额外设置中的「{path}」不能为空值（null）",
  "provider.no_websearch_wire": "该协议不支持由端点自行搜索",

  // ── 来源：连接与授权 ─────────────────────────────────────────────
  "provider.editing_disabled": "这台服务器不允许修改模型来源",
  "provider.key_required": "请填写 API key",
  "provider.key_too_large": "该 key 长度异常，可能粘贴了错误内容",
  "provider.setup_done": "已连接，无需重复配置",
  "provider.setup_failed": "远端配置未完成，请稍后重试",

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
  "provider.probe.unreachable": "无法连接该地址。请检查网络是否通畅，以及地址是否有误",
  "provider.probe.not_compatible": "该地址有响应，但不是 OpenAI 或 Anthropic 类接口。请确认是否误将网页地址复制过来",

  // ── 推理强度：这个端点给不了 ─────────────────────────────────────
  "effort.not_configurable":
    "{provider} 没说自己有哪些推理强度档位。要有，得在它的配置块里写 reasoning_protocol 或 supported_efforts",
  "effort.unsupported_level": "{provider} 不提供「{level}」档位，可选值为：{levels}",
  "effort.no_provider": "无法识别当前使用的来源，请先切换一次模型",

  // ── 会话 ─────────────────────────────────────────────────────────
  "session.disabled": "这台服务器已关闭会话切换",
  "session.pending_cleanup": "该会话正在清理，请稍后再打开",
  "session.in_use": "该会话正被其他位置占用，请先关闭该处",
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
  "activation.unavailable": "开关未能保存：其存储文件无法读取或写入",

  // ── 待送达：条目、队列、这份存档各自会拒 ─────────────────────────
  "inbox.not_found": "该条已不在待送达队列中",
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
  "permissions.rejected": "该权限未能保存：{detail}",
  "sandbox.rejected": "沙箱设置未能保存：{detail}",
  "compaction.rejected": "压缩阈值未能保存：{detail}",
  "compaction.no_soft_limit": "本次请求未包含阈值，未做任何修改",
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
  "theme.unreadable": "无法读取该主题：{detail}",
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
  "wallet.unauthorized": "该供应商拒绝了当前密钥，无法读取余额",
  "wallet.unreachable": "该供应商的余额接口无响应",
  "wallet.unreadable": "无法解析该供应商余额接口返回的内容",

  // ── 远程连接停下来问的那一句 ───────────────────────────────────
  "ask.not_found": "不存在该待回答的问题",
  "ask.stale_epoch": "该回答对应上一次启动的内核，请重新连接",
  "ask.cancelled": "本次连接已结束，该问题无需回答",
  "ask.already_resolved": "该问题已有其他答案",

  // ── 本机通道：这个请求不是 Studio 自己发的 ───────────────────────
  "tray.rejected": "状态图标设置未能保存：{detail}",
  "update.rejected": "本次启动未能记录为健康状态：{detail}",

  // ── 版本：这个内核背后有没有一个可更新的 Studio ─────────────────
  "studio.no_install": "这个内核不是由 Studio 启动的，没有可以查看或切换的版本",
  "studio.pin_rejected": "版本固定未能保存：{detail}",
  "update.install_running": "已有一个版本切换正在进行，请等待其完成后重试",
  "update.install_rejected": "本次版本切换未能启动：{detail}",

  "loopback.host_rejected": "该请求未发往 Studio 监听的地址，已被拒绝",
  "loopback.origin_rejected": "该页面不属于 Studio，无法对其操作",
  "loopback.unauthorized": "缺少本次启动的凭据，请重新打开 Studio 后重试",
  "loopback.misconfigured": "本机通道未建立，请重新打开 Studio 后重试",
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
  if (e instanceof HttpError && e.reason) return say(e.reason, e.message);
  // Nothing came back but a status: printing message here would put a path and
  // a number in front of the user. The status is the only identity there is.
  if (e instanceof HttpError && !e.detailed) return t("请求未能送达内核（HTTP {status}）", { status: e.status });
  return e instanceof Error ? e.message : String(e);
}

/** codes is what the parity check reads. */
export const codes = SAID;
