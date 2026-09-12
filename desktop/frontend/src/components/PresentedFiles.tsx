import { useEffect, useRef, useState } from "react";
import {
  ChevronDown, ChevronUp, Code2, ExternalLink, FileArchive, FileAudio,
  FileImage, FileText, FileVideo, FolderSearch, Globe, MoreHorizontal, Save,
} from "lucide-react";
import type { PresentedFileView } from "../lib/chatViewSource";
import type { TurnFileView } from "../lib/turnFiles";
import { useT } from "../lib/i18n";
import {
  invalidateFileResourceNavigation, openResource, performResourceAction, resolveFileResourcePath,
  type FileResourceRef, type PresentedFileAction,
} from "../lib/presentedFileNavigation";
import { fileResourceCapabilities } from "../lib/fileResource";
import { writeClipboardText } from "../lib/clipboard";
import "./PresentedFiles.css";

const basename = (path: string) => path.replaceAll("\\", "/").split("/").filter(Boolean).pop() || path;
const extension = (path: string) => basename(path).split(".").pop()?.toLowerCase() ?? "";

function iconFor(path: string) {
  const ext = extension(path);
  if (["png", "jpg", "jpeg", "gif", "webp", "bmp", "ico", "svg"].includes(ext)) return FileImage;
  if (["mp3", "wav", "ogg", "m4a", "aac", "flac"].includes(ext)) return FileAudio;
  if (["mp4", "webm", "mov", "m4v", "ogv"].includes(ext)) return FileVideo;
  if (["zip", "tar", "gz", "7z", "rar"].includes(ext)) return FileArchive;
  if (["js", "jsx", "ts", "tsx", "go", "rs", "py", "java", "c", "cpp", "css", "html", "htm", "json", "csv", "md"].includes(ext)) return Code2;
  return FileText;
}

export function PresentedFiles({ files, tabId, hostId }: { files: readonly PresentedFileView[]; tabId?: string; hostId?: string }) {
  const t = useT();
  const [expanded, setExpanded] = useState(false);
  useEffect(() => invalidateFileResourceNavigation, []);
  const shown = expanded ? files : files.slice(0, 4);
  if (!files.length) return null;
  return <section className="presented-files" aria-label={t("present.files")}>
    <div className="presented-files__grid">
      {shown.map(file => <FileEntry key={file.path} description={file.description}
        refValue={{ source: "presented", hostId: hostId ?? "local", tabId: tabId ?? "", toolCallId: file.toolCallId, path: file.path }} />)}
    </div>
    {files.length > 4 && <button type="button" className="presented-files__toggle" onClick={() => setExpanded(value => !value)}>
      {expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
      {t(expanded ? "present.collapse" : "present.showAll", { count: files.length })}
    </button>}
  </section>;
}

export function ModifiedFiles({ files, tabId, hostId }: { files: readonly TurnFileView[]; tabId?: string; hostId?: string }) {
  const t = useT();
  const [expanded, setExpanded] = useState(false);
  useEffect(() => invalidateFileResourceNavigation, []);
  const shown = expanded ? files : files.slice(0, 6);
  if (!files.length) return null;
  return <section className="turn-files" aria-label={t("present.modifiedFiles")}>
    <strong className="turn-files__title">{t("present.modifiedFiles")}</strong>
    <div className="turn-files__list">
      {shown.map(file => <FileEntry key={file.path} compact
        description={t(file.operation === "modified" ? "present.modified" : "present.written")}
        refValue={{ source: "workspace", hostId: hostId ?? "local", tabId: tabId ?? "", toolCallId: file.toolCallId, path: file.path }} />)}
    </div>
    {files.length > 6 && <button type="button" className="presented-files__toggle" onClick={() => setExpanded(value => !value)}>
      {expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
      {t(expanded ? "present.collapse" : "present.showAll", { count: files.length })}
    </button>}
  </section>;
}

function FileEntry({ refValue, description, compact = false }: { refValue: FileResourceRef; description?: string; compact?: boolean }) {
  const t = useT();
  const [menu, setMenu] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const menuRoot = useRef<HTMLDivElement>(null);
  const menuTrigger = useRef<HTMLButtonElement>(null);
  const Icon = iconFor(refValue.path);
  const capabilities = fileResourceCapabilities(refValue);
  useEffect(() => {
    if (!menu) return;
    menuRoot.current?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus();
    const onPointerDown = (event: PointerEvent) => {
      if (!menuRoot.current?.contains(event.target as Node)) setMenu(false);
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault(); setMenu(false); menuTrigger.current?.focus(); return;
      }
      if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
      const items = Array.from(menuRoot.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? []);
      if (!items.length) return;
      event.preventDefault();
      const current = Math.max(0, items.indexOf(document.activeElement as HTMLButtonElement));
      const next = event.key === "Home" ? 0 : event.key === "End" ? items.length - 1
        : event.key === "ArrowDown" ? (current + 1) % items.length : (current - 1 + items.length) % items.length;
      items[next]?.focus();
    };
    window.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [menu]);
  const run = async (action: PresentedFileAction) => {
    setMenu(false); setError(""); setBusy(true);
    try {
      if (action === "preview" || action === "source" || action === "browser") await openResource(refValue, { view: action });
      else await performResourceAction(refValue, action);
    }
    catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)); }
    finally { setBusy(false); }
  };
  const copyPath = async () => {
    setMenu(false); setError(""); setBusy(true);
    try {
      const path = await resolveFileResourcePath(refValue);
      if (!await writeClipboardText(path)) throw new Error(t("present.copyFailed"));
    } catch (reason) { setError(reason instanceof Error ? reason.message : String(reason)); }
    finally { setBusy(false); }
  };
  return <article className={compact ? "presented-file presented-file--compact" : "presented-file"} title={refValue.path} aria-busy={busy || undefined}>
    <button type="button" className="presented-file__main" disabled={busy} onClick={() => void run("preview")}>
      <span className="presented-file__icon"><Icon size={compact ? 16 : 20} /></span>
      <span className="presented-file__copy"><strong>{basename(refValue.path)}</strong>{description && <small>{description}</small>}</span>
    </button>
    {!compact && <button type="button" className="presented-file__open" disabled={busy} onClick={() => void run("preview")}>{t(busy ? "chat.loading" : "present.open")}</button>}
    <div className="presented-file__menu-wrap" ref={menuRoot}>
      <button ref={menuTrigger} type="button" className="presented-file__more" disabled={busy} aria-label={t("present.more")} aria-haspopup="menu" aria-expanded={menu} onClick={() => setMenu(value => !value)}><MoreHorizontal size={17} /></button>
      {menu && <div className="presented-file__menu" role="menu">
        {capabilities.browser && <MenuItem icon={Globe} label={t("present.browser")} onClick={() => void run("browser")} />}
        {capabilities.revealTree && <MenuItem icon={FolderSearch} label={t("present.revealTree")} onClick={() => void run("reveal-tree")} />}
        {capabilities.source && <MenuItem icon={Code2} label={t("present.source")} onClick={() => void run("source")} />}
        {capabilities.copyPath && <MenuItem icon={FileText} label={t("present.copyPath")} onClick={() => void copyPath()} />}
        {capabilities.openNative && <MenuItem icon={ExternalLink} label={t("present.openNative")} onClick={() => void run("open-native")} />}
        {capabilities.revealNative && <MenuItem icon={FolderSearch} label={t("present.revealNative")} onClick={() => void run("reveal-native")} />}
        {capabilities.saveCopy && <MenuItem icon={Save} label={t("present.saveCopy")} onClick={() => void run("save-copy")} />}
      </div>}
    </div>
    {error && <p className="presented-file__error" role="status">{error}</p>}
  </article>;
}

function MenuItem({ icon: Icon, label, onClick }: { icon: typeof FileText; label: string; onClick: () => void }) {
  return <button type="button" role="menuitem" onClick={onClick}><Icon size={14} /><span>{label}</span></button>;
}
