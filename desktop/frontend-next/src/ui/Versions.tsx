import { useCallback, useEffect, useState } from "react";
import { bytes } from "../i18n/format";
import { t } from "../i18n";
import type { UpdateProgress, VersionHub } from "../port/port";
import { reason } from "../i18n/kernel";

// The panel answers three questions in the order a user asks them: what am I
// running, is something wrong with it, and how do I get off it. Every action
// here is one that actually works — a button that cannot do what it says is
// worse than no button.

type Port = {
  versions(): Promise<VersionHub>;
  pinVersion(v: string): Promise<void>;
  goToVersion(v: string): Promise<void>;
  onUpdateProgress(cb: (p: UpdateProgress) => void): () => void;
};

function when(iso: string): string {
  const at = Date.parse(iso);
  if (Number.isNaN(at)) return "";
  const days = Math.floor((Date.now() - at) / 86400000);
  if (days <= 0) return t("今天");
  if (days === 1) return t("昨天");
  if (days < 30) return t("{n} 天前", { n: days });
  return new Date(at).toLocaleDateString();
}

// The phase is the sentence. Verifying gets its own because it is the pause
// after the bar fills, which otherwise reads as a hang on a large artifact.
function say(p: UpdateProgress): string {
  switch (p.phase) {
    case "downloading":
      return p.total > 0 ? t("下载中 {got} / {all}", { got: bytes(p.received), all: bytes(p.total) }) : t("下载中 {got}", { got: bytes(p.received) });
    case "verifying":
      return t("校验签名…");
    case "downloaded":
      return t("准备安装…");
    case "authorizing":
      return t("等待系统授权…");
    case "idle":
      // Only reachable in the gap between the click and the first read that
      // sees the move: the panel is already showing this row as going.
      return t("准备中…");
    case "relaunching":
      return "正在重启到新版本…";
    case "error":
      return p.err || "安装失败";
  }
}

export function Versions({ port }: { port: Port }) {
  const [hub, setHub] = useState<VersionHub | null>(null);
  const [busy, setBusy] = useState(false);
  const [going, setGoing] = useState("");
  const [failed, setFailed] = useState("");
  const [progress, setProgress] = useState<UpdateProgress | null>(null);

  const reload = useCallback(() => {
    port.versions().then(setHub).catch(() => setHub(null));
  }, [port]);

  useEffect(reload, [reload]);
  useEffect(() => port.onUpdateProgress(setProgress), [port]);

  const pin = async (v: string) => {
    setBusy(true);
    setFailed("");
    try {
      await port.pinVersion(v);
      reload();
    } catch (e) {
      setFailed(reason(e));
    } finally {
      setBusy(false);
    }
  };

  // Answered when the move is under way, not when it is done: an install that
  // worked ends by ending the kernel this asked, so a resolved promise says
  // only that it started. What ends the row is the progress the kernel reports,
  // or the window going with it.
  const goTo = async (v: string) => {
    setGoing(v);
    setProgress(null);
    try {
      await port.goToVersion(v);
    } catch (e) {
      setProgress({ version: v, phase: "error", received: 0, total: 0, err: String(e) });
      setGoing("");
    }
  };

  // The kernel owns whether the move is still running, so the row follows its
  // answer rather than a local guess. Reaching a resting phase means the
  // install did not take over -- it failed, or a package prompt was dismissed --
  // and the catalog is re-read because a pin was written either way.
  useEffect(() => {
    if (!going || progress?.version !== going || progress.phase === "idle") return;
    if (progress.phase === "error" || progress.phase === "downloaded") {
      setGoing("");
      reload();
    }
  }, [going, progress, reload]);

  if (hub === null) {
    return <p className="acct-note">{t("正在读取版本…")}</p>;
  }

  // A shell that answers null (or an older one that omits the field) must not
  // be able to take the window down with it.
  const list = hub.versions ?? [];
  const dev = !hub.current || hub.current === "dev";
  const locked = busy || going !== "";
  return (
    <div className="vers">
      <div className="vnow">
        <span className="cur">{hub.current || "dev"}</span>
        <span className="lb">{t(dev ? "本地构建" : "当前版本")}</span>
        {hub.pinned && !hub.stalePin && <span className="pin">{t("已固定")}</span>}
      </div>

      {/* Severity language is the transcript's: a coloured left rule, no icons.
          Pinned is not a problem, so it is ok-coloured; a stale pin is. */}
      {hub.err && (
        <div className="find" data-lvl="warn">
          <span className="t">{t("连不上版本目录")}</span>
          <span className="why">{t("{err}　—— 本地功能不受影响，稍后再试。", { err: hub.err })}</span>
        </div>
      )}
      {failed && (
        <div className="find" data-lvl="warn" role="alert">
          <span className="t">{t("这一步没做成")}</span>
          <span className="why">{failed}</span>
        </div>
      )}
      {hub.pinned && !hub.stalePin && (
        <div className="find" data-lvl="ok">
          <span className="t">{t("已固定在 {v}，不会自动更新", { v: hub.pinned })}</span>
          <span className="why">
            {t("回退之后固定是有意的：否则下次更新会把你放回刚离开的那个版本。")}
            <button className="lnk" onClick={() => pin("")} disabled={locked}>
              {t("恢复自动更新")}
            </button>
          </span>
        </div>
      )}
      {hub.stalePin && (
        <div className="find" data-lvl="warn">
          <span className="t">固定的是 {hub.pinned}，但现在跑的是 {hub.current}</span>
          <span className="why">
            {t("这条固定已经不再描述现实，自动更新按未固定处理。")}
            <button className="lnk" onClick={() => pin("")} disabled={locked}>
              {t("清除固定")}
            </button>
          </span>
        </div>
      )}
      {!hub.err && hub.newer && !hub.pinned && (
        <div className="find" data-lvl="ok">
          <span className="t">有新版本 {hub.latest}</span>
          <span className="why">{t("在下面那一行装它，安装完会自动重启。")}</span>
        </div>
      )}
      {progress?.phase === "error" && (
        <div className="find" data-lvl="warn">
          <span className="t">切换到 {progress.version} 失败</span>
          <span className="why">{progress.err}　—— 当前版本没有被动过，可以再试一次。</span>
        </div>
      )}
      {/* Going back is the one move with a consequence the user cannot undo by
          going forward again, so it is said before they click, not after. */}
      {going !== "" && (
        <p className="acct-note">{t("切换版本期间请不要关窗口。较新版本写过的会话，回到旧版本后会暂时打不开，升回去就能看。")}</p>
      )}

      {/* Newest first: the list reads as history, and where you are in it is
          marked the way every other "this one" in the app is. */}
      <div className="vlist">
        {list.map((v, i) => (
          <div
            key={v.version}
            className="vrow"
            data-on={v.current ? "" : undefined}
            data-side={v.current ? "now" : v.older ? "past" : "ahead"}
            style={{ animationDelay: `${Math.min(i, 8) * 34}ms` }}
          >
            <span className="nm">{v.version}</span>
            <span className="ds">{v.current ? "正在运行" : v.older ? "更早的版本" : "更新的版本"}</span>
            {/* A row the catalog does not carry has no date. Saying so beats an
                empty column: it is why this version has no download page. */}
            <span className="sc">{v.publishedAt ? when(v.publishedAt) : v.current ? "未发布" : ""}</span>
            {going === v.version && progress ? (
              <span className="sa">{say(progress)}</span>
            ) : (
              !v.current && (
                <button className="sa lnk" onClick={() => goTo(v.version)} disabled={locked}>
                  {t(v.older ? "回退到这个版本" : "安装这个版本")}
                </button>
              )
            )}
            {v.current && !hub.pinned && (
              <button className="sa lnk" onClick={() => pin(v.version)} disabled={locked}>
                {t("固定在这里")}
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
