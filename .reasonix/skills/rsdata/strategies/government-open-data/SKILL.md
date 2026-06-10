# government-open-data — 政府开放数据平台 + 国际组织数据搜索

**气象数据是政府开放数据的典型品类。很多国家气象局和环境部直接公开发布站点数据，不经过科研渠道。**

---

## 一、中国政府开放数据

### 国家级

```
web_search "site:data.gov.cn 气象"
web_search "site:data.gov.cn {变量}"
```

### 省/市级（直接对应用户目标区域）

```
web_search "江苏省 公共数据 开放平台 气象"
web_search "南京市 数据开放 平台 气象 气温"
web_search "{省份} 政务数据 开放 气温 降水"
web_search "南京 统计年鉴 气温 降水 {年份}"
```

### 地方统计局年鉴

```
web_search "南京 统计年鉴 气候 site:tjj.nanjing.gov.cn"
web_search "江苏 统计年鉴 气象 数据"
web_search "南京市 统计局 气温 降水 历年"
```

省会城市统计局每年发布年鉴，内含逐月气温和降水汇总表。1950s 以后的数据往往有电子版。

---

## 二、其他国家政府开放数据（全球互换）

```
web_search "site:data.gov {地点} weather station"
web_search "site:data.gov.uk {地点} climate data"
web_search "site:data.europa.eu {变量} {区域}"
web_search "{国家} meteorological service open data"
```

全球气象站数据通过 WMO 互换，许多国家气象局会公开历史数据。

---

## 三、国际组织数据门户

### WMO (世界气象组织)

```
web_search "site:wmo.int {地点} climate data"
web_search "WMO station {站号} data access"
```

### FAO (联合国粮农组织)

```
web_search "site:fao.org climate data {区域}"
web_search "FAO CLIMWAT {区域}"
```

### World Bank Climate Data

```
web_search "site:climateknowledgeportal.worldbank.org {区域}"
web_search "World Bank climate data {国家} station"
```

---

## 四、领域专用数据库（常被忽略）

这些不列在传统遥感/气象平台清单中，但包含独特的历史站点数据：

| 数据库 | 搜索方式 | 覆盖 |
|--------|---------|------|
| HadISD | `web_search "HadISD station {区域}"` | 全球 7800 站逐小时 |
| ISTI | `web_search "ISTI {区域} temperature station"` | 全球温度基准站 |
| GPCC | `web_search "GPCC precipitation {区域}"` | 全球降水站点分析 |
| APHRODITE | `web_search "APHRODITE precipitation Asia"` | 亚洲高分辨率降水 |
| ISMN | `web_search "ISMN soil moisture {区域}"` | 全球土壤湿度 |
| FluxNet / ChinaFLUX | `web_search "ChinaFLUX {区域} data"` | 通量数据 |

---

## 五、历史档案数据（1950 年前后关键补充）

对于 1950 年代的早期数据：

```
web_search "{地点} 气象站 建站 历史 记录"
web_search "{地点} 1950 年 气温 记录 档案"
web_search "中国 历史 气 候 数据 恢复"
web_search "digitized historical weather records China"
```

1950 年代很多气象记录尚未数字化，但已有团队做了恢复工作。

## 关键原则

- 气象数据在政府开放数据中的优先级很高，不要跳过
- 地方统计局年鉴是常被忽略的宝藏——逐月数据直接以表格形式印刷
- 国际组织数据（WMO/FAO/World Bank）通常比科研数据中心更容易访问
- 1950s 早期数据优先找数字化恢复项目，其次找历史档案
