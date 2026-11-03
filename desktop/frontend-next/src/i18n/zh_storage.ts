export const ZH_STORAGE: Record<string, string> = {
  "storage.recoverOlderSessions": "找回旧版会话",
  "storage.importFromAnOlderReasonix": "从旧版 Reasonix 导入",
  "storage.chooseTheOlderDataFolder":
    "选择旧版的数据文件夹。扫描只会复制会话，不会移动或删除原文件；找不到原工作区的会话会归入当前工作区。",
  "storage.chooseOlderDataFolder": "选择旧版数据目录…",
  "storage.theFolderPanelCouldNot":
    "无法打开文件夹选择器。请输入旧版数据文件夹的路径（里面有 sessions、projects 或 desktop-sessions-v5）。",
  "storage.olderDataFolderPath": "旧版数据目录路径",
  "storage.importThisFolder": "导入此文件夹",
  "storage.scanning": "正在扫描…",
  "storage.recoveredSessions": "已找回 {n} 个会话。",
  "storage.nothingNewToImport": "没有发现尚未导入的旧会话。",
  "storage.noOlderSessionsWereFound":
    "这个文件夹里没有找到旧版会话。请选择 Reasonix 的数据文件夹（里面有 sessions、projects 或 desktop-sessions-v5）。",
  "storage.itemsCouldNotBeRead": "有 {n} 项无法读取。",
  "legacySkips.copyPath": "复制路径：{name}",
  "legacySkips.sessionsWereNotImported": "有 {n} 个会话没有导入",
  "legacySkips.theseFilesAreStillWhere":
    "这些文件仍在原位置，没有被移动或删除。可复制路径自行处理，或修复后重新导入。",
  "legacySkips.largerThanTheSizeLimit": "体积超过读取上限",
  "legacySkips.notASessionFormatThis": "不是本版本能读取的会话格式",
  "legacySkips.itRecordsNoProjectIt": "没有记录它属于哪个项目，所以没有导入",
  "legacySkips.writtenByAStorageVersion": "来自本版本尚不支持的存储版本",
  "legacySkips.noPermissionToReadIt": "没有读取权限",
  "legacySkips.theFileIsDamaged": "文件已损坏",
  "legacySkips.copyingItToTheNew": "复制到新位置时失败",
  "storage.thisFolderCouldNotBe": "未能扫描这个文件夹。请确认它是旧版 Reasonix 的数据目录。",
  "common.storage": "存储",
  "settings.whereTheDataIsWritten":
    "数据的存储位置与占用空间。会话和索引会持续增长，配置和凭据不会，因此只有前者可以迁移。迁移在重启后生效。",
  "storage.cannotReadWhatIsOn": "无法读取存储占用。",
  "storage.measuring": "正在统计…",
  "storage.measuringTimedOut": "统计超时",
  "storage.thisFolderIsTooLarge": "目录过大，已统计到时间上限，实际占用不小于此数",
  "common.spaceUsed": "占用",
  "storage.dataRemainsInThePrevious": "原位置仍有残留数据",
  "storage.theseAreStillInA":
    "以下内容仍位于 {dir}：{names}。迁移存储位置时未一并迁移，因此项目列表、全局指令与记忆、计划任务、壁纸、主题包或更新回滚备份可能显示为缺失。下次启动会自动并入当前位置；若仍显示，请退出后把这些文件或目录复制到当前位置，已有的文件不要覆盖。",
  "common.locations": "位置",
  "common.migrate": "迁移",
  "storage.theMoveCouldNotBe": "迁移未能启动。",
  "storage.freeOf": "{drive} 剩余 {free} / {total}",
  "storage.setBy": "由 {env} 指定",
  "storage.cannotBeMoved": "不可移动",
  "storage.move": "移动…",
  "storage.startTheMigration": "开始迁移",
  "storage.pointItHere": "指向这里",
  "storage.fullPathToTheDestination":
    "目标文件夹的完整路径：空文件夹，或已存有该数据的文件夹",
  "storage.movesInFilesTheDestination":
    "将迁移 {size}（{n} 个文件），目标磁盘剩余 {free}。完成后需重启生效。",
  "storage.thisFolderAlreadyHoldsThat":
    "该文件夹中已存有此数据（{size}，{n} 个文件）。将直接指向该位置，不复制也不删除。重启后生效。",
  "storage.theCurrentLocationStillHolds":
    "当前位置仍有 {size}，不会一并迁移。",
  "storage.movedItTakesEffectAfter": "迁移完成。重启后生效。",
  "storage.pointingAtTheNewLocation": "正在指向新位置",
  "storage.copying": "正在复制",
  "storage.verifying": "正在校验",
  "storage.committedClearingTheOldLocation": "已提交，正在清理原位置",
  "storage.sessionsAndArchives": "会话与归档",
  "storage.transcriptsCompactionArchivesUsageSt":
    "转录、压缩归档、用量统计、回溯快照",
  "storage.indexesAndCaches": "索引与缓存",
  "storage.searchIndexesAndDerivedData":
    "搜索索引与派生数据，删除后会自动重建",
  "storage.isolatedWorktrees": "隔离工作区",
  "storage.theSeparateCheckoutsDeliveryWorks": "交付模式检出的独立副本",
  "storage.configurationAndCredentials": "配置与凭据",
  "storage.settingsAndAPIKeysThese":
    "设置与 API key，始终随用户配置文件保存",
  "storage.processLocks": "进程锁",
  "storage.howInstancesExcludeEachOther":
    "用于多实例互斥，必须保留在本机固定位置。每个均为空文件；删除会破坏互斥，因此只保留不清理",
};
