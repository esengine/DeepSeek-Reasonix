import { useRef, useState } from "react";
import type { AgentPort } from "../port/port";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";

interface Props {
  port: AgentPort;
  empty: boolean;
  onImported: () => void;
  onUse: (id: string) => void;
}

// A zip carries a whole pack; loose files are a pack's folder opened and
// selected, which is what a browser can read without learning a path.
const ACCEPT = ".zip,.json,.webp,.png,.jpg,.jpeg";

/** ThemeImport installs a pack from disk and shows where installed packs live,
 *  so authoring one never starts with guessing a directory. */
export function ThemeImport({ port, empty, onImported, onUse }: Props) {
  const [owner, setOwner] = useState({ port });
  const currentOwner = useRef(owner);
  currentOwner.current = owner;
  const file = useRef<HTMLInputElement>(null);
  const pending = useRef(false);
  const folderRequest = useRef(0);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");
  const [landed, setLanded] = useState("");
  const [failed, setFailed] = useState("");
  if (owner.port !== port) {
    setOwner({ port });
    pending.current = false;
    folderRequest.current += 1;
    setBusy(false);
    setNote("");
    setLanded("");
    setFailed("");
  }

  const take = async (files: File[]) => {
    if (files.length === 0 || pending.current) return;
    pending.current = true;
    folderRequest.current += 1;
    setBusy(true);
    setFailed("");
    setNote("");
    setLanded("");
    try {
      const got = await port.importTheme(files);
      if (currentOwner.current !== owner) return;
      onImported();
      const skipped = got.ignored?.length ? " " + t("look.not_read_names", { names: got.ignored.join("、") }) : "";
      setNote(t("look.imported_name", { name: got.pack.name }) + skipped);
      setLanded(got.pack.id);
    } catch (e) {
      if (currentOwner.current === owner) setFailed(reason(e));
    } finally {
      if (currentOwner.current === owner) {
        pending.current = false;
        setBusy(false);
      }
    }
  };

  const reveal = () => {
    if (pending.current) return;
    const request = ++folderRequest.current;
    setFailed("");
    setLanded("");
    port
      .openThemeFolder()
      .then((dir) => {
        if (currentOwner.current === owner && request === folderRequest.current) setNote(t("look.themes_folder_path", { path: dir }));
      })
      .catch((e) => {
        if (currentOwner.current === owner && request === folderRequest.current) setFailed(reason(e));
      });
  };

  return (
    <>
      <input
        ref={file}
        type="file"
        multiple
        accept={ACCEPT}
        disabled={busy}
        hidden
        data-action="theme.import"
        onChange={(e) => {
          void take([...(e.target.files ?? [])]);
          e.target.value = "";
        }}
      />
      <button className="paperpick" data-action="theme.import" data-busy={busy ? "" : undefined} disabled={busy} onClick={() => file.current?.click()}>
        <span className="plus" aria-hidden="true">
          <svg viewBox="0 0 16 16">
            <path d="M8 3.7v8.6M3.7 8h8.6" />
          </svg>
        </span>
        {t(busy ? "common.importing" : "look.import_theme")}
      </button>
      <div className="themeacts" data-landed={landed ? "" : undefined}>
        <p className="note" role="status" aria-atomic="true">
          {note || t(empty ? "look.no_themes_installed_choose_a" : "look.choose_a_zip_or_select")}
        </p>
        {landed && (
          <button
            className="btn sm"
            data-action="theme.activate"
            onClick={() => {
              onUse(landed);
              setLanded("");
            }}
          >
            {t("look.use_now")}
          </button>
        )}
        <button className="btn sm" data-action="theme.folder" disabled={busy} onClick={reveal}>
          {t("look.open_themes_folder")}
        </button>
      </div>
      {failed && (
        <div className="find themefail" data-lvl="err" role="alert">
          <span className="t">{failed}</span>
        </div>
      )}
    </>
  );
}
