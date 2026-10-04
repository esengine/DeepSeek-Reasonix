// English for the storage panel: what is on disk, where it lives, and moving
// it. Split out of en.ts because that file is one screen's worth of catalogue
// per section and had grown past its ceiling — the grouping is the same.
export const EN_STORAGE: Record<string, string> = {
  "找回旧版会话": "Recover older sessions",
  "从旧版 Reasonix 导入": "Import from an older Reasonix installation",
  "选择旧版的数据文件夹。扫描只会复制会话，不会移动或删除原文件；找不到原工作区的会话会归入当前工作区。":
    "Choose the older data folder. The scan only copies sessions and never moves or deletes the originals; sessions whose workspace no longer exists are placed in the current workspace.",
  "选择旧版数据目录…": "Choose older data folder…",
  "正在扫描…": "Scanning…",
  "已找回 {n} 个会话。": "Recovered {n} sessions.",
  "没有发现尚未导入的旧会话。": "No sessions remain to be imported.",
  "有 {n} 项无法读取。": "{n} items could not be read.",
  "未能扫描这个文件夹。请确认它是旧版 Reasonix 的数据目录。": "This folder could not be scanned. Make sure it is an older Reasonix data folder.",
  存储: "Storage",
  "数据的存储位置与占用空间。会话和索引会持续增长，配置和凭据不会，因此只有前者可以迁移。迁移在重启后生效。":
    "Where the data is written and how much space it uses. Sessions and indexes keep growing while configuration and credentials do not, so only the former can be moved. A move takes effect after a restart.",
  "无法读取存储占用。": "Cannot read what is on disk.",
  "正在统计…": "Measuring…",
  "统计超时": "Measuring timed out",
  "目录过大，已统计到时间上限，实际占用不小于此数": "This folder is too large to finish measuring in time; the real size is at least this much.",
  占用: "Space used",
  原位置仍有残留数据: "Data remains in the previous location",
  "以下内容仍位于 {dir}：{names}。迁移存储位置时未一并迁移，因此项目列表、全局指令与记忆、计划任务、壁纸、主题包或更新回滚备份可能显示为缺失。下次启动会自动并入当前位置；若仍显示，请退出后把这些文件或目录复制到当前位置，已有的文件不要覆盖。":
    "These are still in {dir}: {names}. A move left them behind, so the project list, global instructions and memory, scheduled runs, a wallpaper, a theme pack or the backups an update rolls back to can look gone on this machine. The next launch merges them into the current location; if they are still listed, quit and copy them there without overwriting files that already exist.",
  位置: "Locations",
  迁移: "Migrate",
  "迁移未能启动。": "The move could not be started.",
  "{drive} 剩余 {free} / {total}": "{drive} — {free} free of {total}",
  "由 {env} 指定": "set by {env}",
  不可移动: "cannot be moved",
  "移动…": "Move…",
  开始迁移: "Start the migration",
  指向这里: "Point it here",
  "目标文件夹的完整路径：空文件夹，或已存有该数据的文件夹":
    "Full path to the destination folder — an empty one, or the one that already holds this data",
  "将迁移 {size}（{n} 个文件），目标磁盘剩余 {free}。完成后需重启生效。":
    "Moves {size} in {n} files; the destination has {free} free. Takes effect after a restart.",
  "该文件夹中已存有此数据（{size}，{n} 个文件）。将直接指向该位置，不复制也不删除。重启后生效。":
    "This folder already holds that data ({size} in {n} files). It will simply be pointed at — nothing is copied and nothing is deleted. Takes effect after a restart.",
  "当前位置仍有 {size}，不会一并迁移。":
    "The current location still holds {size}, which is not carried across.",
  "迁移完成。重启后生效。": "Moved. It takes effect after a restart.",
  正在指向新位置: "Pointing at the new location",
  正在复制: "Copying",
  正在校验: "Verifying",
  "已提交，正在清理原位置": "Committed; clearing the old location",
  会话与归档: "Sessions and archives",
  "转录、压缩归档、用量统计、回溯快照":
    "Transcripts, compaction archives, usage stats, rewind snapshots",
  索引与缓存: "Indexes and caches",
  "搜索索引与派生数据，删除后会自动重建":
    "Search indexes and derived data; deleting them rebuilds them",
  隔离工作区: "Isolated worktrees",
  交付模式检出的独立副本: "The separate checkouts Delivery works in",
  配置与凭据: "Configuration and credentials",
  "设置与 API key，始终随用户配置文件保存":
    "Settings and API keys; these always stay with your user profile",
  进程锁: "Process locks",
  "用于多实例互斥，必须保留在本机固定位置。每个均为空文件；删除会破坏互斥，因此只保留不清理":
    "How instances exclude each other; these must stay in one place on this machine. Each is an empty file and removing one would break the exclusion, so they are kept rather than cleaned",
};
