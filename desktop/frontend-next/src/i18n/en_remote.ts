// English for the remote-machine surfaces: the sidebar's host section and
// the settings page's host book. Split out for the reason en_settings and
// en_kernel were — one screen's worth of wording, read together.

export const EN_REMOTE: Record<string, string> = {
  // ── 远程主机 ─────────────────────────────────────────────────
  "远程": "Remote",
  "已断开": "Disconnected",
  // The bootstrap's steps. Looked up by key rather than written out at a call
  // site, so the scanner cannot see them — they are listed here by hand, and a
  // missing one shows up as Chinese in an English window rather than as a test
  // failure.
  "探测平台": "Detecting the platform",
  "安装 reasonix": "Installing reasonix",
  "等待其他连接": "Waiting for another client",
  "启动 serve": "Starting serve",
  "等待就绪": "Waiting for it to become ready",
  "挂载端口转发": "Attaching the port forward",
  "接入已运行的实例": "Reusing the running instance",

  // ── 远程：挑一个目录 ─────────────────────────────────────────

  // ── 远程：设置页 ─────────────────────────────────────────────
  "接入另一台机器上的工作区：内核在远程运行，界面仍在本机。远程面板与本地面板并排显示，每处都会标明所在的机器。":
    "Connect a workspace on another machine: the kernel runs there, the window stays here. Remote panes sit alongside local ones, so each place states which machine it belongs to.",
  "{n} 台在线": "{n} online",
  "手机通知": "Phone notifications",
  "地址": "Address",
  "用户": "User",
  "默认": "Default",
  "设为默认": "Make default",
  "已连接": "Connected",
  "重连中": "Reconnecting",
  "部分转发未建立": "A forward did not attach",

  // ── 连接时的两个问题 ─────────────────────────────────────────
  "继续": "Continue",
  "{host} 上没有 reasonix，且本机已设置为不自动安装。请将安装方式改回「自动」，或自行在该机器上安装": "There is no reasonix on {host}, and this machine is set never to install one. Switch installing back to automatic, or install one over there yourself",
  "{host} 上无法运行 npm —— 通常是该机器未安装 Node.js。请安装 Node.js，或将安装方式改为「上传」": "npm will not run on {host} — most likely it has no Node.js. Install one, or switch installing to upload",
  "npm 安装完成，但安装位置不在登录 shell 的搜索路径中。请在该机器上调整 npm prefix，或将安装方式改为「上传」": "npm installed it somewhere the login shell does not look. Fix the npm prefix over there, or switch installing to upload",
  "本机的 reasonix 不支持 {host} 的平台，也没有对应的官方包可供下载。请将安装方式改为「npm」": "This machine's reasonix cannot run on {host}'s platform, and no official build for it was available. Switch installing to npm",
  "无法在 {host} 上安装 reasonix —— npm、上传、下载均已尝试。请先自行在该机器上安装，再重新连接": "Nothing could install reasonix on {host} — npm, upload and download were all tried. Install one there by hand, then connect again",
  "安装到 {host} 上的 reasonix 无法运行。该目录可能挂载了 noexec，也可能传输中断": "The reasonix installed on {host} will not run. That directory may be mounted noexec, or the transfer was cut short",
  "{host} 上的 reasonix 已启动，但始终未报告端口。请查看该机器上 ~/.reasonix/remote 下的日志": "reasonix started on {host} but never reported a port. Look at the logs under ~/.reasonix/remote over there",
  "{host} 上已有一个正在运行的 reasonix serve，它的模型来源（本机代理或该机器自带的密钥）与本次连接的设置不一致。为避免打断它正在做的事，没有替换它。请把该主机的 provider 改成与它一致，或先在该机器上运行 reasonix remote serve stop 再连接": "A reasonix serve is already running on {host}, and it resolves models differently from this connection (this machine's broker versus the host's own keys). It was left alone so its work is not interrupted. Set this host's provider to match it, or run reasonix remote serve stop on that machine first and connect again",
  "{host} 上已有一个正在运行的 reasonix serve 占用着该工作区，但无法接入（令牌文件或地址不可读）。请先在该机器上结束那个进程（进程号记在该机器 .reasonix/remote 目录下的 .pid 文件里），再重新连接": "A reasonix serve is already running for this workspace on {host}, but it cannot be attached to (its token file or address is unreadable). End that process over there (its id is in the .pid file in that machine's .reasonix/remote directory), then connect again",
  "自动选择": "Pick one automatically",
  "机器": "Machine",
  "上传": "upload",
  "下载": "download",

  // ── 手机访问 ─────────────────────────────────────────────────
  "手机访问": "Phone access",
  "远程连接已断开": "Remote connection ended",
  "刷新页面": "Refresh page",
  "这个页面不是最新版": "This page is not the latest version",
  "它是之前打开的旧版本，继续用可能出现莫名的报错。刷新一下就能换成最新版。": "It is an older build that was opened earlier, and may show confusing errors. Refreshing switches to the latest.",
  "这台电脑已停止网页控制。重新连接需要再次验证设备状态。": "This computer has stopped web control. Reconnecting will verify the device again.",
  "重新连接": "Reconnect",
  "打开 reasonix.io": "Open reasonix.io",
  "{action}（{n}）": "{action} ({n})",
  "需要重新登录": "Sign in again",
  "为了安全，远程控制在登录满 24 小时或退出登录后需要重新登录。": "For security, remote control asks you to sign in again 24 hours after signing in, or after you sign out.",
  "重新登录": "Sign in",
  "换一个": "New code",
  "这台手机的提醒": "This phone's reminders",
};
