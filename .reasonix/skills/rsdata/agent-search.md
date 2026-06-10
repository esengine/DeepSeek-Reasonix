---
name: agent-search
description: >-
  多智能体并行搜索子技能。rsdata 发现数据时，调用此技能将搜索任务分解为多个维度的并行 agent，每个 agent 负责一个搜索方向（官方平台/中文平台/GitHub/数据期刊/科研仓库/AI平台/非正式渠道/政府开放数据），合并去重后返回结构化结果。适用于标准/深入搜索深度。
license: MIT
metadata:
  author: LY
  version: "2.0"
  requires: Workflow tool (available in Claude Code)
---

# agent-search — 多智能体并行数据搜索

你是 rsdata 的子技能，负责**并行化搜索执行**。

## 何时调用

- 用户要求标准或深入搜索（默认 10+ 条/类，深入 20+ 条/类）
- 需求明确、搜索维度 ≥3 个时
- rsdata 主技能判断需要多路并行搜索

## 输入参数

从 `args` 全局变量获取（JSON 对象）：

```json
{
  "variable_cn": "气温、降水",
  "variable_en": "temperature, precipitation",
  "region_cn": "南京",
  "region_en": "Nanjing",
  "lat": 32.06,
  "lon": 118.79,
  "time_start": "1952",
  "time_end": "2026",
  "station_id": "58238",
  "country": "CN",
  "satellite": null,
  "depth": "standard",
  "data_category": "meteorology"
}
```

`data_category`: `"meteorology"` | `"sar"` | `"optical"` | `"dem"` | `"ocean"` | `"atmosphere"` | `"land"` | `"nightlight"` | `"ai_model"` | `"general"`
— 由 rsdata 主技能根据用户需求判断，控制各搜索维度的权重分配。
`depth`: `"quick"` | `"standard"` | `"deep"`

## 执行逻辑

```
解析 args → 判断匹配的搜索维度 → 并行启动 agent(每个维度一个) → 收集结果 → 去重 → 返回结构化 JSON
```

## 搜索维度矩阵（自适应权重）

根据 `data_category` 自动调整各维度的搜索条目配额。`official` 和 `crossref` 始终启用，其余按数据类型智能分配。

**权重档位**：`■■■` 高(默认搜索条数) / `■■` 中(默认的 60%) / `■` 低(默认的 30%) / `—` 跳过

| 维度 | 气象/气候 | SAR | 光学 | DEM | AI模型 | 海洋 |
|------|----------|-----|------|-----|--------|------|
| `official` | ■■■ | ■■■ | ■■■ | ■■■ | ■■ | ■■■ |
| `chinese` | ■■■ | ■■■ | ■■■ | ■■■ | ■■ | ■■ |
| `journals` | ■■■ | ■■ | ■■ | ■■ | ■■■ | ■■ |
| `repos` | ■■ | ■■ | ■■ | ■■ | ■■■ | ■■ |
| `government` | ■■■ | — | — | ■ | — | ■ |
| `github` | ■■ | ■■■ | ■■■ | ■■ | ■■■ | ■■ |
| `ai_ml` | ■ | ■ | ■■ | ■ | ■■■ | ■ |
| `informal` | ■ | — | — | — | ■ | — |
| `crossref` | ■■ | ■■ | ■■ | ■■ | ■■ | ■■ |

**规则**：
- `ai_model` 类型 → `ai_ml` + `github` + `repos` 权重拉满，`government`/`informal` 跳过
- `sar` 类型 → 追加 `site:asf.alaska.edu` + `site:dataspace.copernicus.eu`，`government`/`informal` 跳过
- `meteorology` 类型 → `chinese` + `government` 权重拉满，`ai_ml`/`informal` 降到最低
- `country != CN` → `chinese` 维度自动跳过

## 搜索深度参数

| 深度 | 官方 | 中文 | 期刊 | 仓库 | 政府 | GitHub | AI | 非正式 | 交叉 |
|------|------|------|------|------|------|--------|-----|--------|------|
| quick | 5 | 5 | 3 | 3 | 3 | 3 | 2 | 2 | 3 |
| standard | 10 | 10 | 6 | 6 | 6 | 6 | 4 | 4 | 6 |
| deep | 20 | 20 | 12 | 12 | 12 | 12 | 8 | 8 | 12 |

> 上表为 baseline。高权重维度按 baseline 搜，中权重 ×60%，低权重 ×30%，跳过的不搜。最终总搜索量约 30~180 条。

---

## 各维度的搜索 prompt 模板

每个 agent 的 prompt 必须包含：
1. 搜索目标描述
2. 具体搜索关键词（中英文）
3. site: 限定
4. 输出格式要求

### 维度 1: official — 全球官方数据中心

```
Search for {variable_en} data covering {region_en} (lat:{lat}, lon:{lon}) from {time_start} to {time_end}.

Search these platforms (use web_search with site: restrictions):
- site:cds.climate.copernicus.eu {variable_en} {region_en}
- site:ecmwf.int {variable_en} reanalysis
- site:ncei.noaa.gov {region_en} station data
- site:psl.noaa.gov {variable_en} {region_en}
- site:crudata.uea.ac.uk {variable_en}
- site:worldclim.org {region_en}
- site:esgf-node.llnl.gov {variable_en} {region_en}
- site:isimip.org {variable_en}
- site:jra.kishou.go.jp {variable_en}
- site:data.cma.cn {variable_en}

Also search: "{variable_en} reanalysis {region_en} 1950", "best {variable_en} dataset {region_en} long-term"

For each result found, record: name, time coverage, spatial resolution, temporal resolution, variables, URL, access difficulty.

Return all findings.
```

### 维度 2: chinese — 中国数据平台

```
搜索覆盖 {region_cn}（经纬度 {lat}, {lon}）的 {variable_cn} 数据，时间范围 {time_start} 至 {time_end}。

搜索以下平台（用 web_search + site: 限定）：
- site:data.cma.cn {variable_cn} {region_cn}
- site:data.tpdc.ac.cn {variable_cn} {region_cn}
- site:resdc.cn {variable_cn}
- site:geodata.cn {variable_cn} {region_cn}
- site:noda.ac.cn {variable_cn}
- site:cresda.com {variable_cn}
- site:gscloud.cn {region_cn}

也搜期刊论文附带数据:
- site:csdata.org {variable_cn} {region_cn}
- site:geodoi.ac.cn {region_cn} {variable_cn}
- site:cnki.net {region_cn} {variable_cn} 数据

也搜非正式渠道:
- {region_cn} {variable_cn} 百度网盘 提取码
- {region_cn} {variable_cn} 阿里云盘 分享
- {region_cn} 气象 数据 下载 CSDN
- 如何获取 {region_cn} 气象数据 知乎

对每个结果记录: 数据集名称、时间覆盖、空间分辨率、时间分辨率、变量、下载入口、获取难度。
Return all findings.
```

### 维度 3: github — GitHub 工具仓库

```
Search GitHub for tools to download {variable_en} data for {region_en}.

Search queries (use web_search):
- "{variable_en} station data download python github"
- "ERA5 download tool python github stars"
- "GHCN daily data download python"
- "China meteorological data download tool github"
- "NOAA climate data access python github"
- "awesome climate data github"
- "awesome meteorological data github"
- "CMFD download script github"
- "data.cma.cn python download"
- "{region_en} weather data csv github"

For each repo found, record: full_name, stars, last_updated, maintenance_status, one-line description, URL.
```

### 维度 4: journals — 数据期刊论文

```
Search for data descriptor papers about {variable_en} datasets covering {region_en}, {time_start}-{time_end}.

Search (use web_search):
- "{variable_en} {region_en} site:nature.com/sdata"
- "{variable_en} China dataset site:earth-system-science-data.net"
- "{variable_en} {region_en} Geoscience Data Journal"
- "{region_en} meteorological dataset data paper"
- "{variable_en} station data China data descriptor"
- "{time_start} {variable_en} China dataset DOI"

Also search for cross-validation papers (which reveal data quality):
- "evaluation of {variable_en} dataset over {region_en}"
- "validation of gridded {variable_en} China station"

For each dataset found, record: name, DOI, time range, resolution, variables, data hosting URL (usually Zenodo/Figshare).
```

### 维度 5: repos — 科研数据仓库

```
Search general research data repositories for {variable_en} data covering {region_en}.

Use web_search:
- site:zenodo.org "{region_en}" {variable_en} station
- site:zenodo.org China {variable_en} dataset
- site:figshare.com {region_en} meteorological
- site:figshare.com China {variable_en} station
- site:pangaea.de {region_en} {variable_en}
- site:datadryad.org {region_en} climate

For each result: dataset name, DOI, temporal coverage, spatial extent, variables, format, file size.
```

### 维度 6: ai_ml — AI/ML 平台

```
Search AI/ML platforms for {variable_en} datasets and models related to {region_en}.

Use web_search:
- site:huggingface.co {variable_en} dataset
- site:huggingface.co China climate data
- site:kaggle.com {region_en} weather dataset
- site:kaggle.com China meteorological
- site:mlhub.earth {variable_en}
- site:aistudio.baidu.com {variable_cn} 数据集
- site:heywhale.com {variable_cn} 数据
- site:opendatalab.com {variable_cn} {variable_en}
- site:tianchi.aliyun.com {variable_cn} 气象

For each: name, platform, size, downloads, update date, format.
```

### 维度 7: informal — 非正式渠道

```
Search informal channels (Chinese forums, blogs, cloud drives) for {variable_cn} data of {region_cn}.

Search:
- "{region_cn} {variable_cn} 百度网盘"
- "{region_cn} 气象 数据 百度网盘 提取码"
- "{region_cn} 气温 历史 数据 下载 site:csdn.net"
- "{region_cn} 气象 数据 site:zhihu.com"
- "{region_cn} 气象 数据 site:cnblogs.com"
- "如何获取 {region_cn} 历史气象数据"
- "{region_cn} 历年 气温 降水 统计"

Record: filename, platform, extraction code (if any), source post URL, and a warning that links may expire.
```

### 维度 8: government — 政府开放数据

```
Search government open data portals for {variable_en} data of {region_en}.

Search:
- "site:data.gov.cn {variable_cn}"
- "{region_cn} 公共数据 开放 平台 气象"
- "{region_cn} 统计年鉴 气温 降水 site:tjj.nanjing.gov.cn" (adapt city name)
- "site:data.gov {region_en} weather station"
- "site:data.gov.uk {region_en} climate"
- "site:climateknowledgeportal.worldbank.org {region_en}"

Also search domain-specific databases:
- "HadISD {region_en} station"
- "ISTI {region_en} temperature"
- "GPCC precipitation {region_en}"
- "APHRODITE precipitation Asia {region_en}"
- "FluxNet {region_en}" (if relevant)

For each: name, URL, coverage, access method.
```

### 维度 9: crossref — 论文反向追踪

```
Search for papers that have USED {variable_en} data for {region_en} — find what datasets they used.

Search (use web_search):
- "{region_en} {variable_en} trend {time_start} study site"
- "{region_en} climate change long-term analysis dataset"
- "temperature precipitation {region_en} historical data paper"
- "{region_en} extreme weather event {time_start} data"
- "filetype:pdf {region_en} temperature precipitation table"
- "{region_en} meteorological station record digitized"

For each paper found: note which datasets they used, where the data was obtained from, any data quality notes mentioned.
```

---

## 输出格式

所有维度 agent 返回结果后，合并去重，生成如下 JSON（仅作为中间数据结构，供 rsdata 主技能消费）：

```json
{
  "datasets": [
    {
      "name": "ERA5-Land",
      "source_dimension": "official",
      "institution": "ECMWF",
      "time_start": "1950",
      "time_end": "present",
      "spatial_resolution": "0.1deg",
      "temporal_resolution": "hourly",
      "variables": ["2m_temperature", "total_precipitation", ...],
      "url": "https://cds.climate.copernicus.eu/...",
      "access": "free_registration",
      "gee_available": true,
      "notes": "1950-1978 is back extension, lower quality"
    }
  ],
  "github_repos": [
    {
      "full_name": "ecmwf/cdsapi",
      "stars": 315,
      "updated": "2024",
      "status": "active",
      "description": "Official CDS Python API",
      "url": "https://github.com/ecmwf/cdsapi"
    }
  ],
  "unverified_links": [
    {"url": "...", "reason": "403", "dimension": "informal"}
  ],
  "search_statistics": {
    "dimensions_searched": 9,
    "total_queries": 90,
    "datasets_found": 25,
    "repos_found": 15,
    "duration_estimate": "30-60 seconds"
  }
}
```

## 注意事项

1. **并行启动**：所有维度的 agent 同时启动，不串行等待
2. **不重复搜索**：如果 rsdata 主技能已做了初步搜索，agent-search 聚焦在深度和广度扩展
3. **标注未核验**：搜索引擎摘要结果标注 "from search snippet, not verified by web_fetch"
4. **去重**：同一数据集在不同维度可能出现，按 DOI/URL/名称去重
5. **去噪**：排除天气预报、新闻、商业广告等无关结果
