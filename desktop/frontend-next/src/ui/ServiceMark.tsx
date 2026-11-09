import { StudioIcon } from "./StudioIcon";
import { providerMark } from "./modelmenu";
import { hasVendorMark, VendorMark } from "./vendormarks";

/** Declared vendor artwork or a library service icon. */
export function ServiceMark({ mark, id }: { mark?: string; id: string }) {
  if (hasVendorMark(mark)) return <VendorMark slug={mark} size={20} />;
  const { tone } = providerMark(id);
  return <i className="mi-mono" data-tone={tone} style={{ "--tone": `var(--cat-${tone + 1})`, width: 20, height: 20 } as React.CSSProperties} aria-hidden="true"><StudioIcon name="server" /></i>;
}
