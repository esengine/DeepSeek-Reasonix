import { t } from "../i18n";
import { bytes } from "../i18n/format";
import type { FeedbackProcessMemory, FeedbackProcessType } from "../port/feedback";

const NAME: Record<FeedbackProcessType, string> = {
  browser: "memoryrows.mainProcess",
  renderer: "memoryrows.interfaceProcess",
  gpu: "memoryrows.graphicsProcess",
  utility: "memoryrows.helperProcess",
  kernel: "memoryrows.kernel",
};

const ORDER: FeedbackProcessType[] = ["renderer", "browser", "gpu", "utility", "kernel"];

// The rows a report will carry, the largest working set of each type first in
// the order a person would look for them, each said as a number they can read.
export function memoryLines(rows: FeedbackProcessMemory[]): string[] {
  return [...rows]
    .sort((a, b) => ORDER.indexOf(a.process) - ORDER.indexOf(b.process) || b.workingSetKB - a.workingSetKB)
    .map((r) => t("memoryrows.private", { name: t(NAME[r.process]), set: bytes(r.workingSetKB * 1024), own: bytes(r.privateKB * 1024) }));
}
