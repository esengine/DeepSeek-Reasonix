import { monogram } from "./modelmenu";
import { hasVendorMark, VendorMark } from "./vendormarks";

/** The vendor's artwork when a preset declared one, else the letter chip. */
export function ServiceMark({ mark, label, id }: { mark?: string; label: string; id: string }) {
  if (hasVendorMark(mark)) return <VendorMark slug={mark} size={20} />;
  const { letter, tone } = monogram(label, id);
  return <i className="mi-mono" data-tone={tone} style={{ "--tone": `var(--cat-${tone + 1})`, width: 20, height: 20 } as React.CSSProperties} aria-hidden="true">{letter}</i>;
}
