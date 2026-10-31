import type { HistoryMessage } from "../src/port/session";
import pdfText from "./media/delivery.pdf?raw";

export const deliveryFiles = [
  { path: "方案.md", description: "Markdown 方案文档，包含正文、表格和指向其他示例文件的链接。" },
  { path: "数据.csv", description: "CSV 数据示例，含表头、多行字段、引号转义、公式样式和空字段。" },
  { path: "报告.html", description: "静态 HTML 报告页面，可用于对比静态预览与本地交互运行效果。" },
  { path: "示例.pdf", description: "PDF 示例文件，可测试分页、缩放和文字选择等预览能力。" },
  { path: "docs/reports/总结.md", description: "嵌套目录中的总结文档，可通过右键菜单在文件树中定位。" },
];
export const deliveryContents: Record<string, string> = {
  "方案.md": "# 文件交付与预览体验\n\n点击聊天中的交付卡片，可在右侧打开对应文件。\n\n| 示例文件 | 体验内容 |\n| --- | --- |\n| 方案.md | Markdown 正文与表格 |\n| 数据.csv | 表格与源码 |\n| 报告.html | 静态 HTML |\n| 示例.pdf | 分页与文字选择 |",
  "数据.csv": '编号,内容\n001,"第一行\n第二行"\n002,=SUM(A1:A2)',
  "报告.html": "<!doctype html><meta charset='utf-8'><h1>交付文件可以直接阅读</h1><p>文件交付预览示例。</p>",
  "示例.pdf": pdfText,
  "docs/reports/总结.md": "# 交付总结\n\n这份文档位于 docs/reports 中。右键交付卡片并选择“在文件树中显示”，可展开父目录并选中此文件。",
};
export function deliveryHistory(): HistoryMessage[] {
  return [
    { role: "user", content: "请展示这些文件，方便我直接预览并在文件树中定位。", msgIndex: 0 },
    { role: "assistant", content: "", toolCalls: [{ id: "delivery", name: "present_files" }] },
    { role: "tool", content: "Presented files", toolCallId: "delivery", toolName: "present_files", deliveries: deliveryFiles },
    { role: "assistant", content: "文件已准备好。点击卡片在右侧预览；右键“总结.md”并选择“在文件树中显示”，可定位到 docs/reports 中的文件。" },
  ];
}
