# paper-reverse-search — 从论文反向追踪数据源

**搜不到公开数据集时，先搜使用过该类型数据的论文，从论文 Methods/Data 章节提取数据来源。**
**一次成功的论文追踪可带出 3-5 个数据集，是效率最高的数据发现方法。**

## 核心思路

```
搜论文 → 看他们用了什么数据 → 追溯数据 DOI → 下载
```

论文作者已经做过了数据源调研，不需要你从头搜。

---

## 一、数据期刊论文搜索（优先级最高）

数据描述论文（data descriptor / data paper）专门介绍数据集，内容包含完整元数据。

### 英文数据期刊

```
# Scientific Data (Nature)
web_search "{变量} {区域} site:nature.com/sdata"
web_search "{数据类型} data descriptor site:nature.com/sdata"

# Earth System Science Data (ESSD)
web_search "{变量} {区域} site:earth-system-science-data.net"
web_search "{变量} dataset China site:earth-system-science-data.net"

# Geoscience Data Journal (RMets/Wiley)
web_search "{变量} {区域} site:rmets.onlinelibrary.wiley.com"
web_search "China meteorological dataset site:rmets.onlinelibrary.wiley.com"

# Data in Brief (Elsevier)
web_search "{变量} {区域} data in brief"

# Big Earth Data (T&F)
web_search "{变量} China site:bigearthdata.com"

# Data (MDPI)
web_search "{变量} {区域} site:mdpi.com/journal/data"
```

### 中文数据期刊

```
web_search "{变量} site:csdata.org"
web_search "{地点} {变量} site:geodoi.ac.cn"
web_search "中国 气象 数据集 数据论文"
web_search "{变量} 数据集 描述 DOI"
```

---

## 二、从研究论文中追溯数据源

### 搜"谁用过这个数据"

```
web_search "{数据集名} {区域} study site"
web_search "{数据集名} {变量} trend analysis {区域}"
web_search "{数据集名} validation {区域}"
web_search "{数据集名} vs {另一个数据集名} {区域}"
```

从结果中找引用该数据集做南京/中国研究的论文，论文中对数据集描述往往比官方页面更详细。

### 搜数据交叉验证论文

```
web_search "{数据集A} compared with {数据集B}"
web_search "{数据集A} intercomparison {数据集B}"
web_search "evaluation of {数据集名} over China"
web_search "validation of {数据集名} station observation China"
```

交叉验证论文通常附带多个数据集的详细精度评估，能告诉你哪个数据集在南京表现最好。

### 搜附带数据表格的论文

```
web_search "{地点} temperature precipitation {年份} table"
web_search "{地点} climate change {时间段} data"
web_search "long-term trend {变量} {区域} supplementary"
filetype:pdf "{地点}" "{年份}" "temperature" "precipitation"
```

---

## 三、数据引用链追溯（递归发现）

**找到一个数据集 → 看什么论文引用了它的 DOI → 这些论文还用了什么其他数据 → 递归扩展**

```
Step 1: 用 DOI 搜索引用论文
web_search "{DOI} cited by"
web_search "{DOI}" (Google Scholar 直接显示引用次数)

Step 2: 从引用论文的致谢/数据声明找新数据源
→ 论文的 "Data Availability Statement" 段通常列出 2-5 个数据集

Step 3: 递归
→ 对每个新发现的数据集，重复 Step 1-2
```

---

## 四、预印本服务器（发现最新未发表数据集）

```
web_search "site:arxiv.org {变量} dataset China"
web_search "site:arxiv.org {区域} climate station data"
web_search "site:essoar.org {变量} China"
web_search "site:researchgate.net {变量} {区域} dataset"
```

预印本比正式论文早 6-12 个月，用于发现即将发布的新数据集。

---

## 五、反向搜数据下载页面（找使用者教程）

```
web_search "how to download {数据集名} step by step"
web_search "{数据集名} 下载 教程"
web_search "{数据集名} 获取 方法"
web_search "where to find {数据集名} data"
```

教程通常会写清楚注册流程、数据格式、注意事项，比官方文档更实用。

## 关键原则

- **数据论文 > 研究论文 > 官方网站**——数据论文对数据集的描述最详细
- 搜"谁用过"比搜"哪里有"更高效
- 一篇好的交叉验证论文可以替代你手动对比 10 个数据集
- 递归追溯不要超过 3 层（避免迷失）
