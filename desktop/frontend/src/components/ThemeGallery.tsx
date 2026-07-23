import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { ArrowLeft, Check, CircleHelp, Copy, Download, ImagePlus, MoreHorizontal, Pencil, Plus, Trash2, Upload, X } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { THEME_STYLES, type ThemeStyle, isThemeStyle } from "../lib/theme";
import {
  type ThemePackView,
  type ThemeSaveInput,
  type ThemePackBackground,
  type ThemePackSceneBackground,
  type ThemePackRecipes,
  type ThemePackTokens,
  beginThemePreview,
  cancelThemePreview,
  defaultBackground,
  defaultTaskBackground,
  draftPackView,
  emptyThemeTokens,
  isSafeHex,
  themePackKind,
  themeTokenKeys,
} from "../lib/themePack";
import {
  type GalleryTab,
  type ThemeExperienceView,
  type ThemeSelection,
  activateBaseStyle,
  activateThemePack,
  cancelGlobalPreview,
  commitGlobalPreview,
  groupThemePacks,
  isSelectionActive,
  selectionFromPack,
  startGlobalPreview,
} from "../lib/themeExperience";
import { useToast } from "../lib/toast";
import { themePreviewPalette } from "../lib/themePreviewPalette";
import { useConfirmDialog } from "./ConfirmDialog";
import { ThemePreviewSurface } from "./ThemePreviewSurface";
import { Tooltip } from "./Tooltip";

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
  taskBackground: ThemePackSceneBackground | null;
  taskBackgroundDataUrl: string;
  existingTaskBackgroundUrl: string;
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

function packDisplayName(pack: ThemePackView, t: (key: never, vars?: Record<string, string | number>) => string): string {
  return pack.nameKey ? t(pack.nameKey as never) : pack.name;
}

function packDescription(pack: ThemePackView, t: (key: never, vars?: Record<string, string | number>) => string): string {
  if (pack.descriptionKey) return t(pack.descriptionKey as never);
  return pack.description || "";
}

function createEditorState(baseStyle: ThemeStyle): EditorState {
  return {
    mode: "create",
    id: "my-theme",
    name: "My Theme",
    author: "",
    description: "",
    license: "",
    baseStyle,
    tokens: emptyThemeTokens(),
    recipes: { density: "comfortable", corners: "soft" },
    background: null,
    backgroundDataUrl: "",
    existingBackgroundUrl: "",
    taskBackground: null,
    taskBackgroundDataUrl: "",
    existingTaskBackgroundUrl: "",
    tokenMode: "dark",
    originalId: "",
  };
}

function handlePreviewRadioKey<T extends string>(
  event: ReactKeyboardEvent<HTMLButtonElement>,
  values: readonly T[],
  current: T,
  onChange: (value: T) => void,
) {
  const direction =
    event.key === "ArrowRight" || event.key === "ArrowDown"
      ? 1
      : event.key === "ArrowLeft" || event.key === "ArrowUp"
        ? -1
        : 0;
  if (!direction) return;
  event.preventDefault();
  const currentIndex = values.indexOf(current);
  const nextIndex = (currentIndex + direction + values.length) % values.length;
  onChange(values[nextIndex]);
  const radios = event.currentTarget
    .closest('[role="radiogroup"]')
    ?.querySelectorAll<HTMLButtonElement>('[role="radio"]');
  radios?.[nextIndex]?.focus();
}

function ThemePreviewControls({
  mode,
  scene,
  onModeChange,
  onSceneChange,
}: {
  mode: "light" | "dark";
  scene: "home" | "task";
  onModeChange: (mode: "light" | "dark") => void;
  onSceneChange: (scene: "home" | "task") => void;
}) {
  const t = useT();
  return (
    <div className="theme-gallery__preview-controls">
      <div className="theme-gallery__preview-control">
        <span className="theme-gallery__preview-label">{t("settings.themeGallery.appearancePreview")}</span>
        <div className="set-seg" role="radiogroup" aria-label={t("settings.themeGallery.appearancePreview")}>
          <button
            type="button"
            role="radio"
            aria-checked={mode === "light"}
            tabIndex={mode === "light" ? 0 : -1}
            className={`set-seg__btn${mode === "light" ? " set-seg__btn--on" : ""}`}
            onClick={() => onModeChange("light")}
            onKeyDown={(event) => handlePreviewRadioKey(event, ["light", "dark"], mode, onModeChange)}
          >
            {t("settings.themeLight")}
          </button>
          <button
            type="button"
            role="radio"
            aria-checked={mode === "dark"}
            tabIndex={mode === "dark" ? 0 : -1}
            className={`set-seg__btn${mode === "dark" ? " set-seg__btn--on" : ""}`}
            onClick={() => onModeChange("dark")}
            onKeyDown={(event) => handlePreviewRadioKey(event, ["light", "dark"], mode, onModeChange)}
          >
            {t("settings.themeDark")}
          </button>
        </div>
      </div>
      <div className="theme-gallery__preview-control">
        <span className="theme-gallery__preview-label">
          {t("settings.themeGallery.scenePreview")}
          <Tooltip label={t("settings.themeGallery.scenePreviewHint")} side="top">
            <button
              type="button"
              className="theme-gallery__preview-help"
              aria-label={t("settings.themeGallery.scenePreviewHint")}
            >
              <CircleHelp size={13} aria-hidden="true" />
            </button>
          </Tooltip>
        </span>
        <div className="set-seg" role="radiogroup" aria-label={t("settings.themeGallery.scenePreview")}>
          <button
            type="button"
            role="radio"
            aria-checked={scene === "home"}
            tabIndex={scene === "home" ? 0 : -1}
            className={`set-seg__btn${scene === "home" ? " set-seg__btn--on" : ""}`}
            onClick={() => onSceneChange("home")}
            onKeyDown={(event) => handlePreviewRadioKey(event, ["home", "task"], scene, onSceneChange)}
          >
            {t("settings.themeGallery.sceneHome")}
          </button>
          <button
            type="button"
            role="radio"
            aria-checked={scene === "task"}
            tabIndex={scene === "task" ? 0 : -1}
            className={`set-seg__btn${scene === "task" ? " set-seg__btn--on" : ""}`}
            onClick={() => onSceneChange("task")}
            onKeyDown={(event) => handlePreviewRadioKey(event, ["home", "task"], scene, onSceneChange)}
          >
            {t("settings.themeGallery.sceneTask")}
          </button>
        </div>
      </div>
    </div>
  );
}

export function ThemeGallery({
  experience,
  initialCreateBaseStyle,
  onExperienceChange,
  onBack,
}: {
  experience: ThemeExperienceView;
  initialCreateBaseStyle?: ThemeStyle;
  onExperienceChange: (view: ThemeExperienceView) => void;
  onBack: () => void;
}) {
  const t = useT();
  const { showToast } = useToast();
  const { confirm, dialog: confirmDialog } = useConfirmDialog();
  const [packs, setPacks] = useState<ThemePackView[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [tab, setTab] = useState<GalleryTab>("catalog");
  const [selected, setSelected] = useState<ThemeSelection | null>(null);
  const [detailMode, setDetailMode] = useState<"light" | "dark">("dark");
  const [detailScene, setDetailScene] = useState<"home" | "task">("home");
  const [immersive, setImmersive] = useState(false);
  const [previewingId, setPreviewingId] = useState<string | null>(null);
  const [editor, setEditor] = useState<EditorState | null>(() =>
    initialCreateBaseStyle ? createEditorState(initialCreateBaseStyle) : null,
  );
  const [menuOpen, setMenuOpen] = useState(false);
  const selectionSeeded = useRef(false);
  const moreActionsRef = useRef<HTMLButtonElement>(null);

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const list = await app.ListThemePacks();
      setPacks(list || []);
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setLoading(false);
    }
  }, [showToast]);

  useEffect(() => {
    void reload();
    return () => {
      cancelGlobalPreview();
      cancelThemePreview();
    };
  }, [reload]);

  const groups = useMemo(() => groupThemePacks(packs), [packs]);
  const catalogPacks = useMemo(() => [...groups.official, ...groups.base], [groups.official, groups.base]);
  const visible = tab === "catalog" ? catalogPacks : groups.user;
  const visibleSections =
    tab === "catalog"
      ? [
          { id: "official", label: t("settings.themeGallery.sectionFlagship"), packs: groups.official },
          { id: "base", label: t("settings.themeGallery.tabBase"), packs: groups.base },
        ]
      : [{ id: "user", label: "", packs: groups.user }];

  const changeTab = (nextTab: GalleryTab) => {
    const nextPacks = nextTab === "catalog" ? catalogPacks : groups.user;
    if (nextTab !== tab) {
      cancelGlobalPreview();
      setPreviewingId(null);
    }
    setTab(nextTab);
    setMenuOpen(false);
    if (selected && nextPacks.some((pack) => pack.id === selected.id)) return;
    const nextSelection =
      nextPacks.find((pack) => isSelectionActive(selectionFromPack(pack), experience)) || nextPacks[0] || null;
    setSelected(nextSelection ? selectionFromPack(nextSelection) : null);
  };

  // Seed selection from active experience.
  useEffect(() => {
    if (selectionSeeded.current || packs.length === 0) return;
    selectionSeeded.current = true;
    if (experience.activePack) {
      setSelected(selectionFromPack(experience.activePack));
      setTab(themePackKind(experience.activePack) === "user" ? "user" : "catalog");
      return;
    }
    const base = groups.base.find((p) => p.id === experience.baseStyle) || groups.base[0] || groups.official[0];
    if (base) {
      setSelected(selectionFromPack(base));
      setTab("catalog");
    }
  }, [packs, experience, selected, groups.base, groups.official]);

  const selectedPack = selected?.pack || (selected?.kind === "base" ? groups.base.find((p) => p.id === selected.id) : null) || null;
  const isActive = isSelectionActive(selected, experience);

  const previewPackGlobally = useCallback((pack: ThemePackView) => {
    if (themePackKind(pack) === "base") {
      const draft = draftPackView({
        id: pack.id,
        name: pack.name,
        baseStyle: pack.id,
        tokens: emptyThemeTokens(),
        recipes: { density: "comfortable", corners: "soft" },
      });
      startGlobalPreview(draft);
      return;
    }
    startGlobalPreview(pack);
  }, []);

  // Entering immersive mode is the explicit opt-in to a live, reversible
  // preview. Every rail selection replaces that preview until Back restores it
  // or Apply persists it.
  useEffect(() => {
    if (!immersive || !selectedPack) return;
    previewPackGlobally(selectedPack);
  }, [immersive, selectedPack, previewPackGlobally]);

  const onSelectPack = (pack: ThemePackView) => {
    setSelected(selectionFromPack(pack));
    setMenuOpen(false);
    if (!immersive) {
      previewPackGlobally(pack);
      setPreviewingId(pack.id);
    }
  };

  const applySelected = async () => {
    if (!selected) return;
    setBusy(true);
    try {
      if (selected.kind === "base") {
        const view = await activateBaseStyle(selected.id);
        onExperienceChange(view);
        showToast(t("settings.themeGallery.appliedBase", { name: selected.id }), "info");
      } else {
        const view = await activateThemePack(selected.id);
        onExperienceChange(view);
        commitGlobalPreview(view.activePack ?? null);
        showToast(t("settings.themeGallery.applied", { name: packDisplayName(selected.pack, t) }), "info");
      }
      setPreviewingId(null);
      await reload();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  const closeImmersivePreview = () => {
    cancelGlobalPreview();
    setPreviewingId(null);
    setImmersive(false);
  };

  const copySelected = async () => {
    if (!selectedPack) return;
    setBusy(true);
    try {
      const newId = slugifyId(`${selectedPack.id}-copy`);
      const created = await app.CopyThemePack(selectedPack.id, newId, `${packDisplayName(selectedPack, t)} Copy`);
      showToast(t("settings.themeLibrary.copied", { name: created.name }), "info");
      await reload();
      cancelGlobalPreview();
      setPreviewingId(null);
      setTab("user");
      setSelected(selectionFromPack(created));
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  const removeSelected = async () => {
    if (!selectedPack || themePackKind(selectedPack) !== "user") return;
    const ok = await confirm({
      title: t("settings.themeLibrary.confirmDeleteTitle"),
      message: t("settings.themeLibrary.confirmDelete", { name: packDisplayName(selectedPack, t) }),
      confirmLabel: t("common.delete"),
      cancelLabel: t("common.cancel"),
      tone: "danger",
    });
    setMenuOpen(false);
    if (!ok) {
      requestAnimationFrame(() => moreActionsRef.current?.focus());
      return;
    }
    setBusy(true);
    try {
      await app.DeleteThemePack(selectedPack.id);
      showToast(t("settings.themeGallery.deleted", { name: packDisplayName(selectedPack, t) }), "info");
      setSelected(null);
      await reload();
      // Experience may have changed if we deleted the active pack.
      const { loadThemeExperience, applyExperienceToDOM } = await import("../lib/themeExperience");
      const view = await loadThemeExperience();
      applyExperienceToDOM(view);
      onExperienceChange(view);
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
      setMenuOpen(false);
    }
  };

  const exportSelected = async () => {
    if (!selectedPack || themePackKind(selectedPack) !== "user") return;
    try {
      const ok = await confirm({
        title: t("settings.themeLibrary.exportRightsTitle"),
        message: t("settings.themeLibrary.exportRights"),
        confirmLabel: t("settings.themeLibrary.exportConfirm"),
        cancelLabel: t("common.cancel"),
      });
      if (!ok) return;
      const path = await app.ExportThemePack(selectedPack.id, "");
      if (path) showToast(t("settings.themeLibrary.exported"), "info");
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    }
    setMenuOpen(false);
  };

  const openCreate = () => {
    cancelGlobalPreview();
    setPreviewingId(null);
    setEditor(createEditorState(isThemeStyle(experience.baseStyle) ? experience.baseStyle : "graphite"));
  };

  const openEdit = () => {
    if (!selectedPack || themePackKind(selectedPack) !== "user") return;
    cancelGlobalPreview();
    setPreviewingId(null);
    setEditor({
      mode: "edit",
      id: selectedPack.id,
      name: selectedPack.name,
      author: selectedPack.author || "",
      description: selectedPack.description || "",
      license: selectedPack.license || "",
      baseStyle: isThemeStyle(selectedPack.baseStyle) ? selectedPack.baseStyle : "graphite",
      tokens: { light: { ...(selectedPack.tokens?.light || {}) }, dark: { ...(selectedPack.tokens?.dark || {}) } },
      recipes: {
        density: selectedPack.recipes?.density === "compact" ? "compact" : "comfortable",
        corners:
          selectedPack.recipes?.corners === "square" || selectedPack.recipes?.corners === "round"
            ? selectedPack.recipes.corners
            : "soft",
      },
      background: selectedPack.background ? { ...selectedPack.background } : null,
      backgroundDataUrl: "",
      existingBackgroundUrl: selectedPack.backgroundUrl || "",
      taskBackground: selectedPack.taskBackground ? { ...selectedPack.taskBackground } : null,
      taskBackgroundDataUrl: "",
      existingTaskBackgroundUrl: selectedPack.taskBackgroundUrl || "",
      tokenMode: "dark",
      originalId: selectedPack.id,
    });
    setMenuOpen(false);
  };

  const doImport = async () => {
    setBusy(true);
    try {
      const result = await app.ImportThemePack("", false);
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
        if (confirmed?.pack) {
          showToast(t("settings.themeLibrary.imported", { name: confirmed.pack.name }), "info");
        }
      } else if (result.pack) {
        showToast(t("settings.themeLibrary.imported", { name: result.pack.name }), "info");
      }
      await reload();
      cancelGlobalPreview();
      setPreviewingId(null);
      setTab("user");
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  const saveEditor = async (activate: boolean) => {
    if (!editor) return;
    setBusy(true);
    try {
      const input: ThemeSaveInput = {
        id: editor.id,
        name: editor.name,
        author: editor.author,
        description: editor.description,
        license: editor.license,
        baseStyle: editor.baseStyle,
        tokens: editor.tokens,
        recipes: editor.recipes,
        background: editor.background || undefined,
        backgroundDataUrl: editor.backgroundDataUrl || undefined,
        clearBackground: !editor.background && !editor.backgroundDataUrl && !editor.existingBackgroundUrl,
        taskBackground: editor.taskBackground || undefined,
        taskBackgroundDataUrl: editor.taskBackgroundDataUrl || undefined,
        clearTaskBackground: !editor.taskBackground && !editor.taskBackgroundDataUrl && !editor.existingTaskBackgroundUrl,
        replace: editor.mode === "edit",
        // Keep save and activation separate. The activation path below is the
        // sole owner of the active-theme pointer, so a failed activation leaves
        // the previous theme selected and the preview snapshot reversible.
        activate: false,
      };
      const saved = await app.SaveThemePack(input);
      showToast(t("settings.themeLibrary.saved", { name: saved.name }), "info");
      if (activate) {
        // Commit activation before unmounting the editor. ThemeEditorInline's
        // cleanup cancels any remaining preview, so closing it earlier would
        // briefly restore the old snapshot while reload() is in flight.
        const view = await activateThemePack(saved.id);
        onExperienceChange(view);
      } else {
        cancelThemePreview();
      }
      setEditor(null);
      await reload();
      setTab("user");
      setSelected(selectionFromPack(saved));
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusy(false);
    }
  };

  if (immersive && selectedPack) {
    return (
      <div className="theme-gallery theme-gallery--immersive">
        <header className="theme-gallery__top">
          <button type="button" className="btn btn--small" onClick={closeImmersivePreview}>
            <ArrowLeft size={14} /> {t("settings.themeGallery.back")}
          </button>
          <h2 className="theme-gallery__title">{t("settings.themeGallery.previewTitle")}</h2>
          <div className="theme-gallery__top-actions">
            <button type="button" className="btn btn--small" onClick={() => void doImport()} disabled={busy}>
              <Upload size={13} /> {t("settings.themeLibrary.import")}
            </button>
            <button type="button" className="btn btn--small" onClick={openCreate} disabled={busy}>
              <Plus size={13} /> {t("settings.themeLibrary.new")}
            </button>
          </div>
        </header>
        <div className="theme-gallery__immersive-toolbar">
          <ThemePreviewControls
            mode={detailMode}
            scene={detailScene}
            onModeChange={setDetailMode}
            onSceneChange={setDetailScene}
          />
        </div>
        <div className="theme-gallery__immersive-body">
          <ThemePreviewSurface pack={selectedPack} mode={detailMode} scene={detailScene} />
          <aside className="theme-gallery__immersive-rail">
            <div className="theme-gallery__detail-head">
              <div className="theme-gallery__detail-title-row">
                <h3>{packDisplayName(selectedPack, t)}</h3>
                {isActive ? (
                  <span className="theme-gallery__detail-status">
                    <Check size={12} strokeWidth={3} /> {t("settings.themeGallery.current")}
                  </span>
                ) : null}
              </div>
              <span className="theme-gallery__badge">
                {themePackKind(selectedPack) === "official"
                  ? t("settings.themeGallery.kindOfficial")
                  : themePackKind(selectedPack) === "base"
                    ? t("settings.themeGallery.kindBase")
                    : t("settings.themeGallery.kindUser")}
              </span>
            </div>
            <p className="theme-gallery__detail-desc">{packDescription(selectedPack, t)}</p>
            {!isActive ? (
              <button type="button" className="btn btn--primary" disabled={busy} onClick={() => void applySelected()}>
                {t("settings.themeGallery.apply")}
              </button>
            ) : null}
            <div className="theme-gallery__rail-list" role="listbox" aria-label={t("settings.themeGallery.title")}>
              {[
                { id: "official", label: t("settings.themeLibrary.groupOfficial"), packs: groups.official },
                { id: "user", label: t("settings.themeLibrary.groupUser"), packs: groups.user },
                { id: "base", label: t("settings.themeGallery.tabBase"), packs: groups.base },
              ]
                .filter((section) => section.packs.length > 0)
                .map((section) => (
                  <div key={section.id} className="theme-gallery__rail-section" role="group" aria-label={section.label}>
                    <div className="theme-gallery__rail-section-head" aria-hidden="true">
                      <span>{section.label}</span>
                      <span className="theme-gallery__rail-section-count">{section.packs.length}</span>
                    </div>
                    <div className="theme-gallery__rail-section-items">
                      {section.packs.map((p) => (
                        <button
                          key={p.id}
                          type="button"
                          role="option"
                          aria-selected={selected?.id === p.id}
                          className={`theme-gallery__rail-card${selected?.id === p.id ? " theme-gallery__rail-card--on" : ""}`}
                          onClick={() => onSelectPack(p)}
                        >
                          {themePackKind(p) === "base" ? (
                            <ThemePreviewSurface pack={p} mode={detailMode} scene={detailScene} variant="thumbnail" />
                          ) : p.previewUrl || p.backgroundUrl ? (
                            <img src={p.previewUrl || p.backgroundUrl} alt="" loading="lazy" />
                          ) : (
                            <span className="theme-gallery__rail-fallback" />
                          )}
                          <span>{packDisplayName(p, t)}</span>
                        </button>
                      ))}
                    </div>
                  </div>
                ))}
            </div>
          </aside>
        </div>
        {confirmDialog}
      </div>
    );
  }

  return (
    <div className="theme-gallery">
      <header className="theme-gallery__top">
        <div className="theme-gallery__crumbs">
          <button type="button" className="theme-gallery__back" onClick={onBack}>
            <ArrowLeft size={14} />
            <span>
              {t("settings.appearance")} / {t("settings.themeGallery.title")}
            </span>
          </button>
          <h2 className="theme-gallery__title">{t("settings.themeGallery.title")}</h2>
          <p className="theme-gallery__sub">{t("settings.themeGallery.subtitle")}</p>
        </div>
        <div className="theme-gallery__top-actions">
          <button type="button" className="btn btn--small" onClick={openCreate} disabled={busy}>
            <Plus size={13} /> {t("settings.themeLibrary.new")}
          </button>
          <button type="button" className="btn btn--small" onClick={() => void doImport()} disabled={busy}>
            <Upload size={13} /> {t("settings.themeLibrary.import")}
          </button>
        </div>
      </header>

      <div className="theme-gallery__tabs" role="tablist">
        {(
          [
            ["catalog", t("settings.themeGallery.tabAll"), catalogPacks.length],
            ["user", t("settings.themeLibrary.groupUser"), groups.user.length],
          ] as const
        ).map(([id, label, count]) => (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={tab === id}
            className={`theme-gallery__tab${tab === id ? " theme-gallery__tab--on" : ""}`}
            onClick={() => changeTab(id)}
          >
            {label} <span className="theme-gallery__tab-count">{count}</span>
          </button>
        ))}
      </div>

      <div className="theme-gallery__body">
        <div className="theme-gallery__grid" role="listbox" aria-label={t("settings.themeGallery.title")}>
          {loading ? (
            <div className="theme-lib-card__sub">{t("settings.themeLibrary.loading")}</div>
          ) : visible.length === 0 ? (
            <div className="theme-lib-card__sub">{tab === "user" ? t("settings.themeLibrary.emptyUser") : t("settings.themeGallery.empty")}</div>
          ) : (
            visibleSections.map((section) => (
              <div key={section.id} className="theme-gallery__grid-section" role="group" aria-label={section.label || undefined}>
                {section.label ? (
                  <div className="theme-gallery__section-head">
                    <h3>{section.label}</h3>
                    <span>{section.packs.length}</span>
                  </div>
                ) : null}
                {section.packs.map((pack) => {
                  const name = packDisplayName(pack, t);
                  const active = isSelectionActive(selectionFromPack(pack), experience);
                  const sel = selected?.id === pack.id;
                  return (
                    <button
                      key={pack.id}
                      type="button"
                      role="option"
                      aria-selected={sel}
                      className={`theme-gallery-card${sel ? " theme-gallery-card--selected" : ""}${active ? " theme-gallery-card--active" : ""}`}
                      onClick={() => onSelectPack(pack)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          onSelectPack(pack);
                        }
                      }}
                    >
                      <div className="theme-gallery-card__thumb">
                        {themePackKind(pack) === "base" ? (
                          <ThemePreviewSurface pack={pack} mode={detailMode} scene={detailScene} variant="thumbnail" />
                        ) : pack.previewUrl || pack.backgroundUrl ? (
                          <img src={pack.previewUrl || pack.backgroundUrl} alt={name} loading="lazy" decoding="async" />
                        ) : (
                          <div
                            className="theme-gallery-card__swatches"
                            style={{ background: `linear-gradient(120deg, ${pack.tokens?.light?.bg || "#f4f3ef"}, ${pack.tokens?.dark?.accent || pack.tokens?.light?.accent || "#ff6a3d"})` }}
                          />
                        )}
                        {active ? (
                          <span className="theme-gallery-card__check" aria-hidden="true">
                            <Check size={14} strokeWidth={3} />
                          </span>
                        ) : null}
                      </div>
                      <div className="theme-gallery-card__name">{name}</div>
                      {active ? <div className="theme-gallery-card__status">{t("settings.themeGallery.current")}</div> : null}
                    </button>
                  );
                })}
              </div>
            ))
          )}
        </div>

        <aside className="theme-gallery__detail" aria-live="polite">
          {selectedPack ? (
            <>
              <div className="theme-gallery__detail-preview">
                <ThemePreviewSurface pack={selectedPack} mode={detailMode} scene={detailScene} />
              </div>
              <div className="theme-gallery__detail-meta">
                <div className="theme-gallery__detail-head">
                  <div className="theme-gallery__detail-title-row">
                    <h3 className="theme-gallery__detail-name">{packDisplayName(selectedPack, t)}</h3>
                    {isActive ? (
                      <span className="theme-gallery__detail-status">
                        <Check size={12} strokeWidth={3} /> {t("settings.themeGallery.current")}
                      </span>
                    ) : previewingId === selectedPack.id ? (
                      <span className="theme-gallery__detail-status theme-gallery__detail-status--preview">
                        {t("settings.themeGallery.previewing")}
                      </span>
                    ) : null}
                  </div>
                </div>
                <div className="theme-gallery__detail-tags">
                  <span className="theme-gallery__badge">
                    {themePackKind(selectedPack) === "official"
                      ? t("settings.themeGallery.kindOfficial")
                      : themePackKind(selectedPack) === "base"
                        ? t("settings.themeGallery.kindBase")
                        : t("settings.themeGallery.kindUser")}
                  </span>
                  {selectedPack.license ? <span className="theme-gallery__badge theme-gallery__badge--muted">{selectedPack.license}</span> : null}
                </div>
                <p className="theme-gallery__detail-desc">{packDescription(selectedPack, t)}</p>
                <div className="theme-gallery__detail-palette">
                  <span className="theme-gallery__preview-label">{t("settings.themeGallery.paletteLabel")}</span>
                  <div className="theme-gallery__detail-swatches" aria-hidden="true">
                    <span style={{ background: themePreviewPalette(selectedPack, detailMode).bg }} />
                    <span style={{ background: themePreviewPalette(selectedPack, detailMode).panel }} />
                    <span style={{ background: themePreviewPalette(selectedPack, detailMode).accent }} />
                  </div>
                </div>
                <ThemePreviewControls
                  mode={detailMode}
                  scene={detailScene}
                  onModeChange={setDetailMode}
                  onSceneChange={setDetailScene}
                />
                {!isActive ? (
                  <button type="button" className="btn btn--primary theme-gallery__apply" disabled={busy} onClick={() => void applySelected()}>
                    {t("settings.themeGallery.apply")}
                  </button>
                ) : null}
                <div className="theme-gallery__detail-actions">
                  <button type="button" className="btn btn--small theme-gallery__open-preview" disabled={busy} onClick={() => setImmersive(true)}>
                    {t("settings.themeGallery.openPreview")}
                  </button>
                  {themePackKind(selectedPack) === "user" ? (
                    <div className="theme-gallery__detail-user-actions">
                      <button type="button" className="btn btn--small" disabled={busy} onClick={openEdit}>
                        <Pencil size={12} /> {t("settings.themeLibrary.edit")}
                      </button>
                      <button type="button" className="btn btn--small" disabled={busy} onClick={() => void exportSelected()}>
                        <Download size={12} /> {t("settings.themeLibrary.export")}
                      </button>
                    </div>
                  ) : null}
                  <button type="button" className="btn btn--small theme-gallery__detail-copy" disabled={busy} onClick={() => void copySelected()}>
                    <Copy size={12} /> {t("settings.themeLibrary.copyFrom")}
                  </button>
                  {themePackKind(selectedPack) === "user" ? (
                    <div className="theme-gallery__more theme-gallery__detail-more">
                      <button ref={moreActionsRef} type="button" className="btn btn--small" onClick={() => setMenuOpen((v) => !v)} aria-expanded={menuOpen}>
                        <MoreHorizontal size={14} /> {t("settings.themeGallery.moreActions")}
                      </button>
                      {menuOpen ? (
                        <div className="theme-gallery__menu" role="menu">
                          <button type="button" role="menuitem" className="theme-gallery__menu-danger" onClick={() => void removeSelected()}>
                            <Trash2 size={12} /> {t("settings.themeLibrary.delete")}
                          </button>
                        </div>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              </div>
            </>
          ) : (
            <div className="theme-lib-card__sub">{t("settings.themeGallery.selectHint")}</div>
          )}
        </aside>
      </div>

      {editor ? (
        <ThemeEditorInline
          state={editor}
          busy={busy}
          onChange={(patch) => setEditor((s) => (s ? { ...s, ...patch } : s))}
          onCancel={() => {
            cancelThemePreview();
            setEditor(null);
          }}
          onSave={(activate) => void saveEditor(activate)}
        />
      ) : null}
      {confirmDialog}
    </div>
  );
}

function ThemeEditorInline({
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
  const [previewMode, setPreviewMode] = useState<"light" | "dark">("dark");
  const [previewScene, setPreviewScene] = useState<"home" | "task">("home");
  const titleId = useId();
  const editorRef = useRef<HTMLDivElement>(null);
  const initialFocusRef = useRef<HTMLInputElement>(null);
  const restoreFocusRef = useRef<HTMLElement | null>(null);
  const busyRef = useRef(busy);
  const onCancelRef = useRef(onCancel);
  busyRef.current = busy;
  onCancelRef.current = onCancel;

  const homeUrl = state.backgroundDataUrl || state.existingBackgroundUrl;
  const taskUrl = state.taskBackgroundDataUrl || state.existingTaskBackgroundUrl;
  const draft = useMemo(
    () =>
      draftPackView({
        id: state.id || "preview",
        name: state.name || "Preview",
        baseStyle: state.baseStyle,
        tokens: state.tokens,
        recipes: state.recipes,
        background: state.background,
        backgroundUrl: homeUrl,
        taskBackground: state.taskBackground,
        taskBackgroundUrl: taskUrl,
      }),
    [homeUrl, state.baseStyle, state.background, state.id, state.name, state.recipes, state.taskBackground, state.tokens, taskUrl],
  );

  useEffect(() => {
    beginThemePreview(draft);
  }, [draft]);

  useEffect(() => () => cancelThemePreview(), []);

  useLayoutEffect(() => {
    restoreFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const initialFocus = initialFocusRef.current && !initialFocusRef.current.disabled
      ? initialFocusRef.current
      : editorRef.current?.querySelector<HTMLElement>('input:not([disabled]), textarea:not([disabled]), button:not([disabled])');
    (initialFocus || editorRef.current)?.focus();
    return () => {
      if (restoreFocusRef.current?.isConnected) restoreFocusRef.current.focus();
    };
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        if (!busyRef.current) onCancelRef.current();
        return;
      }
      if (event.key !== "Tab") return;
      const focusable = Array.from(
        editorRef.current?.querySelectorAll<HTMLElement>(
          'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
        ) || [],
      ).filter((element) => !element.hidden && element.getAttribute("aria-hidden") !== "true");
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown, { capture: true });
    return () => document.removeEventListener("keydown", onKeyDown, { capture: true });
  }, []);

  const setToken = (key: string, value: string) => {
    const next = {
      ...state.tokens,
      [state.tokenMode]: { ...(state.tokens[state.tokenMode] || {}) },
    };
    if (value) next[state.tokenMode]![key] = value;
    else delete next[state.tokenMode]![key];
    onChange({ tokens: next });
  };

  const pickBackground = async (scene: "home" | "task") => {
    try {
      const dataUrl = await app.PickThemeBackground();
      if (!dataUrl) return;
      if (scene === "home") {
        onChange({
          background: state.background ? { ...state.background } : defaultBackground(),
          backgroundDataUrl: dataUrl,
          existingBackgroundUrl: "",
        });
      } else {
        onChange({
          taskBackground: state.taskBackground ? { ...state.taskBackground } : defaultTaskBackground(),
          taskBackgroundDataUrl: dataUrl,
          existingTaskBackgroundUrl: "",
        });
      }
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    }
  };

  const warnings = useMemo(() => {
    const out: string[] = [];
    for (const mode of ["light", "dark"] as const) {
      const fg = state.tokens[mode]?.fg;
      const bg = state.tokens[mode]?.bg;
      if (fg && bg && isSafeHex(fg) && isSafeHex(bg)) {
        const ratio = themeContrastRatio(fg, bg);
        if (ratio < 4.5) out.push(`${mode} fg/bg ${ratio.toFixed(2)} < 4.5`);
      }
    }
    return out;
  }, [state.tokens]);

  const appLayoutClass = ["app--classic", "app--workbench", "app--creation"]
    .find((className) => document.querySelector(`.${className}`)) || "";

  return createPortal(
    <div className="theme-gallery__editor-overlay">
      <div
        ref={editorRef}
        className={`theme-editor theme-gallery__editor${appLayoutClass ? ` ${appLayoutClass}` : ""}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
      >
        <header className="theme-editor__header">
          <div>
            <strong id={titleId}>{state.mode === "create" ? t("settings.themeLibrary.editorCreate") : t("settings.themeLibrary.editorEdit")}</strong>
            <p>{t("settings.themeEditor.subtitle")}</p>
          </div>
          <button type="button" className="btn btn--icon" aria-label={t("common.close")} disabled={busy} onClick={onCancel}>
            <X size={16} />
          </button>
        </header>

        <div className="theme-editor__body">
          <div className="theme-editor__controls">
            <section className="theme-editor__section">
              <h3>{t("settings.themeEditor.metadata")}</h3>
              <div className="theme-editor__fields theme-editor__fields--grid">
                <label><span>{t("settings.themeLibrary.fieldId")}</span><input ref={initialFocusRef} value={state.id} disabled={state.mode === "edit" || busy} onChange={(e) => onChange({ id: e.target.value })} /></label>
                <label><span>{t("settings.themeLibrary.fieldName")}</span><input value={state.name} disabled={busy} onChange={(e) => onChange({ name: e.target.value })} /></label>
                <label><span>{t("settings.themeLibrary.fieldAuthor")}</span><input value={state.author} disabled={busy} onChange={(e) => onChange({ author: e.target.value })} /></label>
                <label><span>{t("settings.themeEditor.license")}</span><input value={state.license} disabled={busy} placeholder="MIT" onChange={(e) => onChange({ license: e.target.value })} /></label>
                <label className="theme-editor__wide"><span>{t("settings.themeEditor.description")}</span><textarea value={state.description} disabled={busy} rows={2} onChange={(e) => onChange({ description: e.target.value })} /></label>
              </div>
            </section>

            <section className="theme-editor__section">
              <h3>{t("settings.themeEditor.layout")}</h3>
              <div className="theme-editor__setting-row">
                <span>{t("settings.themeLibrary.fieldBase")}</span>
                <div className="set-seg theme-editor__base-styles">
                  {THEME_STYLES.map((s) => (
                    <button key={s} type="button" className={`set-seg__btn${state.baseStyle === s ? " set-seg__btn--on" : ""}`} disabled={busy} onClick={() => onChange({ baseStyle: s })}>{t(`settings.style.${s}.zh` as never)}</button>
                  ))}
                </div>
              </div>
              <div className="theme-editor__setting-row">
                <span>{t("settings.themeLibrary.fieldRecipes")}</span>
                <div className="theme-editor__recipe-groups">
                  <div className="set-seg">
                    {(["comfortable", "compact"] as const).map((density) => (
                      <button key={density} type="button" className={`set-seg__btn${state.recipes.density === density ? " set-seg__btn--on" : ""}`} onClick={() => onChange({ recipes: { ...state.recipes, density } })}>{t(`settings.themeEditor.density.${density}` as never)}</button>
                    ))}
                  </div>
                  <div className="set-seg">
                    {(["square", "soft", "round"] as const).map((corners) => (
                      <button key={corners} type="button" className={`set-seg__btn${state.recipes.corners === corners ? " set-seg__btn--on" : ""}`} onClick={() => onChange({ recipes: { ...state.recipes, corners } })}>{t(`settings.themeEditor.corners.${corners}` as never)}</button>
                    ))}
                  </div>
                </div>
              </div>
            </section>

            <section className="theme-editor__section">
              <div className="theme-editor__section-head">
                <h3>{t("settings.themeEditor.colors")}</h3>
                <div className="set-seg">
                  {(["light", "dark"] as const).map((mode) => (
                    <button key={mode} type="button" className={`set-seg__btn${state.tokenMode === mode ? " set-seg__btn--on" : ""}`} onClick={() => onChange({ tokenMode: mode })}>{mode === "light" ? t("settings.themeLight") : t("settings.themeDark")}</button>
                  ))}
                </div>
              </div>
              {TOKEN_GROUPS.map((group) => (
                <div key={group.labelKey} className="theme-editor__token-group">
                  <h4>{t(group.labelKey as never)}</h4>
                  <div className="theme-editor__color-grid">
                    {group.keys.filter((key) => themeTokenKeys().includes(key)).map((key) => {
                      const value = state.tokens[state.tokenMode]?.[key] || "";
                      return (
                        <label key={key} className="theme-editor__color">
                          <span className="theme-editor__token-label"><span>{t(`settings.themeTokens.key.${key}` as never)}</span><code>{key}</code></span>
                          <div className="theme-editor__color-control">
                            <input type="color" value={isSafeHex(value) ? value.slice(0, 7) : "#888888"} disabled={busy} onChange={(e) => setToken(key, e.target.value)} />
                            <input type="text" value={value} placeholder="#RRGGBB" disabled={busy} onChange={(e) => setToken(key, e.target.value.trim())} />
                          </div>
                        </label>
                      );
                    })}
                  </div>
                </div>
              ))}
            </section>

            <section className="theme-editor__section">
              <h3>{t("settings.themeEditor.scenes")}</h3>
              <p className="theme-editor__section-help">{t("settings.themeEditor.scenesHint")}</p>
              <div className="theme-editor__scene-grid">
                <SceneImageEditor
                  title={t("settings.themeEditor.homeBackground")}
                  hint={t("settings.themeEditor.homeBackgroundHint")}
                  url={homeUrl}
                  present={Boolean(state.background)}
                  focusX={state.background?.focusX ?? 0.5}
                  focusY={state.background?.focusY ?? 0.5}
                  safeArea={state.background?.safeArea || "center"}
                  opacity={state.background?.homeOpacity ?? 1}
                  opacityMax={1}
                  overlayStrength={state.background?.overlayStrength ?? 0.62}
                  paneOpacity={state.background?.paneOpacity ?? 0.72}
                  busy={busy}
                  onPick={() => void pickBackground("home")}
                  onClear={() => onChange({ background: null, backgroundDataUrl: "", existingBackgroundUrl: "" })}
                  onPatch={(patch) => {
                    const { opacity: nextOpacity, ...rest } = patch;
                    onChange({ background: { ...(state.background || defaultBackground()), ...rest, homeOpacity: nextOpacity ?? state.background?.homeOpacity ?? 1 } });
                  }}
                />
                <SceneImageEditor
                  title={t("settings.themeEditor.taskBackground")}
                  hint={state.taskBackground ? t("settings.themeEditor.taskBackgroundHint") : t("settings.themeEditor.taskBackgroundFallback")}
                  url={taskUrl || homeUrl}
                  present={Boolean(state.taskBackground)}
                  focusX={state.taskBackground?.focusX ?? state.background?.focusX ?? 0.5}
                  focusY={state.taskBackground?.focusY ?? state.background?.focusY ?? 0.5}
                  safeArea={state.taskBackground?.safeArea || state.background?.safeArea || "center"}
                  opacity={state.taskBackground?.opacity ?? state.background?.taskOpacity ?? 0.28}
                  opacityMax={1}
                  overlayStrength={state.taskBackground?.overlayStrength ?? state.background?.overlayStrength ?? 0.62}
                  paneOpacity={state.taskBackground?.paneOpacity ?? state.background?.paneOpacity ?? 0.80}
                  busy={busy}
                  onPick={() => void pickBackground("task")}
                  onClear={() => onChange({ taskBackground: null, taskBackgroundDataUrl: "", existingTaskBackgroundUrl: "" })}
                  onPatch={(patch) => onChange({ taskBackground: { ...(state.taskBackground || defaultTaskBackground()), ...patch, opacity: patch.opacity ?? state.taskBackground?.opacity ?? 0.28 } })}
                />
              </div>
            </section>

            {warnings.length > 0 ? (
              <div className="theme-editor__warn">
                {t("settings.themeLibrary.contrastWarn")}
                <ul>{warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul>
              </div>
            ) : null}
          </div>

          <aside className="theme-editor__live">
            <div className="theme-editor__live-head">
              <strong>{t("settings.themeEditor.livePreview")}</strong>
              <span>{previewScene === "home" ? t("settings.themeGallery.sceneHome") : t("settings.themeGallery.sceneTask")}</span>
            </div>
            <ThemePreviewControls mode={previewMode} scene={previewScene} onModeChange={setPreviewMode} onSceneChange={setPreviewScene} />
            <ThemePreviewSurface pack={draft} mode={previewMode} scene={previewScene} />
            <p>{t("settings.themeEditor.livePreviewHint")}</p>
          </aside>
        </div>

        <div className="theme-editor__actions">
          <button type="button" className="btn" disabled={busy} onClick={onCancel}>
            {t("common.cancel")}
          </button>
          <button type="button" className="btn" disabled={busy} onClick={() => onSave(false)}>
            {t("common.save")}
          </button>
          <button type="button" className="btn btn--primary" disabled={busy} onClick={() => onSave(true)}>
            {t("settings.themeGallery.saveAndApply")}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

type ScenePatch = {
  focusX?: number;
  focusY?: number;
  safeArea?: "left" | "center" | "right";
  opacity?: number;
  overlayStrength?: number;
  paneOpacity?: number;
};

function SceneImageEditor({
  title,
  hint,
  url,
  present,
  focusX,
  focusY,
  safeArea,
  opacity,
  opacityMax,
  overlayStrength,
  paneOpacity,
  busy,
  onPick,
  onClear,
  onPatch,
}: {
  title: string;
  hint: string;
  url: string;
  present: boolean;
  focusX: number;
  focusY: number;
  safeArea: string;
  opacity: number;
  opacityMax: number;
  overlayStrength: number;
  paneOpacity: number;
  busy: boolean;
  onPick: () => void;
  onClear: () => void;
  onPatch: (patch: ScenePatch) => void;
}) {
  const t = useT();
  const previewRef = useRef<HTMLDivElement>(null);
  const dragging = useRef(false);
  const updateFocus = (clientX: number, clientY: number) => {
    const rect = previewRef.current?.getBoundingClientRect();
    if (!rect || !present) return;
    onPatch({
      focusX: Math.min(1, Math.max(0, (clientX - rect.left) / rect.width)),
      focusY: Math.min(1, Math.max(0, (clientY - rect.top) / rect.height)),
    });
  };

  return (
    <div className={`theme-editor__scene${present ? " theme-editor__scene--ready" : ""}`}>
      <div className="theme-editor__scene-head">
        <div><strong>{title}</strong><p>{hint}</p></div>
        <div className="theme-editor__scene-actions">
          <button type="button" className="btn btn--small" disabled={busy} onClick={onPick}><ImagePlus size={13} /> {t("settings.themeLibrary.pickImage")}</button>
          {present ? <button type="button" className="btn btn--small" disabled={busy} onClick={onClear}>{t("settings.themeLibrary.clearImage")}</button> : null}
        </div>
      </div>
      <div
        ref={previewRef}
        className="theme-editor__bg-preview"
        style={url ? { backgroundImage: `url("${url}")`, opacity } : undefined}
        onPointerDown={(event) => {
          dragging.current = true;
          event.currentTarget.setPointerCapture?.(event.pointerId);
          updateFocus(event.clientX, event.clientY);
        }}
        onPointerMove={(event) => dragging.current && updateFocus(event.clientX, event.clientY)}
        onPointerUp={() => { dragging.current = false; }}
      >
        {!url ? <div className="theme-editor__scene-empty"><ImagePlus size={22} /><span>{t("settings.themeEditor.uploadPrompt")}</span></div> : null}
        {present ? <span className="theme-editor__focus" style={{ left: `${focusX * 100}%`, top: `${focusY * 100}%` }} /> : null}
      </div>
      {present ? (
        <div className="theme-editor__scene-controls">
          <div className="theme-editor__setting-block">
            <div className="theme-editor__setting-row">
              <span>{t("settings.themeEditor.safeArea")}</span>
              <div className="set-seg" role="radiogroup" aria-label={t("settings.themeEditor.safeArea")}>
                {(["left", "center", "right"] as const).map((area) => <button key={area} type="button" role="radio" aria-checked={safeArea === area} className={`set-seg__btn${safeArea === area ? " set-seg__btn--on" : ""}`} onClick={() => onPatch({ safeArea: area })}>{t(`settings.themeEditor.safeArea.${area}` as never)}</button>)}
              </div>
            </div>
            <p className="theme-editor__setting-hint">{t("settings.themeEditor.safeAreaHint")}</p>
          </div>
          <label className="theme-editor__range"><span>{t("settings.themeEditor.opacity")} <b>{Math.round(opacity * 100)}%</b></span><input type="range" min={0} max={opacityMax} step={0.01} value={opacity} onChange={(e) => onPatch({ opacity: Number(e.target.value) })} /></label>
          <label className="theme-editor__range"><span>{t("settings.themeLibrary.overlayStrength")} <b>{Math.round(overlayStrength * 100)}%</b></span><input type="range" min={0} max={1} step={0.01} value={overlayStrength} onChange={(e) => onPatch({ overlayStrength: Number(e.target.value) })} /></label>
          <label className="theme-editor__range"><span>{t("settings.themeEditor.paneOpacity")} <b>{Math.round(paneOpacity * 100)}%</b></span><input type="range" min={0} max={1} step={0.01} value={paneOpacity} onChange={(e) => onPatch({ paneOpacity: Number(e.target.value) })} /></label>
        </div>
      ) : null}
    </div>
  );
}

function themeContrastRatio(a: string, b: string): number {
  const luminance = (hex: string) => {
    const raw = hex.slice(1, 7);
    const channels = [0, 2, 4].map((index) => parseInt(raw.slice(index, index + 2), 16) / 255)
      .map((value) => (value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4));
    return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
  };
  const la = luminance(a);
  const lb = luminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}
