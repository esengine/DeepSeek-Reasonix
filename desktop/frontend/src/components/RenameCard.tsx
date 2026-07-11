import { ArrowRight } from "lucide-react";

// RenameCard 渲染一次重命名的 "src → dst" 单行卡片。ToolCard 与 ApprovalModal 共用，
// 避免两处 JSX 漂移。className 供调用方叠加上下文样式（如审批弹窗的紧凑间距）。
export function RenameCard({ srcPath, dstPath, className }: { srcPath: string; dstPath: string; className?: string }) {
  return (
    <div className={`tool__rename${className ? ` ${className}` : ""}`}>
      <span className="tool__rename-path tool__rename-src">{srcPath}</span>
      <ArrowRight size={14} className="tool__rename-arrow" aria-hidden="true" />
      <span className="tool__rename-path tool__rename-dst">{dstPath}</span>
    </div>
  );
}
