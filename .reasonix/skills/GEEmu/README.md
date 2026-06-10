<p align="center">
  <img src="assets/geemu-banner-readme.png" alt="GEEMu banner">
</p>

<p align="center">
  <img src="assets/geemu-mark-128.png" alt="GEEMu logo" width="96">
</p>

<h1 align="center">GEEMu Skill</h1>

**Google Earth Engine Master Utility (GEEMu)**.



Google Earth Engine Master Utility (GEEMu) is a compact full skill folder for OpenAI Codex and Claude Code. It helps
write Google Earth Engine (GEE) Python workflows with geemap. It is designed for
research-grade remote sensing tasks where the assistant must first check the
local environment, understand the study design, reason about data and
boundaries, then write reproducible GEE Python code.

GEEMu 是一个面向 Google Earth Engine、Earth Engine Python API 和 geemap 的
skill 文件夹，可用于 OpenAI Codex 和 Claude Code。它不是单纯的代码片段库，
而是一个研究流程助手：先检查本地环境和认证，再理解研究区、分辨率、数据层、
边界复杂度和输出目标，最后写出可以运行、可以复现、可以记录的 GEE Python
代码。

## Compatibility / 兼容性

- OpenAI Codex: install GEEMu as a local skill folder, then ask Codex to use
  GEEMu for GEE/geemap tasks.
- Claude Code: install the same folder under the Claude skills directory, then
  ask Claude Code to use GEEMu.
- The skill is file-based. It does not require a separate vector-search server,
  embedding model download, private credentials, or the original development
  workspace.

GEEMu 的核心是 `SKILL.md` 加参考资料、模板、示例和本地知识库，所以同一份
GitHub 包可以被 Codex 和 Claude Code 读取。两边的区别主要是安装目录不同。

## Installation / 安装教程

GitHub repository:

`https://github.com/sadassimov/geemu-skill`

> **Install the complete folder — do not skip any files.** The skill depends on
> the whole package, especially the local knowledge base
> `gee_vector_db/chunks.jsonl` and `gee_vector_db/documents.jsonl` (tens of MB):
> these JSONL files are what the retrieval workflow searches, so the skill will
> not work without them. `git clone` and GitHub's "Download ZIP" already include
> every file — just don't hand-pick files, exclude large/`.jsonl` files, or use a
> shallow/sparse/partial checkout.
>
> **必须完整安装,不要遗漏任何文件。** skill 依赖整个包,尤其是本地知识库
> `gee_vector_db/chunks.jsonl` 和 `gee_vector_db/documents.jsonl`(几十 MB):
> 检索就是搜索这两个 JSONL 文件,缺了它们 skill 无法工作。`git clone` 和 GitHub
> 的 “Download ZIP” 本来就包含所有文件——只要不要手动挑选文件、不要排除大文件或
> `.jsonl`、也不要用 shallow/sparse/partial checkout 即可。

### Option A: Clone From GitHub / 从 GitHub 克隆

For OpenAI Codex on Windows PowerShell:

```powershell
git clone https://github.com/sadassimov/geemu-skill.git "$env:USERPROFILE\.codex\skills\GEEMu"
```

For Claude Code on Windows PowerShell:

```powershell
git clone https://github.com/sadassimov/geemu-skill.git "$env:USERPROFILE\.claude\skills\GEEMu"
```

For OpenAI Codex on macOS/Linux:

```bash
git clone https://github.com/sadassimov/geemu-skill.git ~/.codex/skills/GEEMu
```

For Claude Code on macOS/Linux:

```bash
git clone https://github.com/sadassimov/geemu-skill.git ~/.claude/skills/GEEMu
```

### Option B: Download ZIP / 下载 ZIP

1. Open `https://github.com/sadassimov/geemu-skill`.
2. Download the repository as a ZIP file.
3. Unzip it and rename the folder to `GEEMu`.
4. Put the folder here for Codex: `~/.codex/skills/GEEMu`.
5. Put the folder here for Claude Code: `~/.claude/skills/GEEMu`.

Windows users can use these equivalent locations:

- Codex: `%USERPROFILE%\.codex\skills\GEEMu`
- Claude Code: `%USERPROFILE%\.claude\skills\GEEMu`

## Architecture / 架构

GEEMu uses a layered design:

- `SKILL.md`: the main instruction layer. It tells the assistant how to start
  every GEE task, including project ID, authentication, geemap import, proxy
  reminders, research design, data-layer reasoning, boundary complexity, and run
  records.
- `references/`: focused reference layers for local environment checks, proxy
  setup, research design, data selection, administrative/vector boundaries, and
  export strategy.
- `gee_vector_db/`: a portable local knowledge database stored as JSONL text.
  It is searched by lightweight keyword scoring.
- `awesome-gee-community-datasets/community_datasets.csv`: a structured
  community dataset table used as one data-recommendation source.
- `templates/`: standard `RUN.md` and `DATA_LAYER.md` records for documenting
  parameter choices, data transforms, scale, region, export settings, and
  assumptions.
- `examples/`: complete reference tasks, including Sentinel-2 NDVI, tiled export,
  geemap local download, community dataset recommendation, and NSW dNBR/NBR
  recovery.
- `scripts/check_local_environment.py`: a deterministic checker for local
  Python, geemap, Earth Engine credentials, project ID, and geemap vector helper
  availability.

GEEMu 的核心架构可以理解为：

- 主指令层：规定每次 GEE 任务必须先检查 project ID、认证、geemap、代理和
  研究设计。
- 参考层：把环境、代理、数据层、边界层、导出策略等细节拆到 `references/`。
- 本地知识层：把本地经验和参考资料清洗成 `chunks.jsonl` / `documents.jsonl`，
  用关键词检索.
- 数据建议层：使用社区数据表和本地知识库辅助选择 GEE 数据集。
- 记录层：每次代码任务都应留下 `RUN.md` 和 `DATA_LAYER.md`，说明研究区、
  数据变换、分辨率、阈值、导出参数和关键假设。

## Workflow / 工作流

When a user asks GEEMu to write GEE code, the expected flow is:

1. Confirm or request the Google Cloud Project ID.
2. Check whether `ee`, `geemap`, credentials, and vector helpers are available.
3. Remind users in restricted networks to configure proxy environment variables
   before online GEE access.
4. Define the research question, study area, time windows, calculation scale,
   output products, and export destination.
5. Choose boundary data deliberately: administrative boundary, uploaded vector,
   local vector converted by geemap, or drawn geometry.
6. Assess boundary complexity and decide whether simplification, buffering,
   clipping, or tiled export is needed.
7. Choose data layers and transformations, then explain what each value or band
   represents before scaling, masking, thresholding, or exporting.
8. Write GEE Python code using geemap where appropriate.
9. Record the run and data layer decisions in Markdown.

GEEMu 的工作流不是“直接写代码”，而是先把研究问题落地：研究区是什么、边界
代表什么属性、火前火后的时间窗是否合理、分辨率是否足够、导出范围是否过大、
是否需要分块、用哪个数据源、每一步数据变换代表什么含义。这样可以减少
GEE 里常见的尺度、边界、掩膜和导出错误。

## Prompt Examples / Prompt 示例

Use prompts like these with Codex or Claude Code after installing the skill.

可以直接用类似下面的 prompt 触发 GEEMu。

### California Wildfire Recovery / 加州大火恢复监测

> Use GEEMu to write a geemap-based Earth Engine Python workflow for the
> California wildfire region in the United States. First check the local Python
> environment, Earth Engine authentication, geemap availability, proxy reminder,
> and Google Cloud Project ID. Use GADM administrative boundaries to define the
> California study area, then narrow the analysis to the selected burned region
> or fire perimeter if one is provided. Use a pre-fire 3-year median NBR as the
> baseline, then evaluate post-fire recovery for 1 year after the fire. Compute
> dNBR to detect burned areas, then compute an NBR recovery ratio map relative to
> the baseline and post-fire condition. Use an appropriate scale for regional
> analysis, consider boundary complexity and whether tiled export is needed,
> export the recovery map, and write RUN.md plus DATA_LAYER.md documenting all
> choices.

> 用 GEEMu 帮我写一个 geemap + Earth Engine Python 工作流，研究美国加州大火
> 区域的植被恢复。开头先检查本地 Python 环境、Earth Engine 认证、geemap
> 是否安装、是否需要代理，以及 Google Cloud Project ID。研究区先用 GADM
> 行政边界提取 California，再根据指定火场或火烧边界缩小分析范围。用火前
> 3 年 median NBR 作为 baseline，火后 1 年做恢复评估。先用 dNBR 检测火烧区，
> 再用 NBR 恢复比例表达恢复情况，导出恢复比例图。请考虑合适分辨率、边界
> 复杂度、是否需要分块导出，并写 RUN.md 和 DATA_LAYER.md 记录参数、数据层
> 和变换逻辑。

### Sentinel-2 NDVI / Sentinel-2 植被指数

> Use GEEMu to create a Sentinel-2 NDVI workflow for a user-provided study area.
> Check authentication and project ID first, choose a cloud-masking strategy,
> explain the scale and band transformations, make a median composite, export the
> NDVI raster, and record the run decisions.

> 用 GEEMu 写一个 Sentinel-2 NDVI 任务。先检查认证和 project ID，再根据研究区
> 选择云掩膜策略，说明分辨率和波段变换，生成 median composite，导出 NDVI
> 栅格，并记录这次运行的参数。

### Tiled Export / 分块导出

> Use GEEMu to design a tiled export workflow for a large or complex boundary.
> Assess geometry complexity, recommend simplification or tiling, preserve the
> meaning of the boundary, and generate Earth Engine Python code that exports
> manageable tiles with documented scale and CRS choices.

> 用 GEEMu 给一个大范围复杂边界设计分块导出流程。请评估边界复杂度，判断
> 是否需要简化、buffer、clip 或 tile，保持边界属性含义不变，生成可管理的
> Earth Engine Python 导出代码，并记录 scale 和 CRS 选择。

### geemap Local Download / geemap 直接导出到本地

> Use GEEMu to write a geemap workflow that downloads ESA WorldCover 2020
> directly from Earth Engine to a local GeoTIFF. Check the local environment,
> authentication, proxy needs, and Google Cloud Project ID first. Let the user
> provide either an uploaded ROI asset or a simple lon/lat bbox. Use
> `ESA/WorldCover/v100`, select the `Map` band, preserve categorical class
> values with nearest-neighbor resampling, choose a safe scale and dtype, and
> explain when direct local download should be replaced by tiled or Drive export.
> Write RUN.md and DATA_LAYER.md documenting the ROI, output path, class data,
> scale, CRS, and risks.

> 用 GEEMu 写一个 geemap 直接从 Earth Engine 下载到本地 GeoTIFF 的流程。先检查
> 本地环境、认证、代理需求和 Google Cloud Project ID。研究区可以让用户提供
> 已上传的 ROI asset，也可以用简单经纬度 bbox。使用 `ESA/WorldCover/v100`，
> 选择 `Map` 波段，土地覆盖类别要用 nearest-neighbor，避免类别值被插值破坏。
> 请说明什么时候适合直接本地下载，什么时候应该改成分块导出或 Drive 导出，
> 并写 RUN.md 和 DATA_LAYER.md 记录 ROI、输出路径、类别数据、scale、CRS 和风险。

### Community Dataset Recommendation / 社区数据推荐

> Use GEEMu to recommend candidate GEE datasets for a drought, climate, fire, or
> vegetation-monitoring task. Search the local knowledge database and the
> community dataset table, compare spatial scale, temporal coverage, variables,
> limitations, and export suitability before writing code.

> 用 GEEMu 帮我为干旱、气候、火灾或植被监测任务推荐 GEE 数据集。请检索本地
> 知识库和社区数据表，比较空间分辨率、时间覆盖、变量含义、局限性和导出
> 可行性，再决定是否写代码。

## License & Notices / 许可与声明

GEEMu's own files (`SKILL.md`, `references/`, `templates/`, `examples/`,
`scripts/`, and this README) are released under the MIT License; see `LICENSE`.

Bundled third-party material keeps its original terms: the Awesome GEE Community
Datasets table (CC BY 4.0), Earth Engine documentation excerpts (CC BY 4.0 /
Apache 2.0), geemap (MIT), and other sources represented in the local knowledge
database. Review `THIRD_PARTY_NOTICES.md` before reuse or redistribution.

GEEMu 自有文件按 MIT 许可发布(见 `LICENSE`)。打包的第三方内容仍遵循各自的
原始许可,详见 `THIRD_PARTY_NOTICES.md`。
