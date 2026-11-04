// English for the storage panel: what is on disk, where it lives, and moving
// it. Split out of en.ts because that file is one screen's worth of catalogue
// per section and had grown past its ceiling — the grouping is the same.
export const EN_STORAGE: Record<string, string> = {
  体积超过读取上限: "Larger than the size limit for reading",
  "不是本版本能读取的会话格式": "Not a session format this version can read",
  "没有记录它属于哪个项目，所以没有导入": "It records no project it belongs to, so it was not imported",
  "来自本版本尚不支持的存储版本": "Written by a storage version this version does not support yet",
  没有读取权限: "No permission to read it",
  文件已损坏: "The file is damaged",
  复制到新位置时失败: "Copying it to the new location failed",
  存储: "Storage",
  占用: "Space used",
  位置: "Locations",
  迁移: "Migrate",
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
