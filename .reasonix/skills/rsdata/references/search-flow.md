# 搜索执行流程

> 此文件从 rsdata 主 SKILL.md 拆分。定义完整搜索执行步骤。

---

```
Step 0: 代理检测（每次必做，不可跳过）
  → 读取 HTTP_PROXY / HTTPS_PROXY 环境变量
  → 用 AskUserQuestion 向用户确认代理地址
  → web_fetch "https://github.com" 验证代理是否生效
  → 标记 proxy_ok=true/false
  → 禁止假设代理一定可用，每次任务重新检测

Step 1: 需求澄清（每次必做，不可跳过）
  → 检查 5 维度（变量/时间精度/空间精度/用途/获取能力）
  → 缺 1 个以上 → 用 AskUserQuestion 提问
  → 需求仍然模糊 → 调用 Skill("brainstorming") 深入澄清
  → 确认后打印："已确认搜索目标：{变量}，{时间范围}，{空间}，{精度}，用途{用途}"
  → 用户说"快速"或"--quick" → 跳过提问，按默认值处理

Step 2: 解析 + 分类
  → 提取：变量(中→英翻译)、时间、空间、分辨率、格式
  → 读取 references/data-sources.md，判断匹配 A~M 中哪些类别
  → 判断数据类型（气象/SAR/光学/AI权重/其他）→ 影响后续搜索权重

Step 3: 并行搜索 — 两条路同时走
  A路: web_fetch 访问官方页面
  B路: web_search 搜索引擎摘要
  → 合并两路信息，不等待 A 失败再补 B

Step 4: 决策 — 是否需要 agent-search
  标准/深入模式 + 维度≥4 → 调用 Skill("agent-search", args=JSON)
  快速模式 → 继续手动搜索

Step 5: 数据论文 + 论文反向追踪
  → 读取 strategies/paper-reverse-search/SKILL.md

Step 6: 政府开放数据 + 国际组织
  → 读取 strategies/government-open-data/SKILL.md

Step 7: GitHub 仓库搜索
  → 读取 strategies/github-search/SKILL.md

Step 8: AI 搜索增强（多轮）
  → 按 references/data-sources.md #M 执行多轮搜索

Step 9: 去重、交叉验证、生成报告
  → 速览放最开头（3 行：最佳选择 / 最快捷 / 最完整）
  → 详见下方输出模板
```

## 输出结构

### 0. 速览（报告最开头，3 行以内）

```
## 速览
- **最佳选择**: [数据集名] — [一句话理由，含时间覆盖和获取入口]
- **最快捷**: [数据集名] — [零门槛获取方式]
- **最完整**: [数据集名] — [变量数/分辨率等核心卖点]
```

### 1. 需求解析

```
- 地理变量：xxx（中文）/ xxx（英文）
- 时间范围：YYYY-MM 至 YYYY-MM
- 空间范围：全球 / 中国 / xxx区域
- 时间分辨率：逐时/逐日/逐月/逐年
- 空间分辨率：0.1°/1km/10km/...
- 格式偏好：NetCDF / GeoTIFF / CSV / ...
- 站点编码（如有）：CMA xxx / WMO xxx / ICAO xxx
```

### 2. 数据集总览表

| # | 数据集名称 | 机构 | 时间覆盖 | 空间分辨率 | 时间分辨率 | 变量 | 获取难度 |

获取难度：⭐ 直接下载 / ⭐⭐ 需注册(免费) / ⭐⭐⭐ 需申请审核 / ⭐⭐⭐⭐ 付费

### 3. 各数据集详情

每个数据集模板：

```
#### 数据集名称 (英文全称)

- **机构**：xxx
- **时间覆盖**：YYYY-MM 至 YYYY-MM
- **空间分辨率**：xxx
- **时间分辨率**：xxx
- **变量**：列出具体变量名
- **格式**：NetCDF / GeoTIFF / CSV / ...
- **获取入口**：URL
- **获取难度**：⭐ 直接下载 / ⭐⭐ 需注册(免费) / ⭐⭐⭐ 需申请 / ⭐⭐⭐⭐ 付费
- **注册条件**：xxx（如 "需要 edu 邮箱"）
- **数据大小**：xxx（如 "全球约 200GB"）
- **GEE 可用**：是 / 否
- **核验状态**：✅ 已访问 / ⚠️ 搜索引擎摘要 / ❌ 未核验(原因)
- **备注**：xxx
```

### 4. GitHub 工具仓库

```markdown
| 仓库 | ⭐ | 更新 | 状态 | 描述 |
|------|-----|------|------|------|
| owner/repo | 315 | 2025-01 | active | xxx | (链接) |
```

### 5. 使用建议

- 推荐数据集组合
- 注意事项（版本选择、已知问题、区域适用性）
- 注册/申请建议

---

## 何时读取策略文件

| 遇到障碍 | 读取 |
|---------|------|
| web_fetch 403/JS/超时 | `strategies/deep-search/SKILL.md` |
| GitHub 连接超时 | `strategies/github-search/SKILL.md` |
| resdc.cn / data.cma.cn / 百度网盘 / 知乎 / CNKI | `strategies/chinese-platforms/SKILL.md` |
| HuggingFace / 百度AI Studio / Kaggle | `strategies/ai-platforms/SKILL.md` |
| 找不到数据集 → 搜论文 | `strategies/paper-reverse-search/SKILL.md` |
| 气象/统计类数据 → 搜政府公开数据 | `strategies/government-open-data/SKILL.md` |
| 更新数据源平台列表 | `references/data-sources.md` |
| 遇到异常情况处理 | `references/fallback-strategies.md` |
