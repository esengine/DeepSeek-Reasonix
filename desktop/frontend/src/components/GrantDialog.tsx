import { useState, useCallback } from "react";
import { X, Plus } from "lucide-react";
import { useT } from "../lib/i18n";
import type { CapabilityGrantView } from "../lib/types";

interface GrantDialogProps {
  grant?: CapabilityGrantView;
  onSave: (grant: Omit<CapabilityGrantView, "index" | "source">, source: string) => void;
  onClose: () => void;
  busy: boolean;
  error?: string | null;
}

type PathEntry = { identity: string; path: string; kind: string };
type DeviceEntry = { path: string; kind: string; major: number; minor: number };

export function GrantDialog({ grant, onSave, onClose, busy, error }: GrantDialogProps) {
  const t = useT();
  const [source, setSource] = useState(grant?.source ?? "project");
  const [executable, setExecutable] = useState(grant?.canonicalExecutable ?? "");
  const [argvPrefix, setArgvPrefix] = useState<string[]>(grant?.argvPrefix ?? []);
  const [prefixDraft, setPrefixDraft] = useState("");
  const [network, setNetwork] = useState(grant?.network ?? false);
  const [background, setBackground] = useState(grant?.background ?? false);
  const [preserveBg, setPreserveBg] = useState(grant?.preserveBackgroundProcesses ?? false);
  const [reads, setReads] = useState<PathEntry[]>(grant?.reads ?? []);
  const [writes, setWrites] = useState<PathEntry[]>(grant?.writes ?? []);
  const [devices, setDevices] = useState<DeviceEntry[]>(grant?.devices ?? []);

  const addPrefix = useCallback(() => {
    const p = prefixDraft.trim();
    if (p && !argvPrefix.includes(p)) {
      setArgvPrefix([...argvPrefix, p]);
      setPrefixDraft("");
    }
  }, [prefixDraft, argvPrefix]);

  const handleSave = useCallback(() => {
    const view: Omit<CapabilityGrantView, "index" | "source"> = {
      canonicalExecutable: executable.trim(),
      argvPrefix,
      network,
      background,
      preserveBackgroundProcesses: preserveBg,
      reads: reads.map((r) => ({ identity: r.identity, path: r.path, kind: r.kind })),
      writes: writes.map((w) => ({ identity: w.identity, path: w.path, kind: w.kind })),
      devices: devices.map((d) => ({ path: d.path, kind: d.kind, major: 0, minor: 0 })),
    };
    onSave(view, source);
  }, [executable, argvPrefix, network, background, preserveBg, reads, writes, devices, source, onSave]);

  const addPath = (list: PathEntry[], setList: (v: PathEntry[]) => void) => {
    setList([...list, { identity: "canonical_absolute", path: "", kind: "directory" }]);
  };
  const updatePath = (list: PathEntry[], setList: (v: PathEntry[]) => void, i: number, field: string, value: string) => {
    const next = list.map((p, j) => (j === i ? { ...p, [field]: value } : p));
    setList(next);
  };
  const removePath = (list: PathEntry[], setList: (v: PathEntry[]) => void, i: number) => {
    setList(list.filter((_, j) => j !== i));
  };
  const addDevice = () => setDevices([...devices, { path: "", kind: "character", major: 0, minor: 0 }]);
  const updateDevice = (i: number, field: string, value: string) => {
    const next = devices.map((d, j) => (j === i ? { ...d, [field]: value } : d));
    setDevices(next);
  };
  const removeDevice = (i: number) => setDevices(devices.filter((_, j) => j !== i));

  return (
    <div className="modal-overlay" onClick={onClose} role="presentation">
      <div className="modal-content" onClick={(e) => e.stopPropagation()} role="dialog">
        <div className="modal-header">
          <h2>{grant ? t("grant.editTitle") : t("grant.addTitle")}</h2>
          <button className="modal-close" onClick={onClose} disabled={busy}>
            <X size={16} />
          </button>
        </div>

        <div className="modal-body">
          {!grant && (
            <label className="set-field">
              <span className="set-field__label">{t("grant.source")}</span>
              <select className="mem-select set-grow" value={source} disabled={busy} onChange={(e) => setSource(e.target.value)}>
                <option value="project">{t("grant.sourceProject")}</option>
                <option value="user">{t("grant.sourceUser")}</option>
              </select>
            </label>
          )}

          <label className="set-field">
            <span className="set-field__label">{t("grant.canonicalExecutable")}</span>
            <input className="mem-input set-grow" value={executable} disabled={busy} onChange={(e) => setExecutable(e.target.value)} placeholder="/usr/bin/npm" />
          </label>

          <label className="set-field">
            <span className="set-field__label">{t("grant.argvPrefix")}</span>
            <div className="set-rules">
              <div className="set-rules__chips">
                {argvPrefix.map((p, i) => (
                  <span className="set-rule" key={`${p}-${i}`}>
                    {p}
                    <button className="set-rule__x" disabled={busy} onClick={() => setArgvPrefix(argvPrefix.filter((_, j) => j !== i))}>
                      <X size={12} />
                    </button>
                  </span>
                ))}
              </div>
              <div className="set-rules__add">
                <input className="mem-input" value={prefixDraft} disabled={busy} placeholder={t("grant.prefixPlaceholder")} onChange={(e) => setPrefixDraft(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") addPrefix(); }} />
                <button className="btn btn--small" disabled={busy || !prefixDraft.trim()} onClick={addPrefix}>
                  <Plus size={14} />
                </button>
              </div>
            </div>
          </label>

          <div className="set-field">
            <span className="set-field__label">{t("grant.capabilities")}</span>
            <div className="set-check-group">
              <label className="set-check">
                <input type="checkbox" checked={network} disabled={busy} onChange={(e) => setNetwork(e.target.checked)} />
                {t("grant.network")}
              </label>
              <label className="set-check">
                <input type="checkbox" checked={background} disabled={busy} onChange={(e) => setBackground(e.target.checked)} />
                {t("grant.background")}
              </label>
              <label className="set-check">
                <input type="checkbox" checked={preserveBg} disabled={busy} onChange={(e) => setPreserveBg(e.target.checked)} />
                {t("grant.preserveBackground")}
              </label>
            </div>
          </div>

          <div className="set-section-divider" />

          <PathSection label={t("grant.reads")} paths={reads} busy={busy} onChange={setReads} addPath={addPath} updatePath={updatePath} removePath={removePath} />
          <PathSection label={t("grant.writes")} paths={writes} busy={busy} onChange={setWrites} addPath={addPath} updatePath={updatePath} removePath={removePath} />

          <div className="set-section-divider" />

          <div className="set-field">
            <span className="set-field__label">{t("grant.devices")}</span>
            {devices.map((d, i) => (
              <div className="set-device-row" key={i}>
                <input className="mem-input" value={d.path} disabled={busy} placeholder="/dev/dri" onChange={(e) => updateDevice(i, "path", e.target.value)} />
                <select className="mem-select" value={d.kind} disabled={busy} onChange={(e) => updateDevice(i, "kind", e.target.value)}>
                  <option value="character">character</option>
                  <option value="block">block</option>
                </select>
                <button className="btn btn--small btn--danger" disabled={busy} onClick={() => removeDevice(i)}>
                  <X size={14} />
                </button>
              </div>
            ))}
            <button className="btn btn--small" disabled={busy} onClick={addDevice}>
              <Plus size={14} /> {t("grant.addDevice")}
            </button>
          </div>
        </div>

        <div className="modal-footer">
          {error && <div className="modal-footer__error">{error}</div>}
          <button className="btn" disabled={busy} onClick={onClose}>{t("common.cancel")}</button>
          <button className="btn btn--primary" disabled={busy || !executable.trim()} onClick={handleSave}>{t("grant.save")}</button>
        </div>
      </div>
    </div>
  );
}

function PathSection({ label, paths, busy, onChange, addPath, updatePath, removePath }: {
  label: string;
  paths: PathEntry[];
  busy: boolean;
  onChange: (v: PathEntry[]) => void;
  addPath: (list: PathEntry[], setList: (v: PathEntry[]) => void) => void;
  updatePath: (list: PathEntry[], setList: (v: PathEntry[]) => void, i: number, field: string, value: string) => void;
  removePath: (list: PathEntry[], setList: (v: PathEntry[]) => void, i: number) => void;
}) {
  const t = useT();
  return (
    <div className="set-field">
      <span className="set-field__label">{label}</span>
      {paths.map((p, i) => (
        <div className="set-path-row" key={i}>
          <select className="mem-select" value={p.identity} disabled={busy} onChange={(e) => updatePath(paths, onChange, i, "identity", e.target.value)}>
            <option value="workspace_relative">workspace_relative</option>
            <option value="canonical_absolute">canonical_absolute</option>
          </select>
          <input className="mem-input" value={p.path} disabled={busy} placeholder="path" onChange={(e) => updatePath(paths, onChange, i, "path", e.target.value)} />
          <select className="mem-select" value={p.kind} disabled={busy} onChange={(e) => updatePath(paths, onChange, i, "kind", e.target.value)}>
            <option value="directory">directory</option>
            <option value="file">file</option>
          </select>
          <button className="btn btn--small btn--danger" disabled={busy} onClick={() => removePath(paths, onChange, i)}>
            <X size={14} />
          </button>
        </div>
      ))}
      <button className="btn btn--small" disabled={busy} onClick={() => addPath(paths, onChange)}>
        <Plus size={14} /> {t("grant.addPath")}
      </button>
    </div>
  );
}
