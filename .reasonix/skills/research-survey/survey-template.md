# {中文领域名} 技术调研

> 检索词：`{english query}`　|　生成日期：{YYYY-MM-DD}

## 1. 领域简介

{1-2 段中文概述：问题定义、典型输入输出、主要挑战}

来源：[{来源名}]({url})

## 2. 演进路径

### 2.1 朴素 / 规则算法

代表方法：{方法 1}、{方法 2}

开源实现：
- [{库名}]({github_url}) · ⭐ {stars} · {一句话用途}

代表论文：
- {English Paper Title} — {First Author} et al., {year} · [arXiv/DOI]({url}) · [本地 PDF](pdfs/{filename}.pdf)

### 2.2 传统机器学习

代表方法：{方法}

开源实现：
- [{库名}]({github_url}) · ⭐ {stars} · {一句话用途}
（传统 ML 阶段允许仅列代表方法 + 作者主页 + 论文，不强求 Star ≥ 500）

代表论文：
- {English Paper Title} — {First Author} et al., {year} · [arXiv/DOI]({url}) · [本地 PDF](pdfs/{filename}.pdf)

### 2.3 深度学习

代表方法：{方法}

#### 监督学习（Supervised）
- [{库名}]({github_url}) · ⭐ {stars} · 监督 · {一句话用途}
- {English Paper Title} — {First Author} et al., {year} · [arXiv/DOI]({url}) · [本地 PDF](pdfs/{filename}.pdf)

#### 无监督 / 自监督（Unsupervised / Self-Supervised）
- [{库名}]({github_url}) · ⭐ {stars} · 自监督 · {一句话用途}
- {English Paper Title} — {First Author} et al., {year} · [arXiv/DOI]({url}) · [本地 PDF](pdfs/{filename}.pdf)

#### 迁移 / Zero-shot（Transfer / Zero-shot）
- [{库名}]({github_url}) · ⭐ {stars} · zero-shot · {一句话用途}
- {English Paper Title} — {First Author} et al., {year} · [arXiv/DOI]({url}) · [本地 PDF](pdfs/{filename}.pdf)

### 2.4 大模型 / 基础模型

代表方法：{方法}

#### Zero-shot 直推
- [{库名}]({github_url}) · ⭐ {stars} · zero-shot · {一句话用途}
- {English Paper Title} — {First Author} et al., {year} · [arXiv/DOI]({url}) · [本地 PDF](pdfs/{filename}.pdf)

#### 提示 / 微调（Prompt / Fine-tune）
- [{库名}]({github_url}) · ⭐ {stars} · prompt/微调 · {一句话用途}
- {English Paper Title} — {First Author} et al., {year} · [arXiv/DOI]({url}) · [本地 PDF](pdfs/{filename}.pdf)

## 3. 公开数据集

| 名称 | 简介 | 链接 |
|---|---|---|
| {Dataset Name} | {一句话规模与用途} | [{host}]({url}) |

## 4. 推荐复现路线

### 路线 A · 有标注数据
1. 入门：使用「2.1」中的 {库名} 跑通基线，建立评测流程
2. 进阶：切换到「2.3 监督学习」中的 {库名}，复现深度学习基线
3. 追前沿：基于「2.4 提示/微调」中的 {库名} 做 fine-tune
4. 数据：使用「3」中的 {Dataset Name} 作为标准评测集

适用场景：有配对/标注数据、追求最高精度、需要业务可控
风险：数据获取与标注成本

### 路线 B · 无数据集 / zero-shot
1. 直接用「2.4 Zero-shot 直推」中的 {预训练权重/基础模型} 做推理
2. 可选：用业务侧无标注数据做「2.3 自监督」fine-tune
3. 业务验证：把输出送入下游模型，用任务指标（mAP / F1 等）评测

适用场景：无标注数据、快速 PoC、应急出图
风险：生成式方法可能产生「幻觉」细节，对几何精度敏感任务慎用

## 5. 参考来源

- ✓ [{title}]({url})　— GitHub 仓库已实访
- ✓ [{paper title}]({url})　— arXiv/DOI 已实访
- 来源标注 [{wiki title}]({url})　— 维基百科 / 百科类

## 6. 未直接验证（请使用前自行确认）

- {Title} — 来自 {awesome 列表 url}，未亲自打开

## 7. 未下载（PDF 抓取失败或无 arXiv 版本）

- {Title} — 原因：{无 arXiv 版本 / 抓取失败 HTTP xxx / DOI 付费墙}
