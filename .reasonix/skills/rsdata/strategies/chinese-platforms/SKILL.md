# chinese-platforms — 中文平台 + 中文论文数据源搜索策略

**国内数据平台（resdc.cn、geodata.cn、data.tpdc.ac.cn 等）通常 JS 渲染 + 反爬，web_fetch 拿到空白页。以下搜索引擎摘要策略替代。**

## 一、按平台分类的搜索模板

### 资源环境科学与数据中心 (resdc.cn)

```
web_search "site:resdc.cn {数据集名}"
web_search "{数据集名} resdc 分辨率 格式"
web_search "resdc.cn {数据集名} 下载"
```

摘要通常包含：分类体系、年份、空间分辨率、格式(GeoTIFF/Shapefile)。

### 国家地球系统科学数据中心 (geodata.cn)

```
web_search "site:geodata.cn {数据集名}"
web_search "{数据集名} geodata 数据说明"
```

### 国家青藏高原科学数据中心 (data.tpdc.ac.cn)

```
web_search "site:data.tpdc.ac.cn {数据集名}"
web_search "{数据集名} 青藏高原 数据中心 下载"
```

### 国家对地观测科学数据中心 (noda.ac.cn)

```
web_search "site:noda.ac.cn {卫星名/传感器}"
web_search "noda.ac.cn 高分 数据 下载"
```

### 中国资源卫星应用中心 (cresda.com)

```
web_search "site:cresda.com {卫星名} 数据"
web_search "高分系列 CRESDA 数据申请 下载"
```

### 地理空间数据云 (gscloud.cn)

```
web_search "site:gscloud.cn {数据集名}"
web_search "gscloud.cn Landsat MODIS 下载"
```

### 中国气象数据网 (data.cma.cn)

```
web_search "site:data.cma.cn {变量名}"
web_search "CMA CRA-40 下载 格式"
web_search "中国气象数据网 站点数据 {站号} 历史 下载"
```

---

## 二、中文论文数据库（新增——从论文反向追数据）

**硕博论文经常在附录直接附带 CSV/Excel 数据。这是一条常被忽略但极其有效的管道。**

### 知网 (CNKI)

```
web_search "site:cnki.net {地点} {变量} 数据集"
web_search "{地点} 气象 数据 分析 site:cnki.net"
web_search "{地点} 气温 降水 长期 变化 硕士 论文"
```

### 万方

```
web_search "site:wanfangdata.com.cn {地点} {变量} 数据"
```

### 百度学术

```
web_search "{地点} {变量} 数据集 下载"
web_search "{地点} {年份} 气象 资料 来源"
```

### 中国科学数据 (csdata.org)

```
web_search "site:csdata.org {变量}"
web_search "csdata.org 气象 数据 论文"
```

### 全球变化数据学报 / 全球变化科学研究数据出版

```
web_search "site:geodoi.ac.cn {地点}"
web_search "{数据集名} geodoi 数据论文"
```

---

## 三、百度网盘/阿里云盘/夸克网盘

```
web_search "{数据集名} 百度网盘 提取码"
web_search "{数据集名} 阿里云盘 分享"
web_search "{数据集名} 夸克网盘 下载"
web_search "{数据集名} 微云 下载"
```

只记录文件名、提取码（如有）、来源帖链接。不 web_fetch。
标注"链接具有时效性，如失效请用文件名+提取码重新搜索"。

---

## 四、知乎/CSDN/博客园

```
web_search "{数据集名} site:zhihu.com"
web_search "{数据集名} site:csdn.net"
web_search "{数据集名} site:cnblogs.com"
web_search "如何获取 {地点} 气象数据"
web_search "{地点} 气象 数据 免费 下载 教程"
```

从搜索摘要提取技术细节，不做 web_fetch（这些站点统一 403）。

## 关键原则

- 中文平台几乎全员 JS 渲染 + 反爬，web_fetch 是徒劳的
- Google 对中文站点索引不如 Bing/百度，必要时用不同搜索引擎试试
- 论坛/博客的第三方描述往往比官方页面更详细
- **中文硕博论文是隐藏数据矿**——搜论文比搜数据集更容易找到可用数据
