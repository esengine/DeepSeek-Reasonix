import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { bytes } from "../i18n/format";
import type { AgentPort } from "../port/port";
import type { HubPort, LegacyImport } from "../port/hub";
import { LegacySkips } from "./LegacySkips";
import { NativeSessionImport } from "./NativeSessionImport";
import type { StoragePlan, StorageRoot, StorageState } from "../port/storage";

// Three questions in the order a person asks them: how much is there, where is
// it, and what can go. The rows are whatever the kernel declares — a root added
// there shows up here without this panel being told about it.

// The rows a person recognises. A root the kernel declares but this list has no
// name for still renders, under its own id: showing an unnamed row is better
// than hiding storage someone is looking for.
const NAMED: Record<string, [string, string]> = {
  state: ["data.sessions_and_archives", "data.transcripts_compaction_archives_usage_stats"],
  cache: ["data.indexes_and_caches", "data.search_indexes_and_derived_data"],
  worktrees: ["data.isolated_worktrees", "data.the_separate_checkouts_delivery_works"],
  home: ["data.configuration_and_credentials", "data.settings_and_api_keys_these"],
  locks: ["data.process_locks", "data.how_instances_exclude_each_other"],
};

const LAYOUT_LIMIT_MS = 10_000;
// Longer than the kernel's own per-root budget, so a root the kernel gave up on
// arrives as a partial count and this limit only fires when the kernel is silent.
const MEASURE_LIMIT_MS = 20_000;

function within<T>(work: Promise<T>, ms: number): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("timeout")), ms);
    work.then(
      (v) => {
        clearTimeout(timer);
        resolve(v);
      },
      (e) => {
        clearTimeout(timer);
        reject(e);
      },
    );
  });
}

const PLAN_DELAY_MS = 400;

export function Storage({ port, hub, workspace, onRecovered }: { port: AgentPort; hub: HubPort; workspace: string; onRecovered: () => void }) {
  const [state, setState] = useState<StorageState | null>(null);
  const [error, setError] = useState("");
  const [picking, setPicking] = useState<string | null>(null);
  const [target, setTarget] = useState("");
  const [plan, setPlan] = useState<StoragePlan | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const [recovering, setRecovering] = useState(false);
  const [recovery, setRecovery] = useState<LegacyImport | null>(null);
  const [recoveryError, setRecoveryError] = useState("");
  const [typing, setTyping] = useState(false);
  const [typed, setTyped] = useState("");

  const [stalled, setStalled] = useState<ReadonlySet<string>>(new Set());
  const generation = useRef(0);

  // Rows appear as soon as the kernel has laid them out; each root's size then
  // arrives on its own, so one slow directory cannot hold the others back.
  const read = useCallback(() => {
    const mine = ++generation.current;
    setError("");
    setStalled(new Set());
    within(port.storage({ layout: true }), LAYOUT_LIMIT_MS)
      .then((layout) => {
        if (mine !== generation.current) return;
        setState(layout);
        for (const root of layout.roots) {
          if (!root.pending) continue;
          within(port.storage({ root: root.id }), MEASURE_LIMIT_MS)
            .then((answer) => {
              if (mine !== generation.current) return;
              const measured = answer.roots[0];
              if (!measured) return;
              setState((prev) => (prev ? { ...prev, roots: prev.roots.map((r) => (r.id === measured.id ? measured : r)) } : prev));
            })
            .catch(() => {
              if (mine === generation.current) setStalled((prev) => new Set(prev).add(root.id));
            });
        }
      })
      .catch(() => {
        if (mine === generation.current) setError(t("data.cannot_read_what_is_on"));
      });
  }, [port]);

  useEffect(read, [read]);

  // A move outlives the request that started it, so the panel follows it the
  // way it follows a running turn: by asking again until it says it is done.
  // The poll carries the move and leaves the sizes already measured in place.
  const running = state?.move && !state.move.done;
  const wasRunning = useRef(false);
  useEffect(() => {
    if (!running) {
      if (wasRunning.current) read();
      wasRunning.current = false;
      return;
    }
    wasRunning.current = true;
    const timer = setInterval(() => {
      port
        .storage({ layout: true })
        .then((layout) =>
          setState((prev) => (prev ? { ...layout, roots: prev.roots } : layout)),
        )
        .catch(() => setError(t("data.cannot_read_what_is_on")));
    }, 500);
    return () => clearInterval(timer);
  }, [running, read, port]);

  useEffect(() => {
    if (picking) input.current?.focus();
  }, [picking]);

  const planTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const planSeq = useRef(0);
  useEffect(() => () => clearTimeout(planTimer.current), []);

  const check = useCallback(
    (root: string, dir: string) => {
      clearTimeout(planTimer.current);
      const seq = ++planSeq.current;
      setPlan(null);
      if (!dir.trim()) return;
      planTimer.current = setTimeout(() => {
        port
          .planStorageMove(root, dir)
          .then((answer) => seq === planSeq.current && setPlan(answer))
          .catch(() => seq === planSeq.current && setPlan(null));
      }, PLAN_DELAY_MS);
    },
    [port],
  );

  const start = useCallback(
    (root: string, dir: string) => {
      port
        .moveStorage(root, dir)
        .then((accepted) => {
          if (!accepted.ok) {
            setPlan(accepted);
            return;
          }
          setPicking(null);
          setTarget("");
          setPlan(null);
          read();
        })
        .catch(() => setError(t("data.the_move_could_not_be")));
    },
    [port, read],
  );

  const importFrom = useCallback(
    async (source: string) => {
      setRecovering(true);
      try {
        const result = await hub.importLegacySessions(source, workspace);
        setRecovery(result);
        setTyping(false);
        onRecovered();
      } catch {
        setRecoveryError(t("data.this_folder_could_not_be"));
      } finally {
        setRecovering(false);
      }
    },
    [hub, onRecovered, workspace],
  );

  const recoverLegacy = useCallback(async () => {
    setRecoveryError("");
    const source = await hub.pickFolder();
    // null is a panel that could not be shown, "" one the person closed.
    if (source === null) {
      setTyping(true);
      return;
    }
    if (!source) return;
    await importFrom(source);
  }, [hub, importFrom]);

  if (error) return <div className="empty">{error}</div>;
  if (!state) return <div className="empty">{t("data.measuring")}</div>;

  const measured = state.roots.filter((r) => !r.missing || r.relocatable);
  const largest = Math.max(1, ...measured.filter((r) => !r.pending).map((r) => r.bytes));

  return (
    <div className="storage">
      <NativeSessionImport hub={hub} workspace={workspace} onImported={onRecovered} />
      <section className="grp">
        <h3 className="lbl">{t("data.recover_older_sessions")}</h3>
        <div className="item">
          <div className="l">
            <b>{t("data.import_from_an_older_reasonix")}</b>
            <span className="hint">{t("data.choose_the_older_data_folder")}</span>
            {recovery && (
              <span className="ready">
                {recovery.imported > 0
                  ? t("data.recovered_n_sessions", { n: recovery.imported })
                  : recovery.recognised
                    ? t("data.nothing_new_to_import")
                    : t("data.no_older_sessions_were_found")}
                {recovery.warnings > 0 ? " " + t("data.n_items_could_not_be", { n: recovery.warnings }) : null}
              </span>
            )}
            {recoveryError && <span className="held">{recoveryError}</span>}
          </div>
          <button className="btn" data-action="session.import-legacy" disabled={recovering || !workspace} onClick={() => void recoverLegacy()}>
            {t(recovering ? "data.scanning" : "data.choose_older_data_folder")}
          </button>
        </div>
        {typing && (
          <form
            className="item"
            data-action="session.import-legacy"
            data-value="path"
            onSubmit={(ev) => {
              ev.preventDefault();
              if (typed.trim()) void importFrom(typed.trim());
            }}
          >
            <div className="l">
              <span className="hint">{t("data.the_folder_panel_could_not")}</span>
              <input
                value={typed}
                spellCheck={false}
                dir="ltr"
                placeholder="/home/you/.reasonix"
                aria-label={t("data.older_data_folder_path")}
                data-action="session.import-legacy"
                data-value="path"
                onChange={(ev) => setTyped(ev.target.value)}
              />
            </div>
            <button className="btn" type="submit" data-action="session.import-legacy" data-value="path" disabled={recovering || !typed.trim()}>
              {t("data.import_this_folder")}
            </button>
          </form>
        )}
        {recovery && <LegacySkips skipped={recovery.skipped ?? []} />}
      </section>

      <section className="grp">
        <h3 className="lbl">{t("data.space_used")}</h3>
        {measured.map((root) => (
          <Bar key={root.id} root={root} largest={largest} stalled={stalled.has(root.id)} />
        ))}
        <Drives roots={state.roots} />
      </section>

      {state.leftBehind && <LeftBehind at={state.leftBehind} />}

      <section className="grp">
        <h3 className="lbl">{t("data.locations")}</h3>
        {/* Said once for the section rather than on each row: it is one fact
            about this kernel, not a property of any one path. Without it the
            rows lose their 「移动…」 with nothing in its place, which is the
            reading Row's own note exists to prevent. */}
        {!state.editable && <p className="note">{t("data.this_server_does_not_open")}</p>}
        {state.roots.map((root) => (
          <Row
            key={root.id}
            root={root}
            editable={state.editable}
            busy={Boolean(running)}
            open={picking === root.id}
            target={target}
            plan={plan}
            inputRef={input}
            onOpen={() => {
              setPicking(root.id);
              setTarget("");
              setPlan(null);
            }}
            onCancel={() => {
              setPicking(null);
              setPlan(null);
            }}
            onTarget={(dir) => {
              setTarget(dir);
              check(root.id, dir);
            }}
            onStart={() => start(root.id, target)}
          />
        ))}
      </section>

      {state.move && <Moving move={state.move} />}
    </div>
  );
}

function Bar({ root, largest, stalled }: { root: StorageRoot; largest: number; stalled: boolean }) {
  const [name] = NAMED[root.id] ?? [root.id, ""];
  const unknown = root.pending || stalled;
  return (
    <div className="vol" data-state={stalled ? "stalled" : root.pending ? "measuring" : root.truncated ? "partial" : "done"}>
      <div className="row">
        <span className="nm">{t(name)}</span>
        {/* The count breaks down the size, so it goes where there is a size to
            break down. The locks root is empty files — 0 B beside thousands of
            them read as an occupancy nobody could find, next to a meter bar
            drawn at its 1% floor. */}
        <span className="sz">
          {stalled
            ? t("data.measuring_timed_out")
            : root.pending
              ? t("data.measuring")
              : (root.truncated ? "≥ " : "") + bytes(root.bytes)}
          {!unknown && root.bytes > 0 && root.files > 0 && ` · ${t("common.n_files", { n: root.files })}`}
        </span>
      </div>
      {root.truncated && !unknown && <p className="note">{t("data.this_folder_is_too_large")}</p>}
      {!unknown && (
        <div className="meter">
          <i style={{ width: `${Math.max(1, Math.round((root.bytes / largest) * 100))}%` }} />
        </div>
      )}
    </div>
  );
}

// Which disks this adds up on. Roots sharing a volume are one line, because
// moving one of them off a full drive only helps if the others go too.
// A move made before the state root claimed these left them where they were,
// and this run is not allowed to fetch them: an install whose roots come from
// the environment gets its own copies, never the production one moved into it.
// Saying where they are beats a wallpaper that is simply gone.
function LeftBehind({ at }: { at: { dir: string; names: string[] } }) {
  return (
    <section className="grp">
      <h3 className="lbl">{t("data.data_remains_in_the_previous")}</h3>
      <p className="note">
        {t("data.these_are_still_in_dir", {
          dir: at.dir,
          names: at.names.join("、"),
        })}
      </p>
    </section>
  );
}

function Drives({ roots }: { roots: StorageRoot[] }) {
  const seen = new Map<string, StorageRoot>();
  for (const root of roots) {
    if (root.volume && root.volumeTotal && !seen.has(root.volume)) seen.set(root.volume, root);
  }
  if (seen.size === 0) return null;
  return (
    <div className="drives">
      {[...seen.values()].map((root) => (
        <p key={root.volume}>
          {t("data.drive_free_free_of_total", {
            drive: root.volume ?? "",
            free: bytes(root.volumeFree ?? 0),
            total: bytes(root.volumeTotal ?? 0),
          })}
        </p>
      ))}
    </div>
  );
}

function Row({
  root,
  editable,
  busy,
  open,
  target,
  plan,
  inputRef,
  onOpen,
  onCancel,
  onTarget,
  onStart,
}: {
  root: StorageRoot;
  editable: boolean;
  busy: boolean;
  open: boolean;
  target: string;
  plan: StoragePlan | null;
  inputRef: React.RefObject<HTMLInputElement | null>;
  onOpen: () => void;
  onCancel: () => void;
  onTarget: (dir: string) => void;
  onStart: () => void;
}) {
  const [name, hint] = NAMED[root.id] ?? [root.id, ""];
  // Every path on screen is either actionable or says why it is not. A row that
  // simply had no button would read as a bug.
  const held = root.pinnedBy
    ? t("data.set_by_env", { env: root.pinnedBy })
    : !root.relocatable
      ? t("data.cannot_be_moved")
      : "";

  return (
    <div className="item">
      <div className="l">
        <b>{t(name)}</b>
        {hint && <span className="hint">{t(hint)}</span>}
        <span className="p">{root.dir}</span>
        {held && <span className="held">{held}</span>}
        {root.err && <span className="held">{root.err}</span>}
      </div>
      {root.relocatable && !root.pinnedBy && editable && !open && (
        <button className="btn" disabled={busy} onClick={onOpen}>
          {t("data.move")}
        </button>
      )}
      {open && (
        <div className="mover">
          <input
                data-action="storage.move"
            ref={inputRef}
            value={target}
            spellCheck={false}
            placeholder={t("data.full_path_to_the_destination")}
            onChange={(e) => onTarget(e.target.value)}
          />
          {plan && <Verdict plan={plan} />}
          <div className="acts">
            <button className="btn" onClick={onCancel}>
              {t("common.cancel")}
            </button>
            <button className="btn pri" data-action="storage.move" disabled={!plan?.ok} onClick={onStart}>
              {t(plan?.adopt ? "data.point_it_here" : "data.start_the_migration")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// The preflight's answer, verbatim. Refusals arrive with their own sentences,
// so nothing here reconstructs an explanation from a code.
function Verdict({ plan }: { plan: StoragePlan }) {
  if (!plan.ok) {
    return (
      <ul className="refusals">
        {(plan.refusals ?? []).map((r) => (
          <li key={r.code}>{r.detail}</li>
        ))}
      </ul>
    );
  }
  // Adopting is not a small move, it is a different operation: the data is
  // already there, so what a person needs to know is that nothing gets copied
  // and nothing at the old location gets deleted.
  if (plan.adopt) {
    return (
      <p className="ready">
        {t("data.this_folder_already_holds_that", {
          size: bytes(plan.bytes),
          n: plan.files,
        })}
        {(plan.stays ?? 0) > 0
          ? " " + t("data.the_current_location_still_holds", { size: bytes(plan.stays ?? 0) })
          : null}
      </p>
    );
  }
  return (
    <p className="ready">
      {t("data.moves_size_in_n_files", {
        size: bytes(plan.bytes),
        n: plan.files,
        free: bytes(plan.free),
      })}
    </p>
  );
}

const PHASES: Record<string, string> = {
  adopting: "data.pointing_at_the_new_location",
  copying: "data.copying",
  verifying: "data.verifying",
  committed: "data.committed_clearing_the_old_location",
  done: "common.done_2",
};

function Moving({ move }: { move: NonNullable<StorageState["move"]> }) {
  const pct = move.total > 0 ? Math.min(100, Math.round((move.bytes / move.total) * 100)) : 0;
  return (
    <section className="grp moving">
      <h3 className="lbl">{t("data.migrate")}</h3>
      {move.err ? (
        <p className="failed">{move.err}</p>
      ) : (
        <>
          <div className="row">
            <span className="nm">{t(PHASES[move.phase] ?? move.phase)}</span>
            <span className="sz">
              {bytes(move.bytes)} / {bytes(move.total)}
            </span>
          </div>
          <div className="meter">
            <i style={{ width: `${pct}%` }} />
          </div>
          {move.done && <p className="ready">{t("data.moved_it_takes_effect_after")}</p>}
          {move.detail && <p className="ready">{move.detail}</p>}
        </>
      )}
    </section>
  );
}
