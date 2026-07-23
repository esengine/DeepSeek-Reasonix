import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Check, Copy, Download, Pencil, Plus, RotateCcw, Trash2, Upload } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { THEME_STYLES, type ThemeStyle, isThemeStyle } from "../lib/theme";
import {
  type ThemePackBackground,
  type ThemePackRecipes,
  type ThemePackTokens,
  type ThemePackView,
  type ThemeSaveInput,
  applyThemePack,
  beginThemePreview,
  cancelThemePreview,
  clearThemePack,
  commitThemePreview,
  defaultBackground,
  draftPackView,
  emptyThemeTokens,
  isSafeHex,
  themePackKind,
  themeTokenKeys,
} from "../lib/themePack";
import { useToast } from "../lib/toast";
import { useConfirmDialog } from "./ConfirmDialog";

type EditorState = {
  mode: "create" | "edit";
  id: string;
  name: string;
  author: string;
  description: string;
  license: string;
  baseStyle: ThemeStyle;
  tokens: ThemePackTokens;
  recipes: ThemePackRecipes;
  background: ThemePackBackground | null;
  backgroundDataUrl: string;
  existingBackgroundUrl: string;
  tokenMode: "light" | "dark";
  originalId: string;
};

const TOKEN_GROUPS: { labelKey: string; keys: string[] }[] = [
  { labelKey: "settings.themeTokens.surfaces", keys: ["bg", "bgSoft", "bgElev", "panel", "sidebar", "chat", "workspace", "workspaceFiles"] },
  { labelKey: "settings.themeTokens.borderText", keys: ["border", "borderSoft", "fg", "fgDim", "fgFaint"] },
  { labelKey: "settings.themeTokens.accentStatus", keys: ["accent", "accentFg", "ok", "warn", "err"] },
];

function slugifyId(name: string): string {
  const s = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 48);
  if (!s) return "my-theme";
  if (/^[a-z][a-z0-9-]*[a-z0-9]$|^[a-z]$/.test(s)) return s;
  return `t-${s}`.slice(0, 48);
}

function packToEditor(pack: ThemePackView, mode: "create" | "edit"): EditorState {
  return {
    mode,
    id: mode === "create" ? slugifyId(`${pack.id}-copy`) : pack.id,
    name: mode === "create" ? `${pack.name} Copy` : pack.name,
    author: pack.author || "",
    description: pack.description || "",
    license: pack.license || "",
    baseStyle: isThemeStyle(pack.baseStyle) ? pack.baseStyle : "graphite",
    tokens: {
      light: { ...(pack.tokens?.light || {}) },
      dark: { ...(pack.tokens?.dark || {}) },
    },
    recipes: {
      density: pack.recipes?.density === "compact" ? "compact" : "comfortable",
      corners: pack.recipes?.corners === "square" || pack.recipes?.corners === "round" ? pack.recipes.corners : "soft",
    },
    background: pack.background ? { ...pack.background } : null,
    backgroundDataUrl: "",
    existingBackgroundUrl: pack.backgroundUrl || "",
    tokenMode: "dark",
    originalId: pack.id,
  };
}

function emptyEditor(): EditorState {
  return {
    mode: "create",
    id: "my-theme",
    name: "My Theme",
    author: "",
    description: "",
    license: "",
    baseStyle: "graphite",
    tokens: emptyThemeTokens(),
    recipes: { density: "comfortable", corners: "soft" },
    background: null,
    backgroundDataUrl: "",
    existingBackgroundUrl: "",
    tokenMode: "dark",
    originalId: "",
  };
}

export function ThemeLibrarySection() {
  const t = useT();
  const { showToast } = useToast();
  const { confirm, dialog: confirmDialog } = useConfirmDialog();
  const [packs, setPacks] = useState<ThemePackView[]>([]);
  const [loading, setLoading] = useState(true);
  const [editor, setEditor] = useState<EditorState | null>(null);
  const [busy, setBusy] = useState(false);
  const previewTimer = useRef<number | null>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const list = await app.ListThemePacks();
      setPacks(list || []);
      const active = await app.GetActiveThemePack();
      if (active?.pack) {
        commitThemePreview(active.pack);
      } else if (!active?.safeMode) {
        const still = (list || []).find((p) => p.active);
        if (!still) clearThemePack();
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void reload();
    return () => {
      if (previewTimer.current) window.clearTimeout(previewTimer.current);
      // Closing settings / leaving the appearance tab must not leave a draft preview applied.
      cancelThemePreview();
    };
  }, [reload]);

  const schedulePreview = useCallback((state: EditorState) => {
    if (previewTimer.current) window.clearTimeout(previewTimer.current);
    previewTimer.current = window.setTimeout(() => {
      const bgUrl = state.backgroundDataUrl || state.existingBackgroundUrl || "";
      const draft = draftPackView({
        id: state.id || "preview",
        name: state.name,
        baseStyle: state.baseStyle,
        tokens: state.tokens,
        recipes: state.recipes,
        background: state.background,
        backgroundUrl: bgUrl,
      });
      beginThemePreview(draft);
    }, 80);
  }, []);

  const openCreate = () => {
    const state = emptyEditor();
    setEditor(state);
    schedulePreview(state);
  };

  const openEdit = (pack: ThemePackView) => {
    if (themePackKind(pack) !== "user") {
      // Editing a base style means copying into a user theme.
      const state = packToEditor(pack, "create");
      setEditor(state);
      schedulePreview(state);
      return;
    }
    const state = packToEditor(pack, "edit");
    setEditor(state);
    schedulePreview(state);
  };

  const openCopy = async (pack: ThemePackView) => {
    setBusy(true);
    try {
      const newId = slugifyId(`${pack.id}-copy`);
      const created = await app.CopyThemePack(pack.id, newId, `${pack.name} Copy`);
      showToast(t("settings.themeLibrary.copied", { name: created.name }), "info");
      await reload();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  const activate = async (pack: ThemePackView) => {
    setBusy(true);
    try {
      await app.ActivateThemePack(pack.id);
      const active = await app.GetActiveThemePack();
      commitThemePreview(active.pack ?? null);
      // Sync base style via appearance when activating a pack.
      if (active.pack && isThemeStyle(active.pack.baseStyle)) {
        // Appearance style stays independent in config; pack overlay supplies baseStyle live.
      }
      await reload();
      showToast(t("settings.themeLibrary.activated", { name: pack.name }), "info");
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  const resetDefault = async () => {
    setBusy(true);
    try {
      await app.ResetThemePack();
      cancelThemePreview();
      applyThemePack(null);
      await reload();
      showToast(t("settings.themeLibrary.resetDone"), "info");
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  const remove = async (pack: ThemePackView) => {
    if (themePackKind(pack) !== "user") return;
    const ok = await confirm({
      title: t("settings.themeLibrary.confirmDeleteTitle"),
      message: t("settings.themeLibrary.confirmDelete", { name: packDisplayName(pack, t) }),
      confirmLabel: t("common.delete"),
      cancelLabel: t("common.cancel"),
      tone: "danger",
    });
    if (!ok) return;
    setBusy(true);
    try {
      await app.DeleteThemePack(pack.id);
      await reload();
      const active = await app.GetActiveThemePack();
      commitThemePreview(active.pack ?? null);
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  const doImport = async (replace = false) => {
    setBusy(true);
    try {
      // First call may open a file dialog. On ID conflict the backend stages the
      // extract and returns needsReplace — confirm then call again with replace=true
      // (empty path) so the staged import is published without re-picking a file.
      const result = await app.ImportThemePack("", replace);
      if (!result) return;
      if (result.needsReplace) {
        const ok = await confirm({
          title: t("settings.themeLibrary.confirmReplaceImportTitle"),
          message: t("settings.themeLibrary.confirmReplaceImport"),
          confirmLabel: t("settings.themeLibrary.replaceConfirm"),
          cancelLabel: t("common.cancel"),
        });
        if (!ok) return;
        const confirmed = await app.ImportThemePack("", true);
        if (!confirmed?.pack?.id) return;
        showToast(t("settings.themeLibrary.imported", { name: confirmed.pack.name }), "info");
        await reload();
        return;
      }
      if (!result.pack?.id) {
        // Cancelled
        return;
      }
      showToast(t("settings.themeLibrary.imported", { name: result.pack.name }), "info");
      await reload();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast(msg, "error");
    } finally {
      setBusy(false);
    }
  };

  const doExport = async (pack: ThemePackView) => {
    if (pack.hasBackground) {
      const ok = await confirm({
        title: t("settings.themeLibrary.exportRightsTitle"),
        message: t("settings.themeLibrary.exportRights"),
        confirmLabel: t("settings.themeLibrary.exportConfirm"),
        cancelLabel: t("common.cancel"),
      });
      if (!ok) return;
    }
    setBusy(true);
    try {
      const path = await app.ExportThemePack(pack.id, "");
      if (path) showToast(t("settings.themeLibrary.exported"), "info");
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  const cancelEditor = () => {
    cancelThemePreview();
    setEditor(null);
  };

  const saveEditor = async (activateAfter: boolean) => {
    if (!editor) return;
    setBusy(true);
    try {
      const input: ThemeSaveInput = {
        id: editor.id.trim(),
        name: editor.name.trim(),
        author: editor.author,
        description: editor.description,
        license: editor.license,
        baseStyle: editor.baseStyle,
        tokens: editor.tokens,
        recipes: editor.recipes,
        background: editor.background,
        backgroundDataUrl: editor.backgroundDataUrl || undefined,
        clearBackground: editor.background === null && editor.mode === "edit",
        replace: editor.mode === "edit",
        activate: activateAfter,
      };
      const saved = await app.SaveThemePack(input);
      commitThemePreview(activateAfter ? saved : (await app.GetActiveThemePack()).pack ?? null);
      setEditor(null);
      await reload();
      showToast(t("settings.themeLibrary.saved", { name: saved.name }), "info");
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  const updateEditor = (patch: Partial<EditorState>) => {
    setEditor((prev) => {
      if (!prev) return prev;
      const next = { ...prev, ...patch };
      schedulePreview(next);
      return next;
    });
  };

  const activeId = useMemo(() => packs.find((p) => p.active)?.id ?? "", [packs]);
  const groups = useMemo(() => {
    const official: ThemePackView[] = [];
    const base: ThemePackView[] = [];
    const user: ThemePackView[] = [];
    for (const p of packs) {
      const kind = themePackKind(p);
      if (kind === "official") official.push(p);
      else if (kind === "base") base.push(p);
      else user.push(p);
    }
    return { official, base, user };
  }, [packs]);

  return (
    <div className="theme-library">
      <div className="theme-library__toolbar">
        <button type="button" className="btn btn--small" disabled={busy} onClick={openCreate}>
          <Plus size={13} /> {t("settings.themeLibrary.new")}
        </button>
        <button type="button" className="btn btn--small" disabled={busy} onClick={() => void doImport(false)}>
          <Upload size={13} /> {t("settings.themeLibrary.import")}
        </button>
        <button type="button" className="btn btn--small theme-reset-btn" disabled={busy} onClick={() => void resetDefault()}>
          <RotateCcw size={13} /> {t("settings.themeLibrary.reset")}
        </button>
      </div>

      {loading ? (
        <div className="theme-lib-card__sub">{t("settings.themeLibrary.loading")}</div>
      ) : (
        <>
          {groups.official.length > 0 && (
            <section className="theme-library__group" data-group="official">
              <h4 className="theme-library__heading">{t("settings.themeLibrary.groupOfficial")}</h4>
              <div className="theme-library__grid theme-library__grid--official">
                {groups.official.map((pack) => (
                  <OfficialThemeCard
                    key={pack.id}
                    pack={pack}
                    active={pack.id === activeId}
                    busy={busy}
                    onActivate={() => void activate(pack)}
                    onCopy={() => void openCopy(pack)}
                  />
                ))}
              </div>
            </section>
          )}

          {groups.base.length > 0 && (
            <section className="theme-library__group" data-group="base">
              <h4 className="theme-library__heading">{t("settings.themeLibrary.groupBase")}</h4>
              <div className="theme-library__grid theme-library__grid--base">
                {groups.base.map((pack) => (
                  <ThemeLibCard
                    key={pack.id}
                    pack={pack}
                    active={pack.id === activeId}
                    busy={busy}
                    onActivate={() => void activate(pack)}
                    onEdit={() => openEdit(pack)}
                    onCopy={() => void openCopy(pack)}
                    onExport={() => void doExport(pack)}
                    onDelete={() => void remove(pack)}
                  />
                ))}
              </div>
            </section>
          )}

          <section className="theme-library__group" data-group="user">
            <h4 className="theme-library__heading">{t("settings.themeLibrary.groupUser")}</h4>
            {groups.user.length === 0 ? (
              <div className="theme-lib-card__sub">{t("settings.themeLibrary.emptyUser")}</div>
            ) : (
              <div className="theme-library__grid">
                {groups.user.map((pack) => (
                  <ThemeLibCard
                    key={pack.id}
                    pack={pack}
                    active={pack.id === activeId}
                    busy={busy}
                    onActivate={() => void activate(pack)}
                    onEdit={() => openEdit(pack)}
                    onCopy={() => void openCopy(pack)}
                    onExport={() => void doExport(pack)}
                    onDelete={() => void remove(pack)}
                  />
                ))}
              </div>
            )}
          </section>
        </>
      )}

      {editor && (
        <ThemeEditor
          state={editor}
          busy={busy}
          onChange={updateEditor}
          onCancel={cancelEditor}
          onSave={(activateAfter) => void saveEditor(activateAfter)}
        />
      )}
      {confirmDialog}
    </div>
  );
}

function packDisplayName(pack: ThemePackView, t: (key: never, vars?: Record<string, string | number>) => string): string {
  return pack.nameKey ? t(pack.nameKey as never) : pack.name;
}

function packDescription(pack: ThemePackView, t: (key: never, vars?: Record<string, string | number>) => string): string {
  if (pack.descriptionKey) return t(pack.descriptionKey as never);
  return pack.description || "";
}

function OfficialThemeCard({
  pack,
  active,
  busy,
  onActivate,
  onCopy,
}: {
  pack: ThemePackView;
  active: boolean;
  busy: boolean;
  onActivate: () => void;
  onCopy: () => void;
}) {
  const t = useT();
  const name = packDisplayName(pack, t);
  const desc = packDescription(pack, t);
  const lightBg = pack.tokens?.light?.bg || "#f4f3ef";
  const darkBg = pack.tokens?.dark?.bg || "#0c0d10";
  const accent = pack.tokens?.dark?.accent || pack.tokens?.light?.accent || "#ff6a3d";

  return (
    <div className={`theme-lib-card theme-lib-card--official${active ? " theme-lib-card--on" : ""}`}>
      <div className="theme-lib-card__thumb theme-lib-card__thumb--img">
        {pack.previewUrl ? (
          <img src={pack.previewUrl} alt={name} loading="lazy" decoding="async" />
        ) : (
          <div className="theme-lib-card__thumb-fallback" style={{ background: `linear-gradient(120deg, ${lightBg} 0%, ${lightBg} 55%, ${accent} 140%)` }} />
        )}
      </div>
      <div className="theme-lib-card__meta">
        <div className="theme-lib-card__name">
          {name} {active ? <Check size={12} style={{ display: "inline", verticalAlign: "middle" }} /> : null}
        </div>
        {desc ? <div className="theme-lib-card__desc">{desc}</div> : null}
        <div className="theme-lib-card__sub">
          {pack.license || "MIT"} · {pack.author || "Reasonix Contributors"}
        </div>
      </div>
      <div className="theme-lib-card__swatches" aria-hidden="true">
        <span className="theme-lib-card__swatch" style={{ background: lightBg }} />
        <span className="theme-lib-card__swatch" style={{ background: darkBg }} />
        <span className="theme-lib-card__swatch" style={{ background: accent }} />
      </div>
      <div className="theme-lib-card__actions">
        <button type="button" className="btn btn--small btn--primary" disabled={busy || active} onClick={onActivate}>
          {active ? t("settings.themeLibrary.active") : t("settings.themeLibrary.enable")}
        </button>
        <button type="button" className="btn btn--small" disabled={busy} onClick={onCopy}>
          <Copy size={12} /> {t("settings.themeLibrary.copyFrom")}
        </button>
      </div>
    </div>
  );
}

function ThemeLibCard({
  pack,
  active,
  busy,
  onActivate,
  onEdit,
  onCopy,
  onExport,
  onDelete,
}: {
  pack: ThemePackView;
  active: boolean;
  busy: boolean;
  onActivate: () => void;
  onEdit: () => void;
  onCopy: () => void;
  onExport: () => void;
  onDelete: () => void;
}) {
  const t = useT();
  const kind = themePackKind(pack);
  const lightBg = pack.tokens?.light?.bg || "#f4f3ef";
  const darkBg = pack.tokens?.dark?.bg || "#0c0d10";
  const accent = pack.tokens?.dark?.accent || pack.tokens?.light?.accent || "#ff6a3d";
  const thumbStyle: Record<string, string> = pack.backgroundUrl
    ? { backgroundImage: `url("${pack.backgroundUrl}")`, backgroundSize: "cover" }
    : { ["--thumb-light"]: lightBg, ["--thumb-dark"]: darkBg };

  return (
    <div className={`theme-lib-card${active ? " theme-lib-card--on" : ""}`}>
      <div className="theme-lib-card__thumb" style={thumbStyle} />
      <div className="theme-lib-card__meta">
        <div className="theme-lib-card__name">
          {pack.name} {active ? <Check size={12} style={{ display: "inline", verticalAlign: "middle" }} /> : null}
        </div>
        <div className="theme-lib-card__sub">
          {kind === "base" ? t("settings.themeLibrary.builtin") : pack.author || t("settings.themeLibrary.userTheme")}
          {" · "}
          {pack.baseStyle}
        </div>
      </div>
      <div className="theme-lib-card__swatches" aria-hidden="true">
        <span className="theme-lib-card__swatch" style={{ background: lightBg }} />
        <span className="theme-lib-card__swatch" style={{ background: darkBg }} />
        <span className="theme-lib-card__swatch" style={{ background: accent }} />
      </div>
      <div className="theme-lib-card__actions">
        <button type="button" className="btn btn--small btn--primary" disabled={busy || active} onClick={onActivate}>
          {active ? t("settings.themeLibrary.active") : t("settings.themeLibrary.enable")}
        </button>
        {kind === "user" && (
          <button type="button" className="btn btn--small" disabled={busy} onClick={onEdit} title={t("settings.themeLibrary.edit")}>
            <Pencil size={12} />
          </button>
        )}
        <button type="button" className="btn btn--small" disabled={busy} onClick={onCopy} title={t("settings.themeLibrary.copy")}>
          <Copy size={12} />
        </button>
        {kind === "user" && (
          <>
            <button type="button" className="btn btn--small" disabled={busy} onClick={onExport} title={t("settings.themeLibrary.export")}>
              <Download size={12} />
            </button>
            <button type="button" className="btn btn--small" disabled={busy} onClick={onDelete} title={t("settings.themeLibrary.delete")}>
              <Trash2 size={12} />
            </button>
          </>
        )}
      </div>
    </div>
  );
}

function ThemeEditor({
  state,
  busy,
  onChange,
  onCancel,
  onSave,
}: {
  state: EditorState;
  busy: boolean;
  onChange: (patch: Partial<EditorState>) => void;
  onCancel: () => void;
  onSave: (activate: boolean) => void;
}) {
  const t = useT();
  const { showToast } = useToast();
  const previewRef = useRef<HTMLDivElement>(null);
  const dragging = useRef(false);

  const setToken = (key: string, value: string) => {
    const mode = state.tokenMode;
    const nextTokens = {
      ...state.tokens,
      [mode]: { ...(state.tokens[mode] || {}), [key]: value },
    };
    // Allow empty to clear override
    if (!value) {
      const map = { ...(nextTokens[mode] || {}) };
      delete map[key];
      nextTokens[mode] = map;
    } else if (!isSafeHex(value) && value.length >= 7) {
      // Keep typing intermediate values without applying invalid hex to preview tokens fully
    }
    onChange({ tokens: nextTokens });
  };

  const pickBackground = async () => {
    try {
      const dataUrl = await app.PickThemeBackground();
      if (!dataUrl) return;
      const bg = state.background ? { ...state.background } : defaultBackground();
      onChange({
        background: bg,
        backgroundDataUrl: dataUrl,
        existingBackgroundUrl: "",
      });
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    }
  };

  const onFocusPointer = (clientX: number, clientY: number) => {
    const el = previewRef.current;
    if (!el || !state.background) return;
    const rect = el.getBoundingClientRect();
    const x = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width));
    const y = Math.min(1, Math.max(0, (clientY - rect.top) / rect.height));
    onChange({ background: { ...state.background, focusX: x, focusY: y } });
  };

  const bgUrl = state.backgroundDataUrl || state.existingBackgroundUrl;
  const warnings = useMemo(() => {
    // Client-side soft check mirroring backend pairs.
    const out: string[] = [];
    for (const mode of ["light", "dark"] as const) {
      const fg = state.tokens[mode]?.fg;
      const bg = state.tokens[mode]?.bg;
      if (fg && bg && isSafeHex(fg) && isSafeHex(bg)) {
        const ratio = contrastRatio(fg, bg);
        if (ratio < 4.5) out.push(`${mode} fg/bg ${ratio.toFixed(2)} < 4.5`);
      }
    }
    return out;
  }, [state.tokens]);

  return (
    <div className="theme-editor">
      <strong>{state.mode === "create" ? t("settings.themeLibrary.editorCreate") : t("settings.themeLibrary.editorEdit")}</strong>

      <div className="theme-editor__row">
        <div className="theme-editor__label">{t("settings.themeLibrary.fieldId")}</div>
        <div className="theme-editor__fields">
          <input
            value={state.id}
            disabled={state.mode === "edit" || busy}
            onChange={(e) => onChange({ id: e.target.value })}
          />
          <input
            value={state.name}
            disabled={busy}
            placeholder={t("settings.themeLibrary.fieldName")}
            onChange={(e) => onChange({ name: e.target.value })}
          />
          <input
            value={state.author}
            disabled={busy}
            placeholder={t("settings.themeLibrary.fieldAuthor")}
            onChange={(e) => onChange({ author: e.target.value })}
          />
        </div>
      </div>

      <div className="theme-editor__row">
        <div className="theme-editor__label">{t("settings.themeLibrary.fieldBase")}</div>
        <div className="set-seg">
          {THEME_STYLES.map((s) => (
            <button
              key={s}
              type="button"
              className={`set-seg__btn${state.baseStyle === s ? " set-seg__btn--on" : ""}`}
              disabled={busy}
              onClick={() => onChange({ baseStyle: s })}
            >
              {s}
            </button>
          ))}
        </div>
      </div>

      <div className="theme-editor__row">
        <div className="theme-editor__label">{t("settings.themeLibrary.fieldRecipes")}</div>
        <div className="theme-editor__fields">
          <div className="set-seg">
            {(["comfortable", "compact"] as const).map((d) => (
              <button
                key={d}
                type="button"
                className={`set-seg__btn${state.recipes.density === d ? " set-seg__btn--on" : ""}`}
                disabled={busy}
                onClick={() => onChange({ recipes: { ...state.recipes, density: d } })}
              >
                {d}
              </button>
            ))}
          </div>
          <div className="set-seg">
            {(["square", "soft", "round"] as const).map((c) => (
              <button
                key={c}
                type="button"
                className={`set-seg__btn${state.recipes.corners === c ? " set-seg__btn--on" : ""}`}
                disabled={busy}
                onClick={() => onChange({ recipes: { ...state.recipes, corners: c } })}
              >
                {c}
              </button>
            ))}
          </div>
        </div>
      </div>

      <div className="theme-editor__row">
        <div className="theme-editor__label">{t("settings.themeLibrary.fieldTokens")}</div>
        <div className="theme-editor__fields">
          <div className="set-seg">
            {(["dark", "light"] as const).map((m) => (
              <button
                key={m}
                type="button"
                className={`set-seg__btn${state.tokenMode === m ? " set-seg__btn--on" : ""}`}
                onClick={() => onChange({ tokenMode: m })}
              >
                {m}
              </button>
            ))}
          </div>
          {TOKEN_GROUPS.map((group) => (
            <div key={group.labelKey}>
              <div className="theme-lib-card__sub" style={{ marginBottom: 6 }}>{t(group.labelKey as never)}</div>
              <div className="theme-editor__color-grid">
                {group.keys.filter((k) => themeTokenKeys().includes(k)).map((key) => {
                  const val = state.tokens[state.tokenMode]?.[key] || "";
                  const colorVal = isSafeHex(val) ? val.slice(0, 7) : "#888888";
                  return (
                    <label key={key} className="theme-editor__color">
                      <span>{key}</span>
                      <input
                        type="color"
                        value={colorVal}
                        disabled={busy}
                        onChange={(e) => setToken(key, e.target.value)}
                      />
                      <input
                        type="text"
                        value={val}
                        placeholder="#RRGGBB"
                        disabled={busy}
                        onChange={(e) => setToken(key, e.target.value.trim())}
                      />
                    </label>
                  );
                })}
              </div>
            </div>
          ))}
        </div>
      </div>

      <div className="theme-editor__row">
        <div className="theme-editor__label">{t("settings.themeLibrary.fieldBackground")}</div>
        <div className="theme-editor__fields">
          <div className="theme-library__toolbar">
            <button type="button" className="btn btn--small" disabled={busy} onClick={() => void pickBackground()}>
              {t("settings.themeLibrary.pickImage")}
            </button>
            <button
              type="button"
              className="btn btn--small"
              disabled={busy || (!state.background && !bgUrl)}
              onClick={() => onChange({ background: null, backgroundDataUrl: "", existingBackgroundUrl: "" })}
            >
              {t("settings.themeLibrary.clearImage")}
            </button>
          </div>
          {state.background && (
            <>
              <div
                ref={previewRef}
                className="theme-editor__bg-preview"
                style={bgUrl ? { backgroundImage: `url("${bgUrl}")` } : undefined}
                onPointerDown={(e) => {
                  dragging.current = true;
                  (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
                  onFocusPointer(e.clientX, e.clientY);
                }}
                onPointerMove={(e) => {
                  if (!dragging.current) return;
                  onFocusPointer(e.clientX, e.clientY);
                }}
                onPointerUp={() => {
                  dragging.current = false;
                }}
              >
                <span
                  className="theme-editor__focus"
                  style={{ left: `${(state.background.focusX ?? 0.5) * 100}%`, top: `${(state.background.focusY ?? 0.5) * 100}%` }}
                />
              </div>
              <div className="set-seg">
                {(["left", "center", "right"] as const).map((s) => (
                  <button
                    key={s}
                    type="button"
                    className={`set-seg__btn${state.background?.safeArea === s ? " set-seg__btn--on" : ""}`}
                    onClick={() => onChange({ background: { ...state.background!, safeArea: s } })}
                  >
                    {s}
                  </button>
                ))}
              </div>
              <label className="theme-editor__color">
                {t("settings.themeLibrary.homeOpacity")}
                <input
                  type="range"
                  min={0}
                  max={1}
                  step={0.01}
                  value={state.background.homeOpacity}
                  onChange={(e) => onChange({ background: { ...state.background!, homeOpacity: Number(e.target.value) } })}
                />
              </label>
              <label className="theme-editor__color">
                {t("settings.themeLibrary.taskOpacity")}
                <input
                  type="range"
                  min={0}
                  max={0.45}
                  step={0.01}
                  value={state.background.taskOpacity}
                  onChange={(e) => onChange({ background: { ...state.background!, taskOpacity: Number(e.target.value) } })}
                />
              </label>
              <label className="theme-editor__color">
                {t("settings.themeLibrary.overlayStrength")}
                <input
                  type="range"
                  min={0}
                  max={1}
                  step={0.01}
                  value={state.background.overlayStrength}
                  onChange={(e) => onChange({ background: { ...state.background!, overlayStrength: Number(e.target.value) } })}
                />
              </label>
            </>
          )}
        </div>
      </div>

      {warnings.length > 0 && (
        <div className="theme-editor__warn">
          {t("settings.themeLibrary.contrastWarn")}
          <ul style={{ margin: "6px 0 0", paddingLeft: 18 }}>
            {warnings.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        </div>
      )}

      <div className="theme-editor__actions">
        <button type="button" className="btn btn--small" disabled={busy} onClick={onCancel}>
          {t("settings.themeLibrary.cancel")}
        </button>
        <button type="button" className="btn btn--small" disabled={busy} onClick={() => onSave(false)}>
          {t("settings.themeLibrary.save")}
        </button>
        <button type="button" className="btn btn--small btn--primary" disabled={busy} onClick={() => onSave(true)}>
          {t("settings.themeLibrary.saveEnable")}
        </button>
      </div>
    </div>
  );
}

function contrastRatio(a: string, b: string): number {
  const la = relativeLuminance(a);
  const lb = relativeLuminance(b);
  const lighter = Math.max(la, lb);
  const darker = Math.min(la, lb);
  return (lighter + 0.05) / (darker + 0.05);
}

function relativeLuminance(hex: string): number {
  const n = hex.replace("#", "");
  const r = parseInt(n.slice(0, 2), 16) / 255;
  const g = parseInt(n.slice(2, 4), 16) / 255;
  const b = parseInt(n.slice(4, 6), 16) / 255;
  const lin = (c: number) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}
