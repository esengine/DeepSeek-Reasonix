import { ManualBrowserPanel } from "./BrowserPanel";

/** The browser pages the workbench draws itself, one panel each, only the one in front shown. */
export function ManualBrowsers({ ids, active, onAddress, onExternal, scheme }: {
  ids: string[]; active: string; onAddress: (id: string, host: string) => void; onExternal: (url: string) => void; scheme: "light" | "dark";
}) {
  return <>{ids.map((id) => <ManualBrowserPanel key={id} id={id} hidden={active !== id} onAddress={onAddress} onExternal={onExternal} scheme={scheme} />)}</>;
}
