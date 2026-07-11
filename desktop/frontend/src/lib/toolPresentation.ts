// ToolCard 的展示决策适配层。把"工具名 + 参数 + 后端 fileDiff + 运行状态"汇总成
// ToolCard 只需渲染的 ToolCardPresentation：detail 描述"渲染什么内容"，defaultOpen
// 描述"是否默认展开"。ToolCard 不再自行维护工具白名单或 diff 推导规则。
//
// 设计原则：
// - 写工具（write/edit/move/delete/notebook）始终默认展开，让用户看到改动结果，
//   即使后端尚未提供结构化 diff；非写工具默认折叠，避免 transcript 噪音。
// - detail 优先使用后端 fileDiff（可信的真实前后差异），其次从 args 推导兼容性 diff
//   （仅 edit_file/write_file/multi_edit），都没有时为 none，由 ToolCard 渲染 args/output。

import { diffsFor, type ToolDiff, type ToolFileDiff } from "./tools";
import type { ToolStatus } from "./useController";

// 写工具集合：这些工具会修改工作区，始终默认展开让用户看到改动。
// 集中在此单一位置维护，避免 ToolCard 与 diffsFor 各持一份分散的工具知识。
const WRITE_TOOLS = new Set([
  "write_file",
  "edit_file",
  "multi_edit",
  "move_file",
  "delete_range",
  "delete_symbol",
  "notebook_edit",
]);

export interface ToolPresentationInput {
  name: string;
  /** effectiveArgs：已考虑 archivedWithoutFullData 后用于推导/渲染的参数 */
  args: string;
  /** 后端提供的结构化整文件 diff，可信度最高 */
  fileDiff?: ToolFileDiff;
  /** dataArchived 且尚未懒加载到 fullData 时为 true，此时不推导 args diff */
  archivedWithoutFullData: boolean;
  /** 是否有嵌套子代理调用 */
  hasNested: boolean;
  status: ToolStatus;
}

export type ToolCardDetail =
  | { kind: "unified-diff"; preview: ToolFileDiff }
  | { kind: "inline-diffs"; diffs: ToolDiff[] }
  | { kind: "rename"; srcPath: string; dstPath: string }
  | { kind: "none" };

export interface ToolCardPresentation {
  detail: ToolCardDetail;
  defaultOpen: boolean;
}

export function toolPresentation(input: ToolPresentationInput): ToolCardPresentation {
  const { name, args, fileDiff, archivedWithoutFullData, hasNested, status } = input;

  // 1. 优先使用后端结构化 fileDiff（真实前后差异，支持所有写工具）
  const hasFileDiff = Boolean(fileDiff?.diff && fileDiff.diff.trim());
  // 2. 无 fileDiff 时，从 args 推导兼容性 diff（仅 edit_file/write_file/multi_edit）；
  //    archivedWithoutFullData 时 args 为空，不推导
  const argsDiffs = archivedWithoutFullData ? [] : diffsFor(name, args);

  let detail: ToolCardDetail;
  if (fileDiff?.kind === "rename") {
    // rename 的 diff 为空，但 kind="rename" 标记了这是一次重命名，
    // 渲染 "src → dst" 卡片而非 unified-diff 或 none。
    detail = { kind: "rename", srcPath: fileDiff.srcPath ?? "", dstPath: fileDiff.dstPath ?? "" };
  } else if (hasFileDiff && fileDiff) {
    detail = { kind: "unified-diff", preview: fileDiff };
  } else if (argsDiffs.length > 0) {
    detail = { kind: "inline-diffs", diffs: argsDiffs };
  } else {
    detail = { kind: "none" };
  }

  // 写工具始终默认展开（让用户看到改动）；非写工具仅在有嵌套子代理运行时展开
  const isWriteTool = WRITE_TOOLS.has(name);
  const defaultOpen = isWriteTool || (hasNested && status === "running");

  return { detail, defaultOpen };
}
