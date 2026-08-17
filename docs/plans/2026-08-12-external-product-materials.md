# intelifar External Product Materials Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 生成一份面向潜在客户和投资人的中文产品 DOCX 与一份可现场演示的中文 PPTX。

**Architecture:** 两项交付物共用本地产品事实、品牌资产和真实 UI 截图。DOCX 使用 python-docx 与 Word 原生样式生成，PPTX 使用 `@oai/artifact-tool` 生成；两者分别执行逐页渲染和版式验收。

**Tech Stack:** bundled Python, python-docx, LibreOffice renderer, bundled Node.js, @oai/artifact-tool

---

### Task 1: 建立事实与素材清单

**Files:**
- Read: `README.md`
- Read: `docs/INTELIFAR-USER-GUIDE.zh-CN.md`
- Read: `artifacts/e2e-report.md`
- Read: `artifacts/semantica-phase-2/report.md`
- Read: `site/public/brand/intelifar-logo.png`
- Read: `artifacts/**/*.png`

**Steps:**
1. 只保留已实现、已测试或明确标注为建议部署的内容。
2. 记录每项产品主张对应的本地文件证据。
3. 选择不重复使用的关键 UI 截图。

### Task 2: 生成产品文档

**Files:**
- Create: `artifacts/external-product-materials/build-product-brief.py`
- Create: `deliverables/intelifar-企业文档IP智能平台-产品介绍.docx`

**Steps:**
1. 按 `narrative_proposal` 数值规范定义页面、字体、标题、列表、表格和页眉页脚。
2. 生成封面、价值主张、产品链路、模块、技术、治理、客户场景、投资逻辑、验证证据和合作入口。
3. 使用真实 UI 截图并设置替代文本。
4. 使用 `render_docx.py` 渲染全部页面并逐页检查。
5. 修复分页、表格、图片和文字密度问题后重新渲染。

### Task 3: 生成演示文稿

**Files:**
- Create: `artifacts/external-product-materials/build-product-pitch.mjs`
- Create: `deliverables/intelifar-企业文档IP智能平台-客户与投资人演示.pptx`

**Steps:**
1. 初始化 artifact-tool 工作区。
2. 创建 14 页 16:9 演示，使用 intelifar 品牌色和真实产品截图。
3. 为产品事实页添加来源说明和讲者备注。
4. 导出 PPTX、每页 PNG、布局 JSON 和总览图。
5. 运行溢出检测并逐页检查文字换行、图片裁切和对象重叠。
6. 修复后重新导出最终 PPTX。

### Task 4: 最终验收

**Files:**
- Verify: `deliverables/intelifar-企业文档IP智能平台-产品介绍.docx`
- Verify: `deliverables/intelifar-企业文档IP智能平台-客户与投资人演示.pptx`

**Steps:**
1. 检查文件可打开、大小合理、无占位文本和内部提示词。
2. 核对品牌名始终为 `intelifar`。
3. 核对所有能力边界与当前产品一致。
4. 仅交付最终 DOCX 与 PPTX。
