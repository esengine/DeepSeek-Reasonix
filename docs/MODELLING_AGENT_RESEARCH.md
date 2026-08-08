# 智能体对 3D 建模的加强——技术与论文调研（2024-2026）

> 调研来源：arXiv API 官方条目（标题/摘要/代码链接逐一核对），标注 ⭐ = 有开源实现可"抄"。
> 目标：Reasonix 建模工具链（meshparse 解析/分析/体素化/操作 + Blender bpy 后端 + 4 工具）
> 的精准度提升与 token 消耗优化。

## ① LLM/Agent 3D 建模生成/编辑

| 项目 | 链接 | 核心思路 | 借鉴点 |
|---|---|---|---|
| **Trellis** ⭐ | arXiv:2412.01506 · github.com/Microsoft/TRELLIS | SLAT 结构化潜表示（稀疏 3D 网格+多视图特征），一 latent 解码 mesh/3DGS/NeRF | meshparse 多格式解析+描述符=中立中间表示，对齐 SLAT 做 agent↔生成模型接口层 |
| **AssetGen** | arXiv:2605.26137 | 30s 单图→带法线/贴图/**受控多边形预算**的可部署网格 | optimize 工具学"polygon budget 受控管线化"，包装成单一 agent 工具 |
| **3D-PLOT-LLM** | arXiv:2606.19828 | 点云 patch 重组为 K 局部区域+`<part_k>` marker，LLM 按部件寻址 | MeshAnalyzer 加 **part-level 描述符 token**（"改桌腿"而非"改顶点 100-200"） |

## ② 3D 网格/点云/体素 token 化（直接对标 token 目标）

| 项目 | 链接 | 核心 | 借鉴点 |
|---|---|---|---|
| **SuperVoxelGPT** ⭐ | arXiv:2605.29655 | 显著性引导**自适应 supervoxel 分区**（复杂细/平坦粗），token 长度仅均匀体素化 **12.8%**、10× 加速 | 体素化加自适应分辨率，产出体素 token 直喂 LLM——token 收益最直接 |
| **MeshWeaver** | arXiv:2606.04688 | 多级稀疏体素编码器（顶点特征+交叉注意力+结构脚手架），token 压缩 18%，16K 面 | 与 voxel 工具+体素化天然同构，可复刻"体素引导"范式 |
| **FACE** | arXiv:2603.01515 | 一 face 一 token 自回归自编码器，序列减 9 倍 | 描述符以"面/部件"为语义单元比逐顶点省一个数量级 |
| **Geo3DPruner** | arXiv:2604.18260 | 几何引导 3D token 剪枝，剪 90% 保 90% | agent 多视角场景上下文用体素空间去重，砍上下文成本 |

## ③ 智能体操作 Blender 的框架

| 项目 | 链接 | 核心 | 借鉴点 |
|---|---|---|---|
| **SceneCraft** | arXiv:2403.01248 | 文本→场景图→数值约束→Blender 脚本→GPT-V 渲染迭代；**库学习** | 现有 4 工具+bpy 操作层=现成"库"，显式设计成 agent 可检索函数库 |
| **L3GO** | arXiv:2402.09052 | Chain-of-3D-Thoughts；SimpleBlenv 把 Blender 包装成**原子 API 组合环境** | bpy 层定义原子操作 API（加立方体/挤出/布尔…），禁止裸 bpy——精准+省 token |
| **LL3M + BlenderRAG** | arXiv:2508.08228 | 多 agent 协作写 Blender 脚本；**Blender API 文档 RAG** 降幻觉 | 给 agent 配 bpy API RAG = 低成本高收益 |
| **MeshCoder** | arXiv:2508.14879 | 点云→可编辑 Blender 脚本（对象-代码配对数据集微调） | meshparse+MeshAnalyzer 产出"网格→代码"或 shape-to-code |
| **PairCoder++** | arXiv:2607.01883 | Driver/Navigator 双 agent + 工具链诊断当验证证据（可执行性 0.20→0.78） | **meshparse 纯 Go 解析/体素化/MeshAnalyzer 当几何校验 oracle**——相对纯 bpy 方案的差异化优势，做成显式 verify 工具 |
| **BlenderLLM** ⭐ | arXiv:2412.14203 · github.com/FreedomIntelligence/BlenderLLM | CAD 脚本生成+自改进微调，开源 BlendNet+CadBench 评测 | CADBench 式"脚本可执行性"评测可抄做自家 agent 回归基准 |

## ④ 格式转换/模型优化的智能方法

| 项目 | 链接 | 核心 | 借鉴点 |
|---|---|---|---|
| **MeshAnything** ⭐ | arXiv:2406.10163 · github.com/buaacyw/MeshAnything | 网格提取重定义为生成：VQ-VAE 网格词表+形状条件 Transformer，输出艺术家级拓扑、**面数少几百倍** | optimize 工具把生成式简化作为 QEM 减面的替代后端 |
| **Meshy T2** ⭐ | arXiv:2607.28675 · github.com/meshy-dev/meshy-t2 | vertex-set mesh VAE 每顶点一个连续 latent，单遍解码+**顶点预算控制**，6s 出图 | convert/optimize 的顶点预算语义对齐 |

## 优先级建议（对 Reasonix 落地）

1. **token 效率（最快收益）**：SuperVoxelGPT 式自适应体素 token（降 87%）+ MeshAnalyzer 部件级描述符（3D-PLOT-LLM）——纯 Go 可做，无外部依赖
2. **精准操作**：L3GO 原子 API（bpy 层禁裸脚本）+ SceneCraft 库学习 + PairCoder++ 双 agent（meshparse 做校验 oracle，Reasonix 差异化）
3. **可选接入**：MeshAnything/Meshy T2/TRELLIS 三个开源项目评估接入 optimize/convert 工具链（有外部依赖，按缓存/依赖原则需评估）
