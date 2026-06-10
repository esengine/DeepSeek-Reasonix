# github-search — GitHub 仓库搜索策略

**当 GitHub 直连超时或 web_fetch 失败时，用搜索引擎替代。**
**即使 GitHub 可达，也应同时跑搜索引擎——搜索引擎对 GitHub 的索引比站内搜索更完整。**

## 搜索模板

### 1. 查仓库基本信息（⭐、语言、更新日期——搜索引擎结果页直接显示）

```
web_search "{owner}/{repo} github"
web_search "{owner}/{repo} stars"
web_search "{owner}/{repo} readme description"
web_search "{owner}/{repo} installation usage"
```

### 2. 发现相关仓库 — Awesome List 系统搜索

**Awesome List 一个页面带出 20-50 个仓库，是最快发现路径。对每个子领域都要搜：**

```
web_search "awesome remote sensing {主题} github"
web_search "awesome {主题} dataset github"
web_search "awesome earth observation {主题} github"
web_search "awesome climate data github"
web_search "awesome open data github"
web_search "awesome satellite data github"
web_search "awesome china climate github"
web_search "awesome china remote sensing github"
```

### 3. 按数据源名称搜索专用工具

**不只要搜通用工具，更要搜有没有人针对特定数据平台写了下载器：**

```
web_search "data.cma.cn python download"
web_search "{数据集名} download script github stars"
web_search "{数据集名} tutorial notebook jupyter github"
web_search "tpdc.ac.cn python"
web_search "ghcn {站点号} github"
web_search "{变量} {平台} python download github"
web_search "how to download {数据集名} python"
```

### 4. 按卫星/产品名搜索

```
web_search "Landsat download python github"
web_search "Sentinel-2 preprocessing tool github"
web_search "MODIS data access script github"
web_search "ERA5 download script github"
web_search "{卫星/产品名} data access github"
```

### 5. 按研究主题搜索

```
web_search "{研究主题} dataset code github"
web_search "{研究主题} benchmark dataset github"
web_search "{研究主题} open source data tools github"
```

## 关键原则

- 搜索引擎摘要已有 ⭐、语言、描述、最后更新——不需要点进去
- 教程和 Notebook 比纯工具仓库更容易发现实际可用代码
- 多用具体 query（数据源名称、站号、产品名），少用泛化关键词
