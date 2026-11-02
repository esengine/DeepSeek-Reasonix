## 新增

- Studio 按需渲染 Mermaid 图表，支持源代码／预览切换、复制、语法错误提示和亮暗主题；流式尾块完成前保留源代码。#13748
- 图表使用受 CSP 限制的渲染器和净化的静态 SVG 图片，不执行回调、不打开图表链接、不加载远程图片。#13748
- Mermaid 围栏的通用下载、行号和折叠控件改为预览／源代码和复制；其他代码围栏保持原样。#13748

---
<!-- lang:en -->

## New

- Studio lazily renders Mermaid fences with source/preview, source copying, explicit syntax errors and light/dark themes. Streaming tails remain source until settled. #13748
- CSP-confined rendering produces sanitized inert SVG images, without graph callbacks, navigation or remote images. #13748
- Mermaid fences replace generic code-download, line-gutter and fold controls with Preview/Source and Copy source; other fences are unchanged. #13748
