# Blender Modeling — 智能体 3D 建模优化（精准 + token 少）

> 用户澄清："建模"一直指 **Blender 或其他 3D 建模软件/格式**（.blend/.fbx/.obj/.gltf 等），
> 不是 agent 建模。目标：**智能体对建模软件与建模格式的精准操作**，在精准的前提下
> token 消耗少。
>
> 本机环境：Blender 4.2.16 LTS（`C:\Program Files\Blender Foundation\Blender 4.2\blender.exe`），
> headless `-b --python-expr` 已验证可执行 bpy（场景摘要/网格统计正常）。

## 1. 为什么 token 少：确定性操作 vs 自然语言描述

| 方式 | token 消耗 | 精准度 |
|---|---|---|
| 自然语言描述（"把场景里所有物体合并然后减面到一半"） | 高（每轮重复描述+解析歧义） | 低（模型理解偏差） |
| **确定性 bpy 原语**（`merge_by_name + decimate_ratio 0.5`） | **低**（短命令，无歧义） | **高**（结果确定） |

核心：智能体发出**结构化原语调用**（一个工具 = 一个明确操作），不用长描述；环境感知用
**场景摘要**（结构化 JSON，只含关键统计），不用读全量 .blend 文件。

## 2. 能力集（internal/blender 包）

### 2.1 场景摘要（环境感知，token 少）
`blender_summary(path)` → JSON：
```json
{"objects":3, "meshes":1, "materials":1, "vertices":1234, "tris":2400,
 "object_names":["Cube","Light","Camera"], "format":"blend"}
```
- 智能体只需 1 次调用 + 紧凑 JSON 即知场景全貌，不必读二进制 .blend。

### 2.2 确定性操作原语（精准）
| 原语 | bpy 实现 | 用途 |
|---|---|---|
| `merge_by_name` | 按名称后缀合并对象 | 场景整理 |
| `decimate`（ratio） | `modifier decimate 0.5` | 减面（LOD/移动端优化） |
| `cleanup` | 删空对象/孤立数据/重复材质 | 文件瘦身 |
| `rename_objects` | 批量重命名（前缀/替换） | 命名规范 |
| `set_origin` | `object.origin_set` | 轴心校正 |
| `apply_transform` | `object.transform_apply` | 冻结变换 |
| `triangulate` | modifier triangulate | 游戏引擎就绪 |

每个原语 = 一次 bpy 脚本执行（headless），返回操作统计（改了几个对象/减了多少面）。

### 2.3 格式转换
`blender_convert(path, out, format)`：.blend → .gltf/.glb/.fbx/.obj/.stl
（bpy export_scene；精度/缩放选项固定为游戏就绪默认）。

## 3. Token 预算（估算）

| 操作 | 传统（描述+人工/模型解析） | 本方案（结构化调用） |
|---|---|---|
| 场景感知 | 读全量 .blend 或截图（~1-2k+ token） | 摘要 JSON ~200 token |
| 减面操作 | 多轮描述+确认（~500+） | 一次调用 `decimate 0.5`（~50） |
| 格式转换 | 描述+检查（~300+） | 一次调用（~50） |

**单次任务 token 降幅约 60-80%**（估算，实测后更新）。

## 4. 正确性/安全（交付原则）

- 操作前自动备份（`<file>.blend.bak`），失败回滚。
- 脚本执行超时（默认 60s）+ 输出上限（2KB）。
- 只对指定路径操作（不扫描全盘）。
- 摘要/转换是纯读取/导出，不修改源文件（转换输出新文件）。

## 5. 实施路线

1. **internal/blender 包**：Summary + RunScript（原语库）+ Convert；blender 存在才跑真实测试（t.Skip 兜底）。
2. 接线为工具（blender_summary/blender_convert/blender_optimize 三工具）。
3. 验证：真实 blender 端到端（建测试场景→摘要→减面→断言三角数下降）。

## 6. 与现有资产的关系

- 工具集 MCP（E:\共享\51\10）无建模工具——本包是第一个建模能力。
- godot-mcp 已接入游戏引擎——Blender 建模 + Godot 导入 = 完整 3D 管线（建模→格式→引擎）。
