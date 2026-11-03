import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, ConfigProblem } from "../port/port";
import { CopyButton } from "./CopyButton";

// The file every panel here writes to, when it is the file that is wrong.
//
// It sits above all of them because it is true of all of them: a config the
// kernel could not parse is not overwritten, so every save is refused and every
// panel is showing recovered values rather than the user's own. Saying that
// once, before anything is tried, is the difference between a settings screen
// and a settings screen that lies until you touch it.
export function ConfigTrouble({ port, onRepaired }: { port: AgentPort; onRepaired: () => void }) {
  const [problem, setProblem] = useState<ConfigProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState("");

  useEffect(() => {
    let live = true;
    port
      .configProblem()
      .then((p) => live && setProblem(p))
      .catch(() => live && setProblem(null));
    return () => {
      live = false;
    };
  }, [port]);

  if (!problem) return null;

  const repair = async () => {
    setBusy(true);
    setFailed("");
    try {
      const done = await port.repairConfig();
      setProblem(done.problem);
      onRepaired();
    } catch (e) {
      setFailed(reason(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="find cfgbad" data-lvl="warn" role="status">
      <span className="t">
        {problem.line ? t("configTrouble.lineOfTheConfigFile", { line: problem.line }) : t("configTrouble.theConfigFileDoesNot")}
      </span>
      <span className="why">
        {problem.recovered === "last-known-good"
          ? t("configTrouble.whatIsShownBelowIs")
          : t("configTrouble.whatIsShownBelowIs2")}
      </span>
      <div className="term">
        <div className="term-l dim">{problem.path}</div>
        {problem.excerpt && (
          <div className="term-l er">
            {problem.line} | {problem.excerpt}
          </div>
        )}
        {/* The repair is the same bytes said in a way TOML accepts, not a
            guess at what was meant — it is only offered when the file parses
            afterwards. */}
        {problem.repair && (
          <div className="term-l ok">
            {t("configTrouble.becomes")} | {problem.repair}
          </div>
        )}
      </div>
      <div className="acts">
        {problem.repair && (
          <button className="act" data-action="config.repair" disabled={busy} onClick={() => void repair()}>
            {busy ? t("configTrouble.repairing") : t("configTrouble.keepACopyAndRepair")}
          </button>
        )}
        <CopyButton text={problem.path} label={t("common.copyPath")} />
      </div>
      {failed && <span className="why nwhy">{failed}</span>}
    </div>
  );
}
