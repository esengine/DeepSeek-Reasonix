// The approval modes a person can give an agent, in the order they widen. An
// empty value follows the window's own mode.
export const PERMISSIONS: [string, string][] = [
  ["", "跟随窗口"],
  ["readOnly", "只读"],
  ["ask", "逐项询问"],
  ["auto", "自动"],
  ["yolo", "完全放行"],
];
