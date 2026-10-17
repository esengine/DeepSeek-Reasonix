import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { t } from "../i18n";
import type { HubPort } from "../port/hub";
import type { RemoteHost } from "../port/remote";
import type { Adder } from "./addws";
import { RemoteDirs } from "./RemoteDirs";
import { StudioIcon } from "./StudioIcon";
import { useModalFocus } from "./useModalFocus";

export function OpenProject({ hub, adder, hosts, onOpen, onClose, onError }: {
  hub: HubPort;
  adder: Adder;
  hosts: RemoteHost[];
  onOpen: (host: string, root: string) => Promise<void>;
  onClose: () => void;
  onError: (error: unknown) => void;
}) {
  const sheet = useRef<HTMLDivElement>(null);
  const [remote, setRemote] = useState<RemoteHost | null>(null);
  useModalFocus(sheet, !remote);
  useEffect(() => {
    if (remote) return;
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") onClose(); };
    window.addEventListener("keydown", escape);
    return () => window.removeEventListener("keydown", escape);
  }, [onClose, remote]);
  useEffect(() => { sheet.current?.querySelector<HTMLButtonElement>("button")?.focus(); }, [remote]);

  if (remote) return <RemoteDirs hub={hub} host={remote.name} start={remote.workspace}
    onClose={onClose} onPick={(root) => {
      onClose();
      void onOpen(remote.name, root).catch(onError);
    }} />;

  return createPortal(
    <div className="pickveil" role="dialog" aria-modal="true" aria-label={t("打开项目")} ref={sheet}>
      <div className="pickcard">
        <h2>{t("打开项目")}</h2>
        <div className="picklist project-locations">
          <button className="pickrow" data-action="workspace.add" onClick={() => { onClose(); adder.add(); }}>
            <StudioIcon name="folder" /><span>{t("选择本机文件夹")}</span>
          </button>
          {hosts.map((host) => <button key={host.name} className="pickrow" data-action="remote.open"
            data-target={host.name} aria-label={host.name + " · SSH"} onClick={() => setRemote(host)}>
            <StudioIcon name="server" /><span>{host.name}</span><small>SSH</small>
          </button>)}
        </div>
        <div className="pickact"><button data-action="layer.dismiss" onClick={onClose}>{t("取消")}</button></div>
      </div>
    </div>, document.body,
  );
}
