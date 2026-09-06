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
  "plan.decision_stale": "这个决定已经不是当前状态了——计划在你回答前已经变了",
  "busy.switch_model": "任务正在运行，请先停止再切换模型",
  "busy.change_effort": "任务正在运行，请先停止再调整推理强度",
  "busy.change_workspace": "任务正在运行，请先停止再切换工作区",
  "busy.reload_extensions": "任务正在运行，请先停止再重载扩展",

  // ── 冲突：有东西挡着 ─────────────────────────────────────────────
  "workspace.has_open_panes": "这个文件夹还有 {n} 个打开的面板，先关掉再移除",
  "provider.model_in_use": "这个来源正在用，先换一个模型再删",
  "hub.too_many_panes": "最多同时开 {max} 个面板，先关掉一个",

  // ── 来源：填错了什么 ─────────────────────────────────────────────
  "provider.name_required": "给这个来源起个名字",
  "provider.name_invalid": "名字只能用字母、数字、点、横线和下划线",
  "provider.endpoint_required": "填一个接口地址",
  "provider.kind_unsupported": "不认识「{kind}」这种接入方式",
  "provider.no_models_picked": "至少挑一个模型",
  "provider.default_not_selected": "默认模型「{model}」不在挑中的那几个里",

  // ── 来源：这个协议做不到 ─────────────────────────────────────────
  "provider.no_thinking_param": "这个协议不发思考参数，开了也没用",
  "request.bad_body": "这次请求的内容读不出来，刷新页面再试一次",
  "request.missing_field": "还缺一个「{field}」",
  "request.not_found": "找不到叫「{name}」的{kind}",
  "project.unknown": "这个项目没有在当前窗口打开",
  "busy.session_in_use": "这个会话正被别处占用：{detail}",
  "busy.session_running": "这个会话正在跑，这句话排到它后面了",
  "busy.session_active": "这就是当前打开的会话，先切走再删",
  "request.bad_value": "「{field}」只能是这几个之一：{allowed}",
  "session.bad_name": "会话名不能是路径",
  "session.bad_path": "这个会话路径解析不出来",
  "session.outside_dir": "这个路径在会话目录之外",
  "request.method_not_allowed": "这个地址不接受这种请求方式",
  "request.bad_content_type": "请求体必须是 application/json",
  "permissions.editing_disabled": "这台服务器没有开放权限编辑",
  "sandbox.editing_disabled": "这台服务器没有开放沙箱编辑",
  "roles.editing_disabled": "这台服务器没有开放角色编辑",
  "storage.moving_disabled": "这台服务器没有开放搬迁存储",
  "storage.move_running": "已经有一个搬迁在跑了，等它结束",
  "plugin.bad_name": "这不是一个插件名",
  "plugin.not_installed": "这个插件没有安装",
  "wallpaper.not_base64": "图片数据不是 base64",
  "shell.unavailable_over_http": "HTTP 上不提供 shell 命令",
  "roles.unknown": "没有「{role}」这个角色",
  "roles.model_unknown": "没有配置好的模型能匹配「{model}」",
  "shell.editing_disabled": "这台服务器没有开放 shell 设置",
  "account.signin_disabled": "这台服务器没有开放账号登录",
  "workspace.changing_disabled": "这台服务器不能切换工作区",
  "settings.unknown_preset": "没有这个预设",
  "drop.too_many_paths": "一次拖入 {count} 个，最多 {limit} 个",
  "complete.line_too_long": "这一行太长，补全不了",
  "stream.unsupported": "这条连接不支持流式传输",
  "internal.failed": "这边出了点问题，不是你的操作有误",
  "provider.bad_context_window": "上下文长度不能是负数；填 0 表示不自动压缩",
  "provider.bad_reasoning_protocol": "不认识「{protocol}」这种思考协议",
  "provider.no_current_model": "现在没有在用的模型，没法给它记窗口大小",
  "context.window_after_this_turn": "窗口大小已经记下了，这一轮跑完才开始按它算",
  "provider.extra_body_null": "额外设置里「{path}」不能是空值（null）",
  "provider.no_websearch_wire": "这个协议没有让端点自己搜索的写法",

  // ── 来源：连接与授权 ─────────────────────────────────────────────
  "provider.editing_disabled": "这台服务器不让改模型来源",
  "provider.key_required": "要填 API key",
  "provider.key_too_large": "这个 key 太长了，八成粘错了东西",
  "provider.setup_done": "已经连上了，不用再配一次",
  "provider.setup_failed": "远端配置没做成，稍后再试",

  "memory.unavailable": "这个会话没有开启记忆",

  // ── 来源：连不上时卡在哪一步 ─────────────────────────────────────
  // Each of these is a different next move, which is the whole reason the
  // kernel sends a code: "连接失败" would send everyone to the same dead end.
  "provider.probe.address_missing": "还没填服务地址",
  "provider.probe.unauthorized": "这串 key 没被接受。看看是不是没复制全，或者到服务商控制台重新生成一个",
  "provider.probe.payment_required": "key 是对的，但这个账户余额不足了，先去服务商那边充值",
  "provider.probe.rate_limited": "服务商说请求太频繁，等一会儿再试",
  "provider.probe.path_not_found": "地址连得上，但这个路径没有模型清单。多数服务的地址要以 /v1 结尾",
  "provider.probe.no_chat_models": "这个服务列出了 {count} 个模型，但没有一个能对话 —— 它可能只做向量或重排",
  "provider.probe.upstream_error": "服务商那边出错了（HTTP {status}），不是你填错了，等一会儿再试",
  "provider.probe.unreachable": "连不上这个地址。看看网络通不通，或者地址有没有打错",
  "provider.probe.not_compatible": "这个地址答了，但不是 OpenAI 或 Anthropic 那种接口。确认一下别是把网页地址复制过来了",

  // ── 推理强度：这个端点给不了 ─────────────────────────────────────
  "effort.not_configurable":
    "{provider} 没说自己有哪些推理强度档位。要有，得在它的配置块里写 reasoning_protocol 或 supported_efforts",
  "effort.unsupported_level": "{provider} 没有「{level}」这一档，能选的是：{levels}",
  "effort.no_provider": "认不出现在用的是哪个来源，先切一次模型",

  // ── 会话 ─────────────────────────────────────────────────────────
  "session.disabled": "这台服务器关掉了会话切换",
  "session.pending_cleanup": "这个会话正在清理，等一下再打开",
  "session.in_use": "这个会话被别处占着，先把那边关掉",
  "session.bind_failed": "接管这个会话失败了，重开一次窗口",
  "session.has_open_pane": "这个会话还开着面板，先关掉那个",
  "session.outside_workspace": "这个路径不在任何已知的工作区里",
  "hub.no_runtime_open": "还没有打开的会话",

  // ── 远程 ─────────────────────────────────────────────────────────
  "remote.unreachable": "{host} 上的内核没有应答，连接可能断了",
  // 连得上、也答了，答的是拒绝 —— 和链路断掉是两件事，下一步也不一样。
  "remote.kernel_refused": "{host} 上的内核没有接受这次请求：{detail}",
  // 答了，但答的是「这条路我不认」—— 那台机器上的内核是上一代，没有面板这套
  // 接口。和「被拒绝」分开写：一个是那次请求的事，一个是那台机器该升级了。
  "remote.kernel_too_old": "{host} 上的 reasonix 太旧了，开不出面板 —— 先把那台机器上的 reasonix 升到和这边同一代",
  "remote.not_available": "这个内核不负责连别的机器",
  "remote.name_required": "给这台机器起个名字",
  "remote.host_required": "说清楚连哪个地址",
  "remote.bad_port": "这不是一个端口号",
  "remote.has_open_panes": "这台机器还开着 {n} 个面板，先关掉再移除",
  // 主机密钥变了没有「仍然连接」这条路：能绕过的警告等于没有警告。
  "remote.host_key_changed": "{host} 的主机密钥变了。可能是那台机器重装了，也可能有人在中间。记录在 {file} 第 {line} 行，核对清楚之前不要连。",
  "remote.host_key_rejected": "你没有接受它的指纹，所以没有连",
  "remote.not_connected": "先在 {host} 上开一个工作区，才问得到它上面还有什么",
  // 挑目录走的是文件协议，不用那台机器上有内核 —— 所以路径打错和连不上是两件
  // 事，一件改地址栏就好，一件得去看链路。
  "remote.no_such_folder": "{host} 上没有 {path} 这个目录",
  "remote.folder_unreadable": "{host} 上的 {path} 这个账号看不了",
  "remote.unsupported_os": "{host} 上跑不了内核 —— SSH 是通的，是那台机器的系统装不上。支持 Linux、macOS、Windows。",
  "remote.attach_failed": "连 {host} 没成功：{detail}",
  "remote.auth_failed": "{host} 不认这套凭据。换个密钥，或者在设置里填对环境变量名。",

  // ── 壁纸 ─────────────────────────────────────────────────────────
  "wallpaper.unsupported_type": "这种图片格式用不了，换 PNG、JPEG、WebP、AVIF 或 GIF",
  "wallpaper.empty": "图片是空的",
  "wallpaper.too_large": "图片太大了，先压到 {limit} MB 以内",

  // ── 来不及了 ──────────────────────────────────────
  "steer.already_applied": "这条已经送给模型了，收不回来了",

  // ── 能力开关：名字、这台机器的存档、以及服务器自己 ───────────────
  "mcp.unavailable": "这个服务器没能起来，开关已经退回原样",
  "mcp.switch_not_undone": "这个服务器没能起来，而且开关也没能退回去——重启后它会是刚才设的那个状态",
  "activation.unavailable": "开关没能存下来：存放它的文件读不到或写不进去",

  // ── 待送达：条目、队列、这份存档各自会拒 ─────────────────────────
  "inbox.not_found": "这一条已经不在待送达里了",
  "inbox.invalid_state": "这一条现在的状态不允许这个操作",
  "inbox.paused": "待送达已暂停，先继续派发再动它",
  "inbox.capacity_items": "待送达的条数满了，先让它发出去几条",
  "inbox.capacity_bytes": "待送达的总字数满了，先让它发出去几条",
  "inbox.item_too_large": "这一条太长了，单条有自己的上限",
  "inbox.empty": "这一条没有正文",
  "inbox.closed": "这个会话的待送达已经关了",
  "inbox.schema_readonly": "这份待送达是更新版本写的，当前版本只能读",
  "inbox.idempotency_conflict": "这个提交标识用过了，而且当时的内容不一样",

  // ── 配置文件本身坏了，以及每一个写设置的面板被它挡住时 ─────────
  "config.unparsed": "配置文件读不了，所以这次没保存",
  "changes.path_outside_tree": "{path} 不在这个工作树里，看不了它的改动",
  "changes.diff_failed": "读不出这个文件的改动 —— git 那边没给出结果",
  "config.editing_disabled": "这台服务器没有开放配置编辑",
  "config.not_repairable": "这个文件得手动改：{detail}",
  "runtime.rebuild_failed": "设置写进去了，但运行时没能照新设置重建：{detail}",
  "permissions.rejected": "这条权限没能保存：{detail}",
  "sandbox.rejected": "沙箱设置没能保存：{detail}",
  "compaction.rejected": "压缩阈值没能保存：{detail}",
  "compaction.no_soft_limit": "这次请求没带阈值，什么都没改",
  "mcp.bad_declaration": "这段服务器声明读不出来：{detail}",
  "mcp.install_failed": "没能装上这个服务器：{detail}",
  "mcp.remove_failed": "没能移除这个服务器：{detail}",
  "hooks.rejected": "这条钩子没能保存：{detail}",
  "hooks.dry_run_failed": "这条钩子没跑起来：{detail}",
  "memory.forget_failed": "没能删掉这条记忆：{detail}",
  "network.rejected": "网络设置没能保存：{detail}",
  "shell.rejected": "shell 设置没能保存：{detail}",
  "extension.action_failed": "扩展没有执行这个动作：{detail}",
  "extension.form_rejected": "扩展没有接受这次提交：{detail}",
  "plugin.state_unreadable": "插件清单读不出来：{detail}",
  "plugin.toggle_failed": "没能改这个插件的开关：{detail}",
  "plugin.export_failed": "没能导出这个插件：{detail}",
  "install.request_unreadable": "这次安装请求读不出来：{detail}",
  "install.failed": "没能装上：{detail}",
  "install.bad_answer": "安装器的回答读不出来：{detail}",
  "theme.unreadable": "这套主题读不出来：{detail}",
  "surface.too_many_slots": "记下的面板位置太多了（最多 {limit} 个），先清掉一个",
  "sandbox.no_bubblewrap": "这台机器上没有 bubblewrap（bwrap），命令只能不受限地跑",
  "sandbox.no_sandbox_exec": "这台机器上的 sandbox-exec 用不了，命令只能不受限地跑",
  "sandbox.unsupported_on_windows": "Windows 上还没有 OS 级沙箱，命令只能不受限地跑",
  "sandbox.unsupported_platform": "这个平台还没有能用的沙箱后端，命令只能不受限地跑",
  "sandbox.unavailable": "这台机器上没有 OS 沙箱，「关进沙箱」这一档存不上",
  "remote.install_disabled": "{host} 上没有 reasonix，而这台机器设了不自动装。把安装方式改回「自动」，或者自己去那边装一个",
  "remote.npm_unavailable": "{host} 上跑不了 npm —— 多半是那台机器没有 Node.js。装一个，或者把安装方式改成「上传」",
  "remote.npm_outside_path": "npm 装好了，可它装到了登录 shell 找不到的地方。去那边调 npm prefix，或者把安装方式改成「上传」",
  "remote.platform_mismatch": "本机的 reasonix 跑不了 {host} 的平台，也没有对应的官方包可下。把安装方式改成「npm」",
  "remote.no_install_path": "{host} 上装不上 reasonix —— npm、上传、下载都试过了。先自己去那台机器上装一个，再回来连",
  "remote.binary_not_runnable": "装到 {host} 上的 reasonix 跑不起来。那个目录可能挂了 noexec，也可能传到一半断了",
  "remote.serve_did_not_start": "{host} 上的 reasonix 起来了，却一直没报出端口。去那边看一眼 ~/.reasonix/remote 下的日志",
  "wallet.unauthorized": "这个供应商拒绝了当前密钥，余额读不到",
  "wallet.unreachable": "这个供应商的余额接口没有应答",
  "wallet.unreadable": "这个供应商的余额接口回的内容读不懂",

  // ── 远程连接停下来问的那一句 ───────────────────────────────────
  "ask.not_found": "没有这个待回答的问题",
  "ask.stale_epoch": "这个回答是给上一次启动的内核的，重新连一次",
  "ask.cancelled": "这次连接已经结束了，这个问题不用答了",
  "ask.already_resolved": "这个问题已经有另一个答案了",

  // ── 本机通道：这个请求不是 Studio 自己发的 ───────────────────────
  "tray.rejected": "状态图标的设置没能保存：{detail}",
  "update.rejected": "这次启动没能被记为健康：{detail}",

  // ── 版本：这个内核背后有没有一个可更新的 Studio ─────────────────
  "studio.no_install": "这个内核不是由 Studio 启动的，没有可以查看或切换的版本",
  "studio.pin_rejected": "版本固定没能保存：{detail}",
  "update.install_running": "已经有一个版本切换在进行，等它结束再试",
  "update.install_rejected": "这次版本切换没能开始：{detail}",

  "loopback.host_rejected": "这个请求没有发往 Studio 在监听的地址，已经拒掉了",
  "loopback.origin_rejected": "这个页面不是 Studio 自己，不能操作它",
  "loopback.unauthorized": "缺少本次启动的凭据，重开 Studio 再试",
  "loopback.misconfigured": "本机通道没有建立起来，重开 Studio 再试",
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
  if (e instanceof HttpError && !e.detailed) return t("这次请求没能送到内核（HTTP {status}）", { status: e.status });
  return e instanceof Error ? e.message : String(e);
}

/** codes is what the parity check reads. */
export const codes = SAID;
