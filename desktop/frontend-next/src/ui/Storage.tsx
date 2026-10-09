import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { bytes } from "../i18n/format";
import type { AgentPort } from "../port/port";
import type { HubPort } from "../port/hub";
import type { StoragePlan, StorageRoot, StorageState } from "../port/storage";

// Three questions in the order a person asks them: how much is there, where is
// it, and what can go. The rows are whatever the kernel declares — a root added
// there shows up here without this panel being told about it.

// The rows a person recognises. A root the kernel declares but this list has no
// name for still renders, under its own id: showing an unnamed row is better
// than hiding storage someone is looking for.
const NAMED: Record<string, [string, string]> = {
  state: ["会话与归档", "转录、压缩归档、用量统计、回溯快照"],
  cache: ["索引与缓存", "搜索索引与派生数据，删除后会自动重建"],
  worktrees: ["隔离工作区", "交付模式检出的独立副本"],
  home: ["配置与凭据", "设置与 API key，始终随用户配置文件保存"],
  locks: ["进程锁", "用于多实例互斥，必须保留在本机固定位置。每个均为空文件；删除会破坏互斥，因此只保留不清理"],
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

export function Storage({ port, hub, workspace, onRecovered }: { port: AgentPort; hub: HubPort; workspace: string; onRecovered: () => void }) {
  const [state, setState] = useState<StorageState | null>(null);
  const [error, setError] = useState("");
  const [picking, setPicking] = useState<string | null>(null);
  const [target, setTarget] = useState("");
  const [plan, setPlan] = useState<StoragePlan | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const [recovering, setRecovering] = useState(false);
  const [recovery, setRecovery] = useState<{ imported: number; warnings: number; recognised: boolean } | null>(null);
  const [recoveryError, setRecoveryError] = useState("");

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
        if (mine === generation.current) setError(t("无法读取存储占用。"));
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
        .catch(() => setError(t("无法读取存储占用。")));
    }, 500);
    return () => clearInterval(timer);
  }, [running, read, port]);

  useEffect(() => {
    if (picking) input.current?.focus();
  }, [picking]);

  const check = useCallback(
    (root: string, dir: string) => {
      setPlan(null);
      if (!dir.trim()) return;
      port.planStorageMove(root, dir).then(setPlan).catch(() => setPlan(null));
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
        .catch(() => setError(t("迁移未能启动。")));
    },
    [port, read],
  );

  const recoverLegacy = useCallback(async () => {
    setRecoveryError("");
    const source = await hub.pickFolder();
    if (!source) return;
    setRecovering(true);
    try {
      const result = await hub.importLegacySessions(source, workspace);
      setRecovery({ imported: result.imported, warnings: result.warnings, recognised: result.recognised });
      onRecovered();
    } catch {
      setRecoveryError(t("未能扫描这个文件夹。请确认它是旧版 Reasonix 的数据目录。"));
    } finally {
      setRecovering(false);
    }
  }, [hub, onRecovered, workspace]);

  if (error) return <div className="empty">{error}</div>;
  if (!state) return <div className="empty">{t("正在统计…")}</div>;

  const measured = state.roots.filter((r) => !r.missing || r.relocatable);
  const largest = Math.max(1, ...measured.filter((r) => !r.pending).map((r) => r.bytes));

  return (
    <div className="storage">
      <section className="grp">
        <h3 className="lbl">{t("找回旧版会话")}</h3>
        <div className="item">
          <div className="l">
            <b>{t("从旧版 Reasonix 导入")}</b>
            <span className="hint">{t("选择旧版的数据文件夹。扫描只会复制会话，不会移动或删除原文件；找不到原工作区的会话会归入当前工作区。")}</span>
            {recovery && (
              <span className="ready">
                {recovery.imported > 0
                  ? t("已找回 {n} 个会话。", { n: recovery.imported })
                  : recovery.recognised
                    ? t("没有发现尚未导入的旧会话。")
                    : t("这个文件夹里没有找到旧版会话。请选择 Reasonix 的数据文件夹（里面有 sessions、projects 或 desktop-sessions-v5）。")}
                {recovery.warnings > 0 ? " " + t("有 {n} 项无法读取。", { n: recovery.warnings }) : null}
              </span>
            )}
            {recoveryError && <span className="held">{recoveryError}</span>}
          </div>
          <button className="btn" data-action="session.import-legacy" disabled={recovering || !workspace} onClick={() => void recoverLegacy()}>
            {t(recovering ? "正在扫描…" : "选择旧版数据目录…")}
          </button>
        </div>
      </section>

      <section className="grp">
        <h3 className="lbl">{t("占用")}</h3>
        {measured.map((root) => (
          <Bar key={root.id} root={root} largest={largest} stalled={stalled.has(root.id)} />
        ))}
        <Drives roots={state.roots} />
      </section>

      {state.leftBehind && <LeftBehind at={state.leftBehind} />}

      <section className="grp">
        <h3 className="lbl">{t("位置")}</h3>
        {/* Said once for the section rather than on each row: it is one fact
            about this kernel, not a property of any one path. Without it the
            rows lose their 「移动…」 with nothing in its place, which is the
            reading Row's own note exists to prevent. */}
        {!state.editable && <p className="note">{t("这台服务器未开放存储迁移")}</p>}
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
            ? t("统计超时")
            : root.pending
              ? t("正在统计…")
              : (root.truncated ? "≥ " : "") + bytes(root.bytes)}
          {!unknown && root.bytes > 0 && root.files > 0 && ` · ${t("{n} 个文件", { n: root.files })}`}
        </span>
      </div>
      {root.truncated && !unknown && <p className="note">{t("目录过大，已统计到时间上限，实际占用不小于此数")}</p>}
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
      <h3 className="lbl">{t("原位置仍有残留数据")}</h3>
      <p className="note">
        {t("以下内容仍位于 {dir}：{names}。迁移存储位置时未一并迁移，因此项目列表、全局指令与记忆、计划任务、壁纸、主题包或更新回滚备份可能显示为缺失。下次启动会自动并入当前位置；若仍显示，请退出后把这些文件或目录复制到当前位置，已有的文件不要覆盖。", {
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
          {t("{drive} 剩余 {free} / {total}", {
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
    ? t("由 {env} 指定", { env: root.pinnedBy })
    : !root.relocatable
      ? t("不可移动")
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
          {t("移动…")}
        </button>
      )}
      {open && (
        <div className="mover">
          <input
                data-action="storage.move"
            ref={inputRef}
            value={target}
            spellCheck={false}
            placeholder={t("目标文件夹的完整路径：空文件夹，或已存有该数据的文件夹")}
            onChange={(e) => onTarget(e.target.value)}
          />
          {plan && <Verdict plan={plan} />}
          <div className="acts">
            <button className="btn" onClick={onCancel}>
              {t("取消")}
            </button>
            <button className="btn pri" data-action="storage.move" disabled={!plan?.ok} onClick={onStart}>
              {t(plan?.adopt ? "指向这里" : "开始迁移")}
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
        {t("该文件夹中已存有此数据（{size}，{n} 个文件）。将直接指向该位置，不复制也不删除。重启后生效。", {
          size: bytes(plan.bytes),
          n: plan.files,
        })}
        {(plan.stays ?? 0) > 0
          ? " " + t("当前位置仍有 {size}，不会一并迁移。", { size: bytes(plan.stays ?? 0) })
          : null}
      </p>
    );
  }
  return (
    <p className="ready">
      {t("将迁移 {size}（{n} 个文件），目标磁盘剩余 {free}。完成后需重启生效。", {
        size: bytes(plan.bytes),
        n: plan.files,
        free: bytes(plan.free),
      })}
    </p>
  );
}

const PHASES: Record<string, string> = {
  adopting: "正在指向新位置",
  copying: "正在复制",
  verifying: "正在校验",
  committed: "已提交，正在清理原位置",
  done: "已完成",
};

function Moving({ move }: { move: NonNullable<StorageState["move"]> }) {
  const pct = move.total > 0 ? Math.min(100, Math.round((move.bytes / move.total) * 100)) : 0;
  return (
    <section className="grp moving">
      <h3 className="lbl">{t("迁移")}</h3>
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
          {move.done && <p className="ready">{t("迁移完成。重启后生效。")}</p>}
          {move.detail && <p className="ready">{move.detail}</p>}
        </>
      )}
    </section>
  );
}
