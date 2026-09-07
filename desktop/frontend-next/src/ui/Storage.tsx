import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { bytes } from "../i18n/format";
import type { AgentPort } from "../port/port";
import type { StoragePlan, StorageRoot, StorageState } from "../port/storage";

// Three questions in the order a person asks them: how much is there, where is
// it, and what can go. The rows are whatever the kernel declares — a root added
// there shows up here without this panel being told about it.

// The rows a person recognises. A root the kernel declares but this list has no
// name for still renders, under its own id: showing an unnamed row is better
// than hiding storage someone is looking for.
const NAMED: Record<string, [string, string]> = {
  state: ["会话与归档", "转录、压缩归档、用量统计、回溯快照"],
  cache: ["索引与缓存", "搜索索引与派生数据，删掉会自动重建"],
  worktrees: ["隔离工作区", "交付模式检出的独立副本"],
  home: ["配置与凭据", "设置和 API key，始终随用户配置文件走"],
  locks: ["进程锁", "多个实例互斥用，必须留在本机固定位置"],
};

export function Storage({ port }: { port: AgentPort }) {
  const [state, setState] = useState<StorageState | null>(null);
  const [error, setError] = useState("");
  const [picking, setPicking] = useState<string | null>(null);
  const [target, setTarget] = useState("");
  const [plan, setPlan] = useState<StoragePlan | null>(null);
  const input = useRef<HTMLInputElement>(null);

  const read = useCallback(() => {
    port
      .storage()
      .then(setState)
      .catch(() => setError(t("读不到存储占用。")));
  }, [port]);

  useEffect(read, [read]);

  // A move outlives the request that started it, so the panel follows it the
  // way it follows a running turn: by asking again until it says it is done.
  const running = state?.move && !state.move.done;
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(read, 500);
    return () => clearInterval(timer);
  }, [running, read]);

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
        .catch(() => setError(t("搬迁没能开始。")));
    },
    [port, read],
  );

  if (error) return <div className="empty">{error}</div>;
  if (!state) return <div className="empty">{t("正在统计…")}</div>;

  const measured = state.roots.filter((r) => !r.missing || r.relocatable);
  const largest = Math.max(1, ...measured.map((r) => r.bytes));

  return (
    <div className="storage">
      <section className="grp">
        <h3 className="lbl">{t("占用")}</h3>
        {measured.map((root) => (
          <Bar key={root.id} root={root} largest={largest} />
        ))}
        <Drives roots={state.roots} />
      </section>

      {state.leftBehind && <LeftBehind at={state.leftBehind} />}

      <section className="grp">
        <h3 className="lbl">{t("位置")}</h3>
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

function Bar({ root, largest }: { root: StorageRoot; largest: number }) {
  const [name] = NAMED[root.id] ?? [root.id, ""];
  return (
    <div className="vol">
      <div className="row">
        <span className="nm">{t(name)}</span>
        <span className="sz">
          {bytes(root.bytes)}
          {root.files > 0 && ` · ${t("{n} 个文件", { n: root.files })}`}
        </span>
      </div>
      <div className="meter">
        <i style={{ width: `${Math.max(1, Math.round((root.bytes / largest) * 100))}%` }} />
      </div>
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
      <h3 className="lbl">{t("上一个位置还留着东西")}</h3>
      <p className="note">
        {t("这些还在 {dir}：{names}。移动存储位置时它们没有被一起带走，所以这台机器上的壁纸、主题包或更新回滚备份可能看起来不见了。手动把这几个目录复制到当前位置即可恢复。", {
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
            placeholder={t("目标文件夹的完整路径，空文件夹，或本来就存着这块数据的那个")}
            onChange={(e) => onTarget(e.target.value)}
          />
          {plan && <Verdict plan={plan} />}
          <div className="acts">
            <button className="btn" onClick={onCancel}>
              {t("取消")}
            </button>
            <button className="btn pri" data-action="storage.move" disabled={!plan?.ok} onClick={onStart}>
              {t(plan?.adopt ? "指向这里" : "开始搬迁")}
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
        {t("这个文件夹里已经存着这块数据（{size}，{n} 个文件）。直接指过去就行，不复制、也不删。重启后生效。", {
          size: bytes(plan.bytes),
          n: plan.files,
        })}
        {(plan.stays ?? 0) > 0
          ? " " + t("当前位置还留着 {size}，不会一起带过去。", { size: bytes(plan.stays ?? 0) })
          : null}
      </p>
    );
  }
  return (
    <p className="ready">
      {t("将搬走 {size}（{n} 个文件），目标盘剩余 {free}。完成后需要重启才会生效。", {
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
      <h3 className="lbl">{t("搬迁")}</h3>
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
          {move.done && <p className="ready">{t("已搬完。重启后生效。")}</p>}
          {move.detail && <p className="ready">{move.detail}</p>}
        </>
      )}
    </section>
  );
}
