# Modelling Overhaul — 建模彻底改造（多边形/体素/紧凑描述符）

> 定位：用户要求对"建模"做**彻头彻尾的改造**——不是只针对 Blender，而是对
> **多边形网格（mesh）/模型/体素（voxel）** 这一整套几何数据域的智能体工程。
> 目标：智能体对建模的感知与操作 **精准**（确定性、结构化、可验证），且
> **token 消耗少**。视觉/图像改造**暂缓**（用户明确"先不要用视觉改造"）。

## 0. 为什么建模值得彻头彻尾改造

现状（`internal/blender`）只覆盖"本机 Blender 场景操作"，局限：

| 局限 | 说明 |
|---|---|
| 绑定 Blender | 无 Blender 环境则完全不可用；解析慢（起进程 ~1s+） |
| 只认 .blend | 对 .obj/.stl/.ply/.gltf/.vox 等裸网格格式无原生感知 |
| 感知粒度粗 | 场景摘要只有统计数，无拓扑/质量/流形等几何语义 |
| 无体素 | 体素建模（.vox、MagicaVoxel）完全缺位 |
| token 浪费 | 原始几何数据（顶点/面数组）不可直接喂给 LLM，需先转紧凑描述 |

改造后：**通用多边形建模引擎**——纯 Go 解析任意网格格式 → 紧凑几何描述符
（几十 token 感知全模型）→ 确定性操作（本地算法优先，Blender 作重操作后端）
→ 格式互转（含体素）。

## 1. DeepSeek 视觉优化的"抄抄"映射（先不做视觉，只借思路）

DeepSeek 视觉模型（DeepSeek-VL2 系）核心是**视觉 token 压缩**：把图像
编码成**少量结构化 token**（而非原始像素），并按需分配注意力。映射到建模：

| DeepSeek 视觉思路 | 映射到建模改造 |
|---|---|
| 视觉编码器→紧凑 token（图 576→144 级） | **紧凑网格描述符**：原始几何（万级顶点）→ 结构化 JSON 摘要（~30-60 token 感知全模型） |
| 按需/稀疏注意力（不一次全量） | **按需细化**：先粗描述符，需要时再取局部（包围盒内子网格/特定对象） |
| 多尺度特征 | 多粒度摘要：整体统计 → 每对象 → 局部拓扑（LOD 化感知） |
| 结构先验 | 几何先验：流形性/空洞/退化面/UV 完整性——模型"健康度"指标 |

> **不实现视觉**：以上全部落在几何域（数字解析+统计+拓扑计算），零图像依赖。

## 2. 分层架构

```
┌─ 调用方：智能体（工具层）────────────────────────────┐
│  modeling_analyze / modeling_optimize / modeling_convert / modeling_voxel │
├─ ① 解析层 meshparse（纯 Go，零外部依赖）────────────┤
│  obj  |  stl(ascii+binary)  |  ply  |  vox（体素）   │
│  → Mesh{Vertices, Faces, Normals, UVs, Bounds}       │
├─ ② 分析层 MeshAnalyzer（紧凑描述符）────────────────┤
│  Stats{verts,faces,tris,edges,components}             │
│  Quality{manifold,degenerate,nonplanar,uv_ok}         │
│  Bounds{min,max,size,diameter}                        │
│  ≈ 30-60 token 完整感知                              │
├─ ③ 操作层（确定性原语）─────────────────────────────┤
│  纯 Go：cleanup/merge-verts/triangulate/decimate(边坍缩)│
│  Blender 后端（存在时）：重拓扑/UV/布尔/雕刻          │
├─ ④ 输出层 convert（格式互转，含体素）────────────────┤
│  obj/stl/ply/gltf ↔ 互转；mesh→vox（体素化）；vox→mesh │
└──────────────────────────────────────────────────────┘
```

分层收益：①解析层快（毫秒级）且无环境依赖（token 少=不启动 Blender）；
②描述符是唯一喂给 LLM 的形态（原始几何永不进 prompt）；③操作层
**本地优先**（纯 Go 算法无需外部进程），重操作才用 Blender；④体素
（.vox）成为一等公民。

## 3. 紧凑描述符（token 预算）

```json
{
  "format": "obj", "verts": 5248, "faces": 10496, "tris": 10496,
  "components": 1, "manifold": true, "degenerate": 0,
  "bounds": {"min":[-1,-1,0],"max":[1,1,2],"size":[2,2,2]},
  "uv": {"present": true, "unwrapped": true}, "materials": 2
}
```

≈ **40 token** 感知全模型。诊断时按需取子级：`component(0)`: 顶点/面、
`faces(0..100)`: 样本面（法线/UV 采样）。

## 4. 体素支持

- **解析**：`.vox`（MagicaVoxel 格式）→ `VoxelModel{size, palette, voxels}`
- **体素化**：mesh → vox（AABB 网格采样 + 空腔标记）
- **去体素化**：vox → 网格（表面游走/surface nets 简化版）
- 体素描述符：`{size:[w,h,d], filled, palette_colors, components}` ≈ 20 token

## 5. 安全性（延续既有原则）

- 解析层纯读；操作层写前备份、超时、显式路径、输出上限
- 描述符只读、无副作用；优化操作输出前后对比（before/after 统计）
- 正确性优先：体素化/减面等算法有误差时返回误差报告而非静默
- **token 原则**：宁可 miss 不可错；本地优先避免无谓进程开销

## 6. 实施路线（先文档后实施）

- **A（本轮）**：`internal/modeling/meshparse`（obj/stl/ply/vox 纯 Go 解析）
  + `MeshAnalyzer` 紧凑描述符 + 体素化 —— 全部纯 Go、可单测
- **B**：操作层（cleanup/merge/triangulate/decimate 边坍缩）纯 Go 实现
- **C**：接线为工具（modeling_analyze/optimize/convert/voxel）+ Blender 后端增强
- **D（暂缓）**：视觉改造（用户明确先不做）

## 7. 与既有工作的关系

- `internal/blender`（7435560e）保留为**重操作后端**（重拓扑/UV/布尔）
- `docs/BLENDER_MODELING.md` 降级为"操作层 Blender 后端说明"
- `internal/flywheel` 轨迹/记忆可用于沉淀建模任务模式（可选）
