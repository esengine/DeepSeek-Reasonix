# 检索系统领域学习测试：律算合一 vs 互联网比对

日期：2026-08-03 晚（北京时间）
工具：`cmd/dbg-usage/domain_learn.go`（本地知识注入 + retrieve_info 命中 + 真实 web_search 比对）
学习对象：`/data/work/discrete-mathematics`（律算合一 Law-Computation Unified，380 Agda 文件）

## 1. 本地知识注入（3 条，Tier=domain）

| 条目 | 内容摘要 |
|---|---|
| 律算合一总览 | GF(3) 三进制/T⁶ 环面/CRT 谐波谱/LCM 商空间，L0-L8 分层，85 文件 0 错误 |
| CRT 理论 | 双振子 T₁=65536, T₂=177147 拍频谱，Z/M ≅ Z/65536×Z/177147，纤维 P⁻¹(144,46) |
| Scholar Loop 实验 | Chern C=±2 Δ=0.04%、√3 能隙 0.3103、π_H=144/46 FOM 0.3379、ρ≈0.38 极限环 |

注入方式：`SaveKnowledge`（查询=中文概念，摘要=README 提取，ExpiresAt=7 天）

## 2. 命中验证（零联网）

```
律算合一 形式化验证     → 缓存命中 3ms
CRT 双振子谐波谱       → 缓存命中 1ms
Scholar Loop 陈数实验  → 缓存命中 1ms
```

## 3. 互联网比对（真实 deepseek-responses 管道）

| 本地概念 | 互联网查询 | 结果 | 交叉验证 |
|---|---|---|---|
| CRT 双振子 | CRT beat frequency harmonic two oscillators | WEB 1m16s | **一致**：CRT 同构需互质（gcd(a,b)=1）是标准数论，律算合一的 T₁=65536/T₂=177147 正是互质对 |
| GF(3) 三进制 | GF(3) ternary formal verification | 缓存 | **一致**：GF(3) 是标准有限域，Agda 形式化验证是成熟方向 |
| T⁶ 环面同伦 | 6-torus fundamental group | 缓存 | **一致**：π₁(T⁶)≅Z⁶（可换环面）是标准代数拓扑，项目用 GF(3)⁶ 是有限化变体 |

## 4. 比对结论

**数学内核一致，映射是项目特有**：
- ✅ 标准层：CRT 同构/GF(3) 有限域/环面同伦——与互联网数学完全对应
- ⚠️ 特有层：谐波谱（物理拍频映射）、纳音孤子、陈数守卫、LCM 桥——项目将 CRT/GF(3) 重新解释为物理/工程结构，互联网无直接对应（原创构造）

**检索系统能力验证**：
- 本地知识注入（Tier=domain）→ 缓存命中 ✅
- 语言门（中文本地条目不被英文查询误命中）✅
- 真实 web_search 英文比对（标准数学）✅
- 注入的知识与 web 检索结果在同一缓存共存、可交叉对照 ✅

## 5. 用途

该模式可用于：任何本地项目/资料库 → 蒸馏注入 → 后续对话零成本命中 + 与互联网标准知识交叉验证（学习本地特有构造时能区分"标准数学"与"项目映射"）。

---

# 补充：定理级完整学习（2026-08-03 第二次迭代）

> 用户批评第一次只学 README 摘要（浅层）。本次**定理级学习**：扫描 126 个
> 含定理的文件，抽取核心域的结构公理（record）/引理/常数，结构化注入。

## 注入（6 域 + 1 补丁，Tier=domain，ExpiresAt=30 天）

| 域 | 内容 |
|---|---|
| 宪法常数 | M=3¹¹×2¹⁶、FULL_TOUR=6624=144×46、X₀、OMEGA₀、RESONANCE、CHERN=±2 |
| RootMath | Gaussian(Sqrt3/Sqrt2/Eisenstein) 代数结构、StableLengthRatio |
| Coupling | DiscreteFiberBundle/Connection/Curvature（Cartan 挠率）、LCM 引理 ∀k n→n<3^k→go k n 1≡n、Twistor/NullGeodesic、DiscreteBerryCurvature |
| HoTT | StandingWave、ChernClass、KanFiller（离散 CCHM）、π₁(T⁶)≅GF(3)⁶、DiscreteHomotopy |
| Structology | HolographicPi 割圆同构、MagicSquare144/M4、QuotientT6A4、HomologyGroup、2T 二元四面体群 |
| T6 定理 | 144 极向 A4 投影 / 46 环向巡游 / 6624 全息闭合 / 144 步和乐归零 |

## 命中验证（6/6 零联网）

```
FULL_TOUR 6624 全息闭合   → 命中 4ms
DiscreteFiberBundle 纤维丛 → 命中 1ms
Eisenstein 艾森斯坦整数   → 命中 1ms
π1 T6 同伦 GF3           → 命中 1ms
HolographicPi 全息π      → 命中 1ms
LCM 引理 三进制归约       → 命中 1ms（补丁条目，短查询需专属 Query）
```

## 元认知提升

- 第一次（README 摘要）：3 条概述——只知道"有什么"
- 第二次（定理级）：7 条结构/定理/常数——知道"结构是什么/定理证什么/常数多少"
- 检索系统现在能回答：宪法常数数值、纤维丛三件套、LCM 引理签名、
  π₁(T⁶)≅GF(3)⁶、6624=144×46 全息闭合、2T 群等**具体数学内容**
- 短查询召回：短查询（8 字）对长条目（15 字）L2 相似度不足——为高频
  短查询建专属条目（Query 贴近实际查询词），或依赖变体学习累积

---

# 补充：逐文件级完整学习（2026-08-03 第三次迭代）

> 用户批评第二次只按域聚合 7 条（126 个含定理文件应逐文件学习）。
> 本次自动化扫描 src/Sovereign/ 全部 .agda，每个含结构/定理/公理的
> 文件生成一条知识条目（模块路径 + record/lemma/postulate 清单）。

## 注入（domain_learn3.go 自动扫描）

- 扫描 380 文件（src/Sovereign/），跳过纯 import/空文件
- **注入 ~126 条**（模块名 + 结构名 + 中文域提示进 Query）
- 每条含：record 结构清单 / lemma 签名 / postulate 计数（0 postulate 标注完全证明）
- 缓存 23 → **149 条**（未超 MaxKnowledgeEntries=500）

## 命中验证（8/8 零联网）

```
L1 Sovereign.Coupling.CartanTorsion    1ms
L1 MagicSquareM4                       3ms
L1 HolographicPi                       0ms
L1 DiscreteBerryCurvature              0ms
L1 卡坦挠率                            0ms
L2 Sovereign.Structology.T6   sim=0.78
L1 ZhonglvPhaseSync                    0ms
L2 Sovereign.HoTT.CRTHarmonics sim=0.77
```

## 设计要点

1. **Query 含三要素**（模块名 + 结构名 + 中文域提示）——L2 只比对 Query
   字段，中文概念必须进 Query 才能被中文查询命中（"卡坦挠率"命中）。
2. **逐文件粒度**：每个模块一条，126 条覆盖全库结构公理——查询任一
   模块名/结构名即 L1 命中，中文概念 L2 语义命中。
3. **0 postulate 标注**：完全证明的模块可检索（"（0 postulate：完全证明）"）。
