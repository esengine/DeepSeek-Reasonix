import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, BackupCatalog, BackupCategory, BackupEntry } from "../port/port";
import { accountBackupSource, BackupRestore, CATEGORY_LABEL } from "./BackupRestore";

export const CATEGORY_NOTE: Record<BackupCategory, string> = {
  settings: "backup.providersDefaultModelPresetsAnd",
  extensions: "backup.pluginsAreRecordedBySource",
  memory: "backup.userLevelInstructionFilesAnd",
  automation: "backup.commandsThatRunOnThis",
  secrets: "backup.offByDefaultWhenTicked",
};

// Shown only to a signed-in account: a backup lives in the account, so there
// is nothing to offer before one exists.
export function Backup({ port }: { port: AgentPort }) {
  const [connection, setConnection] = useState({ port, generation: 0 });
  if (connection.port !== port) setConnection({ port, generation: connection.generation + 1 });
  return <BackupInput key={connection.generation} port={port} />;
}

function BackupInput({ port }: { port: AgentPort }) {
  const [catalog, setCatalog] = useState<BackupCatalog | null>(null);
  const [error, setError] = useState("");
  const [chosen, setChosen] = useState<Set<BackupCategory> | null>(null);
  const [label, setLabel] = useState("");
  const [pass, setPass] = useState("");
  const [again, setAgain] = useState("");
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");
  const [dropping, setDropping] = useState(() => new Map<string, "confirm" | "pending">());
  const [restoring, setRestoring] = useState<BackupEntry | null>(null);

  const load = useCallback(async () => {
    try {
      const c = await port.backups();
      setCatalog(c);
      setChosen((prev) => prev ?? new Set(c.categories.filter((x) => x.defaultOn).map((x) => x.id)));
      setError("");
    } catch (e) {
      setError(reason(e));
    }
  }, [port]);

  useEffect(() => {
    void load();
  }, [load]);

  const toggle = (id: BackupCategory) =>
    setChosen((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const min = catalog?.minPassphrase ?? 10;
  const mismatch = again !== "" && pass !== again;
  const ready = (chosen?.size ?? 0) > 0 && pass.length >= min && pass === again && !busy;
  const wait =
    !chosen?.size
      ? t("common.tickAtLeastOneThing")
      : pass.length < min
        ? t("common.passphraseTooShortMoreNeeded", { n: min - pass.length })
        : again === ""
          ? t("common.repeatThePassphraseToConfirm")
          : "";

  const create = async () => {
    setBusy(true);
    setNote("");
    setError("");
    try {
      const order = (catalog?.categories ?? []).map((c) => c.id).filter((id) => chosen?.has(id));
      const out = await port.createBackup({ label: label.trim(), categories: order, passphrase: pass });
      setPass("");
      setAgain("");
      setLabel("");
      const skipped = out.omitted?.length ?? 0;
      setNote(skipped ? t("backup.backedUpFilesWereToo", { n: skipped }) : t("backup.backedUp"));
      await load();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const drop = async (id: string) => {
    if (dropping.get(id) !== "confirm") {
      setDropping((current) => new Map([...current].filter(([, phase]) => phase === "pending")).set(id, "confirm"));
      return;
    }
    setDropping((current) => new Map(current).set(id, "pending"));
    try {
      await port.deleteBackup(id);
      await load();
    } catch (e) {
      setError(reason(e));
    } finally {
      setDropping((current) => {
        const next = new Map(current);
        next.delete(id);
        return next;
      });
    }
  };

  if (!catalog) {
    return error ? <p className="acct-note" data-err="">{error}</p> : <p className="acct-note">{t("backup.loadingBackups")}</p>;
  }

  return (
    <div className="bk">
      <p className="acct-note">
        {t("backup.aBackupIsEncryptedHere")}
      </p>

      <div className="bk-cats" role="group" aria-label={t("backup.whatToBackUp")}>
        {catalog.categories.map((c) => (
          <label key={c.id} className="bk-cat" data-warn={c.id === "secrets" && chosen?.has(c.id) ? "" : undefined}>
            <input type="checkbox" checked={chosen?.has(c.id) ?? false} disabled={busy} onChange={() => toggle(c.id)} data-action="backup.category" data-target={c.id} />
            <span className="nm">{t(CATEGORY_LABEL[c.id])}</span>
            <span className="why">{t(CATEGORY_NOTE[c.id])}</span>
          </label>
        ))}
      </div>

      <div className="bk-fields">
        <label className="grow full">
          <span>{t("backup.labelOptional")}</span>
          <input data-action="backup.label" value={label} disabled={busy} maxLength={80} placeholder={t("backup.eGWorkLaptop")} onChange={(e) => setLabel(e.target.value)} />
        </label>
        <label className="grow">
          <span>{t("common.passphraseAtLeastCharacters", { n: min })}</span>
          <input data-action="backup.passphrase" data-target="new" type="password" autoComplete="new-password" value={pass} disabled={busy} onChange={(e) => setPass(e.target.value)} />
        </label>
        <label className="grow">
          <span>{t("common.repeatIt")}</span>
          <input data-action="backup.passphrase" data-target="again" type="password" autoComplete="new-password" value={again} disabled={busy} onChange={(e) => setAgain(e.target.value)} />
        </label>
      </div>
      {mismatch && <p className="acct-note" data-err="">{t("common.theTwoPassphrasesDiffer")}</p>}

      <div className="bk-acts">
        <button className="act" data-primary data-action="backup.create" disabled={!ready} aria-describedby={!ready && wait ? "bk-wait" : undefined} onClick={() => void create()}>
          {t(busy ? "backup.encryptingAndUploading" : "backup.backUpToAccount")}
        </button>
        {!ready && !busy && wait && (
          <span id="bk-wait" className="acct-note" aria-live="polite" data-action="backup.wait">
            {wait}
          </span>
        )}
        <span className="acct-note">
          {t("backup.ofUsed", { n: catalog.backups.length, max: catalog.limits.maxCount })}
        </span>
      </div>
      {note && <p className="acct-note">{note}</p>}
      {error && <p className="acct-note" data-err="">{error}</p>}

      {catalog.backups.length === 0 ? (
        <p className="acct-note">{t("backup.noBackupsInThisAccount")}</p>
      ) : (
        <ul className="bk-list">
          {catalog.backups.map((b) => (
            <li key={b.id} className="bk-row">
              <div className="who">
                <span className="nm">{b.label || t("common.untitledBackup")}</span>
                <span className="meta">
                  {new Date(b.createdAt).toLocaleString()} · {b.platform} · {b.appVersion || "—"} · {Math.max(1, Math.round(b.ciphertextBytes / 1024))} KB
                </span>
                <span className="meta">{b.categories.map((c) => t(CATEGORY_LABEL[c] ?? c)).join(t("common.listSeparator"))}</span>
              </div>
              <button className="btn sm" data-action="backup.restore" data-target={b.id} onClick={() => setRestoring(b)}>
                {t("backup.restore")}
              </button>
              <button className="btn sm" data-action="backup.delete" data-target={b.id} disabled={dropping.get(b.id) === "pending"} aria-busy={dropping.get(b.id) === "pending" || undefined} onClick={() => void drop(b.id)} onMouseLeave={() => setDropping((current) => current.get(b.id) === "confirm" ? new Map([...current].filter(([id]) => id !== b.id)) : current)}>
                {t(dropping.get(b.id) === "confirm" ? "common.confirmDelete" : "common.delete")}
              </button>
            </li>
          ))}
        </ul>
      )}

      {restoring && <BackupRestore key={restoring.id} port={port} source={accountBackupSource(port, restoring)} onClose={() => setRestoring(null)} />}
    </div>
  );
}
