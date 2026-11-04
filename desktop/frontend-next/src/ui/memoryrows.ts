import { t } from "../i18n";
import { bytes } from "../i18n/format";
import type { FeedbackProcessMemory, FeedbackProcessType } from "../port/feedback";

const NAME: Record<FeedbackProcessType, string> = {
  browser: "主进程",
  renderer: "界面进程",
  gpu: "图形进程",
  utility: "辅助进程",
  kernel: "内核",
};

const ORDER: FeedbackProcessType[] = ["renderer", "browser", "gpu", "utility", "kernel"];

// The rows a report will carry, the largest working set of each type first in
// the order a person would look for them, each said as a number they can read.
export function memoryLines(rows: FeedbackProcessMemory[]): string[] {
  return [...rows]
    .sort((a, b) => ORDER.indexOf(a.process) - ORDER.indexOf(b.process) || b.workingSetKB - a.workingSetKB)
    .map((r) => t("memory.name_set_private_own", { name: t(NAME[r.process]), set: bytes(r.workingSetKB * 1024), own: bytes(r.privateKB * 1024) }));
}
