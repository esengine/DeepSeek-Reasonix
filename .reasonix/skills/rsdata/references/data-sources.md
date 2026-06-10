# 数据源矩阵 (A~M)

> 此文件从 rsdata 主 SKILL.md 拆分。更新数据源时只改这里，不动主文件。

拿到用户需求后，先判断匹配 A~M 中哪些类别，然后并行搜索。

---

## A. 卫星光学影像

| 平台 | site: 限定 | 覆盖 |
|------|-----------|------|
| NASA EarthData | `site:earthdata.nasa.gov` | Landsat, MODIS, VIIRS, ASTER |
| Copernicus Data Space | `site:dataspace.copernicus.eu` | Sentinel-1/2/3/5P |
| USGS EarthExplorer | `site:earthexplorer.usgs.gov` | Landsat, 历史影像 |
| LAADS DAAC | `site:ladsweb.modaps.eosdis.nasa.gov` | MODIS, VIIRS |
| GLoVIS | `site:glovis.usgs.gov` | Landsat, Sentinel-2 |
| 中国资源卫星应用中心 | `site:cresda.com` | GF-1~7, 资源系列 |
| 国家对地观测科学数据中心 | `site:noda.ac.cn` | 国产卫星 |
| 地理空间数据云 | `site:gscloud.cn` | Landsat, DEM, MODIS 国内镜像 |
| PIE-Engine | `site:pie-engine.com` | 航天宏图 |
| Planet | `site:planet.com` | 商业高分辨率 |
| Maxar | `site:maxar.com` | WorldView/GeoEye |
| CBERS | `site:cbers.inpe.br` | 中巴资源卫星 |
| JAXA G-Portal | `site:gportal.jaxa.jp` | ALOS, GCOM |

## B. SAR / InSAR

| 平台 | site: 限定 | 覆盖 |
|------|-----------|------|
| ASF DAAC | `site:asf.alaska.edu` | Sentinel-1, ALOS PALSAR |
| Copernicus | `site:dataspace.copernicus.eu` | Sentinel-1 |
| JAXA G-Portal | `site:gportal.jaxa.jp` | ALOS-2 PALSAR-2 |
| CRESDA | `site:cresda.com` | GF-3 (C波段) |
| NODA | `site:noda.ac.cn` | 陆探一号 LT-1, 海丝一号 |
| ESA Earth Online | `site:earth.esa.int` | ERS-1/2, Envisat ASAR |
| DLR | `site:dlr.de` | TerraSAR-X, TanDEM-X |

## C. 高程 / DEM

USGS EarthExplorer (SRTM/NASADEM) / JAXA (ALOS AW3D30) / Copernicus (GLO-30) / DLR (TanDEM-X) / OpenTopography

## D. 气象 / 气候 / 再分析

| 平台 | site: 限定 | 覆盖 |
|------|-----------|------|
| ECMWF / ERA5 | `site:cds.climate.copernicus.eu` | ERA5, ERA5-Land |
| NOAA NCEI | `site:ncei.noaa.gov` | GHCN, GSOD |
| NOAA PSL | `site:psl.noaa.gov` | NCEP/NCAR, CFSR, 20CR |
| CRU | `site:crudata.uea.ac.uk` | CRU TS |
| WorldClim | `site:worldclim.org` | 历史+未来气候 |
| CHIRPS | `site:chc.ucsb.edu` | 降水 |
| CMIP6 | `site:esgf-node.llnl.gov` | 气候模式 |
| ISIMIP | `site:isimip.org` | 影响模型 |
| CMA / CRA-40 | `site:data.cma.cn` | 中国再分析+站点 |
| 国家青藏高原科学数据中心 | `site:data.tpdc.ac.cn` | CMFD, 青藏高原+全球变化 |
| JRA-55 | `site:jra.kishou.go.jp` | 日本再分析 (1958–至今) |
| HadISD | `site:metoffice.gov.uk` | 全球 7800 站逐小时 |
| GPCC | `site:gpcc.dwd.de` | 全球降水站点分析 |
| APHRODITE | 直接搜 `APHRODITE precipitation Asia` | 亚洲高分辨率降水 |

## E. 大气成分

GES DISC / CAMS / Sentinel-5P / TROPOMI / OMI / TEMPO

## F. 海洋

CMEMS / AVISO / NOAA CoastWatch / HYCOM / Argo / NSOAS

## G. 陆地/生态/水文

LP DAAC / ORNL DAAC / NSIDC / GLAD / ESA CCI / GLEAM / GRACE / resdc.cn / geodata.cn

## H. 夜光遥感

NOAA NGDC / EOG VIIRS / 珞珈一号 / SDGSAT-1

## I. 科研数据仓库 + 数据期刊

| 平台 | 搜索方式 | 特点 |
|------|---------|------|
| Zenodo | `site:zenodo.org` | 通用科研数据 |
| Figshare | `site:figshare.com` | 通用 |
| Dryad | `site:datadryad.org` | 生态/环境 |
| PANGAEA | `site:pangaea.de` | 地学/海洋 |
| Mendeley Data | `site:data.mendeley.com` | 通用 |
| Science Data Bank | `site:scidb.cn` | 中国科学数据 |

### 数据期刊

```
Scientific Data (Nature)    → site:nature.com/sdata
ESSD                        → site:earth-system-science-data.net
Geoscience Data Journal     → site:rmets.onlinelibrary.wiley.com
Data in Brief (Elsevier)    → 直接搜 "Data in Brief" {变量}
Big Earth Data (T&F)        → site:bigearthdata.com
Data (MDPI)                 → site:mdpi.com/journal/data
中国科学数据                  → site:csdata.org
全球变化数据学报              → site:geodoi.ac.cn
```

## J. AI/ML 平台

HuggingFace / 百度AI Studio / Kaggle / Radiant MLHub / TorchGeo / OpenDataLab / 和鲸社区 / 阿里云天池 / AWS Open Data / Planetary Computer

## K. GitHub 工具仓库

多个 query 并行搜索：

```
"{数据变量} dataset download tool github"
"{数据集名称} download script github"
"awesome {主题} github"
"awesome remote sensing {主题} github"
"data.cma.cn python download"
"ghcn {站点号} github"
"{变量} {平台} python download"
"{数据集名} tutorial notebook jupyter"
```

## L. 非正式渠道

百度网盘 / 阿里云盘 / 夸克网盘 / CSDN / 知乎 / 博客园。
标注"链接可能失效，建议优先使用官方源"。

## M. AI 搜索增强（多轮迭代）

标准/深入模式下至少 3 轮：

```
Round 1 (宽泛): "有哪些 {数据类型} 公开数据集 2024 2025"
              "best {数据类型} dataset for {应用场景}"
Round 2 (具体): 针对 Round 1 发现的新平台深入搜
Round 3 (否定): "为什么 {数据集} 不适合 {应用场景}"
              "{数据集A} vs {数据集B} accuracy {区域}"
Round 4 (最新): "{变量} dataset 2025"
```
