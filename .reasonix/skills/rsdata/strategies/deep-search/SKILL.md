# deep-search — 搜索引擎摘要挖掘策略

**只用 web_search，不依赖 web_fetch。** 当目标页面 403/JS渲染/超时时，从搜索引擎结果摘要中拼凑数据集关键信息。

## 适用场景
- web_fetch 遇到 403、JS 渲染空白页、连接超时
- 需要从多个渠道交叉验证某个数据集参数
- 百度网盘等非正式渠道——从论坛帖子摘要提取文件名和提取码

## 执行步骤

### 第 1 步：site: 限定直搜

```
web_search "site:{目标域名} {数据集关键词}"
```

从搜索结果摘要提取：标题、描述片段、日期。

### 第 2 步：第三方引用搜索

```
web_search "{数据集名} 数据集 description"
web_search "{数据集名} spatial resolution temporal coverage format"
web_search "{数据集名} data paper"
```

学术博客、论坛帖子、数据论文引用中通常包含分辨率、时间范围、格式。

### 第 3 步：GitHub 专用

```
web_search "{owner}/{repo} github stars"
web_search "{owner}/{repo} readme description"
```

Google/Bing 搜索结果直接显示 ⭐ 数、语言、最近更新。

### 第 4 步：中文社区专用

```
web_search "{数据集名} site:zhihu.com"
web_search "{数据集名} site:csdn.net"
web_search "{数据集名} site:cnblogs.com"
web_search "{数据集名} 下载 使用教程"
```

中文社区对国内数据集（CLCD、CNLUCC 等）的描述往往比官方更详细。

### 第 5 步：百度网盘专用

```
web_search "{数据集名} 百度网盘"
web_search "{数据集名} 提取码"
web_search "{数据集名} 百度云 下载 链接"
```

从搜索结果摘要提取：文件名、提取码、文件大小。

### 第 6 步：交叉验证综合输出

汇总所有来源，每条标注来源类型。找不到的字段写"未确认"。

| 字段 | 值 | 来源 |
|------|-----|------|
| 时间覆盖 | 1985-2020 | 搜索引擎摘要 (zhihu.com) |
| 空间分辨率 | 30m | 第三方引用 (CSDN) |
| ... | ... | ... |
