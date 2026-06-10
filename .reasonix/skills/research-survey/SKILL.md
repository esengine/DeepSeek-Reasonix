---
name: tm-seek
description:
  针对用户给出的任意领域技术主题，联网检索权威开源库、代表文献与公开资源，按「领域先验→方法演进→当前实践→前沿探索」的知识纵深路径组织，产出一份带可验证链接的中文调研
  md，并给出实践指引。Use when：技术调研、方法论演进、领域概览、开源库收集、survey、调研报告、给我一个 xx 的方案、xx
  是怎么做到的、xx 有哪些开源实现、xx 领域有哪些方法、xx 的知识体系。
---

# tm-seek（融合 ARIS research-lit 六源搜索）

按知识纵深调研一个技术主题，输出一份链接全部可验证的中文 md。

## ARIS 工具路径

```
ARIS_ROOT = ~/.claude/skills/Auto-claude-code-research-in-sleep-main/Auto-claude-code-research-in-sleep-main
ARIS_TOOLS = $ARIS_ROOT/tools
ARIS_SKILLS = $ARIS_ROOT/skills
```

## 运行环境

- Python 脚本统一用 `conda run -n markitdown python` 执行
- `markitdown` 环境路径：`D:\Miniconda3\envs\markitdown`
- Git Bash 下禁止 `conda activate`
- 代理：首次使用前，在 Git Bash 执行以下命令（一次性，之后 conda run 自动继承代理）：
  ```bash
  conda env config vars set -n markitdown HTTP_PROXY=http://127.0.0.1:7890
  conda env config vars set -n markitdown HTTPS_PROXY=http://127.0.0.1:7890
  ```
  Windows 下不能用 `env` 前缀，必须通过 `conda env config vars set` 将代理写入环境级别。
- Exa API：`EXA_API_KEY` 已配置，key 在 `C:\Users\EDY\Desktop\新建 文本文档.txt`

## API Key 说明

| 工具 | 需要 key？ | 怎么获取 |
|------|:--------:|----------|
| arXiv (`arxiv_fetch.py`) | ❌ 零依赖 | 不需要 |
| OpenAlex (`openalex_fetch.py`) | ❌ 零依赖 | 不需要 |
| Semantic Scholar | ⚠️ 不要也能搜，有 key 更快 | [semanticscholar.org/product/api](https://www.semanticscholar.org/product/api) 免费注册 |
| DeepXiv | ✅ 已安装 | `D:\Miniconda3\envs\markitdown` 已装 deepxiv-sdk |
| Exa | ✅ 已配置 | key 在 `C:\Users\EDY\Desktop\新建 文本文档.txt`，已装 exa-py |
| Gemini | ✅ 需要 | [Google AI Studio](https://aistudio.google.com/) 免费申请 |
| 微信公众号 (Exa) | ❌ 零依赖 | 走 Exa `include_domains=['mp.weixin.qq.com']` |

没有 key 的工具静默跳过，不影响调研。

## 工作流（严格按顺序执行）

### Step A · 主题解析

- 把用户输入归一化为：英文检索词（用于 GitHub/arXiv/学术搜索）+ 中文领域名（用于文件名）
- 例：「遥感火点检测」→ `wildfire detection remote sensing` + `遥感火点检测`
- 日期：文件夹用 `YYYYMMDD`，文档表头用 `YYYY-MM-DD`

### Step B · 多源检索

小批量并行调用。不预设技术分类，以开放姿态覆盖以下维度。

**B.1 六源学术搜索（并行启动，互不等待）**

```bash
# ========== 已配好（直接跑） ==========
# ① arXiv — 预印本（零依赖）✅
conda run -n markitdown python "$ARIS_TOOLS/arxiv_fetch.py" search "{query}" --max 15

# ② OpenAlex — 250M+ 开放引文图谱（零依赖）✅
conda run -n markitdown python "$ARIS_TOOLS/openalex_fetch.py" search "{query}" --max 10

# ③ DeepXiv — 渐进式阅读（已安装）✅
conda run -n markitdown python "$ARIS_TOOLS/deepxiv_fetch.py" search "{query}" --max 10

# ④ Exa — 全网搜索（API key 已配置）✅
conda run -n markitdown python "$ARIS_TOOLS/exa_search.py" --query "{query}" --max 10

# ④+ 微信公众号（Exa 限定 mp.weixin.qq.com，零额外配置）✅
conda run -n markitdown python -c "import os; from exa_py import Exa; exa = Exa(api_key=os.environ.get('EXA_API_KEY')); r = exa.search('{query}', type='auto', num_results=5, include_domains=['mp.weixin.qq.com'], contents={'highlights': True}); [print(f'{i.title} | {i.url}') for i in r.results]"

# ========== 没配好就跳过 ==========
# ⑤ Semantic Scholar — 免费注册即用
conda run -n markitdown python "$ARIS_TOOLS/semantic_scholar_fetch.py" search "{query}" --max 10

# ⑥ Gemini — AI 驱动广域发现
# 调用 $ARIS_SKILLS/gemini-search/SKILL.md
```

退回策略：arXiv + OpenAlex 必须跑通。其余无 key/未安装则静默跳过，数据不足时回退 B.2 网页抓取补齐。

**B.2 网页检索（覆盖工具覆盖不到的维度）**

| 维度 | 工具与查询 |
|------|-----------|
| 领域概览 | `fetch_webpage` 维基百科 `https://en.wikipedia.org/wiki/{topic}` |
| 综述/awesome | `fetch_webpage` `https://github.com/search?q=awesome+{query}&type=repositories&s=stars` |
| 开源实现 | `fetch_webpage` `https://github.com/search?q={query}+stars:>100&type=repositories&s=stars` |
| 学术文献 | （B.1 已覆盖；回退时）`fetch_webpage` arXiv/Semantic Scholar/Google Scholar |
| 数据/基准 | Hugging Face `https://huggingface.co/datasets?search={query}`；Kaggle |
| 技术文档 | 若领域有主流框架/工具，检索其官方文档站 |

搜索策略：
- 不预设方法分类（不用「朴素→ML→DL→大模型」固定路径）
- 从搜索结果中自然发现该领域的方法分类方式
- 记录每个发现的来源 URL

### Step C · 链接验证（强制）

- GitHub 仓库：`fetch_webpage` 实访 repo URL，确认非 404；提取 Star 数
- 学术文献：`fetch_webpage` 实访 arXiv abs 页或 doi.org/{DOI}，确认标题与作者匹配
- 数据集/资源页：实访确认存在
- 验证失败的链接直接剔除，写「暂无可验证来源」
- 二次引用未亲自打开的，放到末尾「未直接验证」

### Step D · 精选与组织

按以下四章组织内容：

#### 1. 领域先验与问题定义
核心概念、输入/输出、挑战、评价标准。精选 2-4 个权威综述/百科来源。

#### 2. 方法演进
由搜索结果自然呈现的方法发展阶段（时间线/范式/技术路线均可）。每阶段精选 2-4 个代表方法，各配 1 个开源库 + 1-2 篇文献。

#### 3. 当前最佳实践与工具
精选 3-5 个最活跃开源库（优先 Star≥500 或知名机构）。含名称、URL、Star、用途。数据集列表附本节。

#### 4. 前沿与开放问题
当前研究前沿、未解决问题、新兴趋势。精选 2-4 篇最新文献。

不滥列，宁可少。

### Step E · 输出

- 在工作区根目录下创建 `调研/{中文领域名}_{YYYYMMDD}/`
- 目录已存在时追加 `_v2`、`_v3`
- 生成 `survey.md`，骨架见同名目录的 [survey-template.md](survey-template.md)
- 「复现路线」给出两条（有标注 + 无标注/zero-shot），根据领域特点灵活设计

### Step F · 资源链接汇编

- 文献 DOI、GitHub 链接、数据集链接汇编到「参考来源」节
- 已验证的标 ✓
- 「未直接验证」段列出二次引用未亲自打开的条目

## 链式调用

```
/research-survey "主题"               → survey.md
/idea-discovery "方向"                → IDEA_REPORT.md（从 survey 找 gap）
/experiment-bridge                    → EXPERIMENT_LOG.md（跑实验）
/paper-write "NARRATIVE_REPORT.md"    → paper/main.pdf
```

ARIS 全部技能入口在 `$ARIS_SKILLS/`，路由索引见 `$ARIS_ROOT/AGENT_GUIDE.md`。

## 硬性规则

- 禁止编造任何链接、论文标题、作者、Star 数。找不到就写「暂无」
- 每个开源库条目：库名、GitHub URL、Star 数（拿不到写「未知」）、一句话用途
- 每篇文献：英文原标题、第一作者、年份、arXiv 或 DOI 链接
- 中文叙述，库名/论文标题/专有名词保留英文
- 所有正文链接必须是 Step C 已验证过的
- 二次引用未亲自验证的进「未直接验证」段落
