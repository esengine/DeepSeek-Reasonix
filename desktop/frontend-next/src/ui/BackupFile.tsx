import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, BackupCategory, BackupFileCatalog } from "../port/port";
import { BackupRestore, CATEGORY_LABEL, type RestoreSource } from "./BackupRestore";
import { CATEGORY_NOTE } from "./Backup";
import { StudioIcon } from "./StudioIcon";

// The same sealed envelope the account keeps, written to a file the person
// chooses. It asks nothing of the account: the passphrase is the only secret,
// and the kernel offers it to this window alone.
export function BackupFile({ port }: { port: AgentPort }) {
  const [catalog, setCatalog] = useState<BackupFileCatalog | null>(null);
  const [unavailable, setUnavailable] = useState(false);
  const [chosen, setChosen] = useState<Set<BackupCategory>>(new Set());
  const [pass, setPass] = useState("");
  const [again, setAgain] = useState("");
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  const [source, setSource] = useState<RestoreSource | null>(null);
  const chooser = useRef<HTMLInputElement>(null);

  const load = useCallback(async () => {
    try {
      const c = await port.backupFileCatalog();
      setCatalog(c);
      setChosen(new Set(c.categories.filter((x) => x.defaultOn).map((x) => x.id)));
    } catch {
      setUnavailable(true);
    }
  }, [port]);
  useEffect(() => { void load(); }, [load]);

  if (unavailable) return <p className="acct-note">{t("data.exporting_and_importing_a_config")}</p>;
  if (!catalog) return <p className="acct-note">{t("common.loading")}</p>;

  const min = catalog.minPassphrase;
  const mismatch = again !== "" && pass !== again;
  const ready = chosen.size > 0 && pass.length >= min && pass === again && !busy;
  const wait = !chosen.size ? t("data.tick_at_least_one_thing")
    : pass.length < min ? t("data.passphrase_too_short_n_more", { n: min - pass.length })
      : again === "" ? t("data.repeat_the_passphrase_to_confirm") : "";

  const toggle = (id: BackupCategory) =>
    setChosen((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const exportFile = async () => {
    setBusy(true);
    setNote("");
    setError("");
    try {
      const order = catalog.categories.map((c) => c.id).filter((id) => chosen.has(id));
      const out = await port.exportBackupFile(order, pass);
      await port.saveText(out.name, out.file);
      setPass("");
      setAgain("");
      const skipped = out.omitted?.length ?? 0;
      setNote(skipped ? t("data.exported_n_files_were_too", { n: skipped }) : t("data.exported"));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const choose = async (file: File | undefined) => {
    if (!file) return;
    setError("");
    try {
      const text = (await file.text()).trim();
      setSource({
        name: file.name,
        preview: (passphrase) => port.previewBackupFile(text, passphrase),
        apply: (planId, items, consented) => port.applyBackupFile(planId, items, consented),
        // A file from elsewhere starts by adding what this machine lacks; what it
        // would replace is the person's to tick.
        preselect: (item) => item.recommended && item.status === "new",
      });
    } catch (e) {
      setError(reason(e));
    }
  };

  return (
    <div className="bk">
      <p className="acct-note">
        {t("data.the_file_is_encrypted_on")}
      </p>
      <div className="bk-cats bk-chips" role="group" aria-label={t("data.what_to_export")}>
        {catalog.categories.map((c) => (
          <label key={c.id} className="bk-chip" data-on={chosen.has(c.id) ? "" : undefined} data-warn={c.id === "secrets" && chosen.has(c.id) ? "" : undefined} title={t(c.id === "secrets" ? "data.off_by_default_if_ticked" : CATEGORY_NOTE[c.id])}>
            <input type="checkbox" checked={chosen.has(c.id)} disabled={busy} onChange={() => toggle(c.id)} data-action="backup.category" data-target={c.id} />
            <span className="box" aria-hidden="true">{chosen.has(c.id) && <StudioIcon name="check" />}</span>
            {t(CATEGORY_LABEL[c.id])}
          </label>
        ))}
      </div>
      {chosen.has("secrets") && <p className="acct-note bk-warn">{t("data.off_by_default_if_ticked")}</p>}
      <div className="bk-fields">
        <label className="grow">
          <span>{t("data.passphrase_at_least_n_characters", { n: min })}</span>
          <input data-action="backup.passphrase" data-target="new" type="password" autoComplete="new-password" value={pass} disabled={busy} onChange={(e) => setPass(e.target.value)} />
        </label>
        <label className="grow">
          <span>{t("data.repeat_it")}</span>
          <input data-action="backup.passphrase" data-target="again" type="password" autoComplete="new-password" value={again} disabled={busy} onChange={(e) => setAgain(e.target.value)} />
        </label>
      </div>
      {mismatch && <p className="acct-note" data-err="">{t("data.the_two_passphrases_differ")}</p>}
      <div className="bk-acts">
        <button className="act" data-primary data-action="backup.file-export" disabled={!ready} aria-describedby={!ready && wait ? "bkf-wait" : undefined} onClick={() => void exportFile()}>
          {t(busy ? "data.encrypting" : "data.export_to_file")}
        </button>
        {!ready && !busy && wait && <span id="bkf-wait" className="acct-note bk-wait" aria-live="polite" data-action="backup.wait">{wait}</span>}
        <button className="act" data-action="backup.file-import" disabled={busy} onClick={() => chooser.current?.click()}>{t("data.import_from_file")}</button>
        <input ref={chooser} type="file" accept=".rxcb,text/plain" hidden data-action="backup.file-choose" onChange={(e) => { void choose(e.target.files?.[0]); e.target.value = ""; }} />
      </div>
      {note && <p className="acct-note">{note}</p>}
      {error && <p className="acct-note" data-err="">{error}</p>}
      {source && <BackupRestore key={source.name} port={port} source={source} onClose={() => setSource(null)} />}
    </div>
  );
}
