# 会话索引与桌面启动

Reasonix 始终以会话 transcript、event log、metadata sidecar 和
`desktop-projects.json` 作为唯一权威数据。桌面项目树读取位于
`<缓存根目录>/session-catalog/v6.sqlite` 的一次性 SQLite 查询投影；删除该数据库
不会删除或修改任何会话。早期的 `v1.sqlite` 至 `v5.sqlite` 缓存会保留，避免与仍在
运行或降级后的旧版本进程交叉写同一投影。v6 引入文件系统感知的路径身份，首次启动
会从权威文件重新建立；旧 v5 文件保留用于回滚，不会被新版本写入。同一 v6 索引的
手动重建也会留下带时间戳的 `.replaced-*` 旧文件。

## 不变量

- 启动和项目树请求不会解码 transcript JSONL、执行旧版迁移或等待目录扫描。
- transcript 成功落盘后才更新 catalog。保存观察器只做词法队列入队，不执行文件系统
  探测；后台 worker 解析文件系统身份，SQLite 唯一约束作为最终去重边界，目录对账
  会修复队列拥塞时丢弃的更新。
- session 和 workspace root 的原始路径拼写继续用于文件访问和展示；独立的 identity
  key 会解析别名，并且只在所属文件系统目录不区分大小写时折叠大小写。大小写敏感
  卷上的不同文件和项目不会合并。
- 缺少旧版计数时使用 `unknown` 状态。会话会立即可见，随后由单个 repair worker
  在后台解码修复。
- 文件首次缺失时只标记为 degraded；只有连续第二次扫描仍缺失且超过宽限期后，
  才会从查询投影移除。
- 运行时状态（`open`、`running` 和实时状态）只来自内存 controller，并覆盖
  catalog 结果；这些状态永远不会持久化到 SQLite。
- catalog、迁移、插件和 MCP 工作都可取消，且不参与桌面退出锁。退出最多等待
  catalog 待写入数据 250 ms。
- ready 状态只有在磁盘会话路径、scope/workspace、topic 投影和恢复派生字段均与
  当前权威文件一致时才可复用；数量相等但路径错位也会触发重建。

## 存储与迁移

`internal/sessioncatalog` 使用 `schema_migrations` 版本台账；数据库文件存在不代表
迁移完成。本地缓存使用 WAL、`synchronous=NORMAL` 和较短的 busy timeout。
缓存目录不可用或明确位于远程文件系统时，会降级为内存 catalog，避免存储故障
阻塞应用。

打开数据库时，Reasonix 会执行完整性检查。损坏或无法迁移的数据库会被重命名，
附加 `.corrupt-<时间戳>` 后缀并由新数据库替代；随后在后台根据 sidecar 和
transcript 重建。隔离和重建都不会删除权威文件。

catalog 只保存查询投影：

- 目录签名、扫描代次、检查点和错误；
- 项目排序、标题、颜色、置顶状态及 workspace root identity key；
- topic 排序、聚合计数、活动时间、恢复、健康状态及 workspace root identity key；
- session 访问路径及路径、目录和 workspace root identity key、preview、计数、指纹、
  恢复和健康状态。

topic 分页使用 `(pinned, last_activity_at, topic_id)` keyset cursor。默认每页
50 条，最多 200 条。目录对账每批最多提交 64 个 sidecar，并在让出调度前持久化
检查点。

## 桌面 API

- `GetProjectTreeSnapshot` 返回项目壳、catalog 状态、进度和 revision，不打开
  session 或 sidecar 文件。
- `ListProjectTopics` 提供基于 cursor 的分页搜索和时间过滤。
- `GetTopicSummary` 为 active-turn UI 查询单个 topic，无需重建整棵树。
- `GetSessionCatalogStatus` 和 `RebuildSessionCatalog` 提供安全诊断和索引替换。
  状态同时报告最近修复原因、源文件数量和未完成的目录目标，项目树提供手动重建入口。
- `project-tree:changed-v2` 携带单调递增的 revision、受影响 workspace root 和
  原因。客户端会忽略旧 revision，并只刷新受影响且已展开的项目。

`ListProjectTree` 作为基于 catalog 的兼容包装继续保留，不再回退到同步文件系统
扫描。

## 运维

只读检查 catalog，不创建或修改它：

```sh
reasonix sessions diagnose
reasonix sessions diagnose --json
```

只替换一次性查询投影，并索引所有已保存的桌面项目：

```sh
reasonix sessions reindex
reasonix sessions reindex --json
```

可重复传入 `--dir PATH`，从指定目录集合重建；显式目录按 global scope 处理。
reindex 不会编辑或删除 transcript、event、metadata、recovery、archive 或项目文件；旧
索引文件会保留，便于回滚。

recovery-only 会话在普通树中显示为一个“可恢复”逻辑行；被覆盖的物理副本仍只在
恢复历史中展示，避免用户把未显示的副本误认为内容丢失。

## 插件隔离

manifest 校验和插件握手与 catalog、项目树互相独立。不兼容插件会报告为
`disabled_incompatible`，核心 controller 仍可使用。Reasonix 管理目录中的旧版
manifest 会在生成备份后原子升级；开发目录、外部绝对路径和软链接源码不会被自动
改写，而会给出手动迁移提示。

## 发布门禁

Preview/canary 晋级应观测 catalog 修复积压、重建失败、分页延迟、队列压力和退出
耗时。必须覆盖旧版与损坏 fixture、确定性的生命周期竞态、`go test -race`、React
契约测试，以及受支持 macOS、Windows 和 Linux 架构的 `CGO_ENABLED=0` 构建。
