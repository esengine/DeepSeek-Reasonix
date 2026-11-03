import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { t } from "../i18n";
import type { FeedbackMine } from "../port/feedback";
import type { AgentPort } from "../port/port";
import { loadLevelNoticePref, useShowsLevelNotice } from "../state/levelnotices";

const LEVEL_NAME = ["common.newSeed", "common.sprout", "common.seedling", "common.sapling", "common.bloom", "common.canopy", "common.grove"] as const;
const FOCUS_GAP_MS = 30_000;

const levelName = (level: number): string => (Number.isInteger(level) && level >= 0 && level < LEVEL_NAME.length ? t(LEVEL_NAME[level]!) : `L${level}`);

// Says once that a feedback level went up, in the note slot, and only while the
// person asked to be told. Looking never acknowledges: only 知道了 does, and the
// kernel keeps what was acknowledged, so a reload or a second window cannot say
// it again. Anything else the slot has to say is drawn when there is nothing here.
export function LevelUpNotice({ port, tick, fallback }: { port: AgentPort | null; tick: number; fallback: ReactNode }) {
  const enabled = useShowsLevelNotice(port ?? {});
  const [mine, setMine] = useState<FeedbackMine | null>(null);
  const [error, setError] = useState(false);
  const portRef = useRef(port);
  portRef.current = port;
  const lastRead = useRef(0);

  const read = useCallback(async () => {
    const p = portRef.current;
    if (!p) return;
    if (!(await loadLevelNoticePref(p))) {
      setMine(null);
      return;
    }
    lastRead.current = Date.now();
    try {
      setMine(await p.myFeedback());
    } catch {
      // a failed background read says nothing and is tried again on the next opening
    }
  }, []);

  useEffect(() => {
    void read();
  }, [read, tick, port]);

  useEffect(() => {
    const again = () => {
      if (document.visibilityState !== "hidden" && Date.now() - lastRead.current > FOCUS_GAP_MS) void read();
    };
    document.addEventListener("visibilitychange", again);
    return () => document.removeEventListener("visibilitychange", again);
  }, [read]);

  const up = enabled ? mine?.promotion : null;
  if (!up || !port) return <>{fallback}</>;
  const next = mine?.profile?.nextLevel;
  const dismiss = async () => {
    setError(false);
    try {
      await port.ackFeedbackPromotion(up.to);
      setMine((m) => (m ? { ...m, promotion: null } : m));
    } catch {
      setError(true);
    }
  };
  return (
    <div className="errbar" data-kind="note" role="status">
      <span>
        {t("levelUpNotice.yourFeedbackLevelWentUp", { n: up.to, name: levelName(up.to) })}
        {next != null && mine?.profile?.remaining != null ? " " + t("levelUpNotice.moreShippedToReach", { r: mine.profile.remaining, next: levelName(next) }) : ""}
        {error ? " " + t("levelUpNotice.thatCouldNotBeRemembered") : ""}
      </span>
      <button data-action="level-up.notice-dismiss" onClick={() => void dismiss()}>{t("common.gotIt")}</button>
    </div>
  );
}
