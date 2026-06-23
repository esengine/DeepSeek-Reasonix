import { memo, useRef, useState } from "react";
import { ChevronRight } from "lucide-react";
import { useT } from "../lib/i18n";
import { useGSAPCollapse } from "../lib/useGSAPCollapse";
import type { Item } from "../lib/useController";
import { ToolCard } from "./ToolCard";

type ToolItem = Extract<Item, { kind: "tool" }>;

type ReadOnlyBatchProps = {
  items: ToolItem[];
  subcalls: ReadonlyMap<string, ToolItem[]>;
  tabId?: string;
};

export const ReadOnlyBatch = memo(function ReadOnlyBatch({ items, subcalls, tabId }: ReadOnlyBatchProps) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const bodyRef = useRef<HTMLDivElement>(null);
  useGSAPCollapse(bodyRef, open);

  const readCount = items.filter((it) => it.name === "read_file" || it.name === "ls").length;
  const searchCount = items.filter((it) => it.name === "grep" || it.name === "glob" || it.name === "web_fetch").length;

  const parts: string[] = [];
  if (readCount > 0) parts.push(t("tool.readCount", { n: readCount }));
  if (searchCount > 0) parts.push(t("tool.searchCount", { n: searchCount }));
  const otherCount = items.length - readCount - searchCount;
  if (otherCount > 0) parts.push(t("tool.otherReadCount", { n: otherCount }));
  const label = parts.join(" · ");

  if (!label || items.length === 0) return null;

  return (
    <div className={`readonly-batch${open ? " readonly-batch--open" : ""}`} data-entrance={items[0]?.id}>
      <button type="button" className="reasoning__head" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        <ChevronRight className={`reasoning__chevron${open ? " reasoning__chevron--open" : ""}`} size={12} />
        <span className="readonly-batch__label" data-creation-label={t("creation.toolCallsLabel")}>{label}</span>
      </button>
      <div ref={bodyRef} className="readonly-batch__body">
        {items.map((it) => (
          <ToolCard key={it.id} item={it} subcalls={subcalls.get(it.id)} tabId={tabId} />
        ))}
      </div>
    </div>
  );
});
