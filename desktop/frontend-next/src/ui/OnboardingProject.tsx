import { useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { StudioIcon } from "./StudioIcon";
import { useFileDrop } from "./filedrop";

export interface ProjectChoice {
  pick: () => Promise<string | null>;
  add: (path: string) => Promise<void>;
}

const nameOf = (path: string) => path.replace(/[/\\]+$/, "").split(/[/\\]/).pop() || path;

// The last step: which folder the work happens in. A folder can be dropped on
// the box or picked; once chosen it sits in the box and "Start" lights up.
export function OnboardingProject({ project, onDone, onSkip }: { project: ProjectChoice; onDone: () => void; onSkip: () => void }) {
  const [over, setOver] = useState(false);
  const [dir, setDir] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const drop = useFileDrop((d) => { if (!busy && d.paths[0]) { setDir(d.paths[0]); setErr(""); } }, setOver);
  const pick = async () => {
    if (busy) return;
    try {
      const got = await project.pick();
      if (got) { setDir(got); setErr(""); }
    } catch (e) {
      setErr(reason(e));
    }
  };
  const start = async () => {
    if (!dir || busy) return;
    setBusy(true);
    try {
      await project.add(dir);
      onDone();
    } catch (e) {
      setErr(reason(e));
      setBusy(false);
    }
  };
  return (
    <>
      <h1 className="onb-t">{t("onboarding.which_project")}</h1>
      <p className="onb-s">{t("onboarding.drop_to_choose")}</p>
      <div className="onb-drop" ref={drop} data-over={over ? "" : undefined} data-chosen={dir ? "" : undefined}>
        {dir ? (
          <div className="onb-chosen">
            <StudioIcon name="folder" />
            <b>{nameOf(dir)}</b>
            <small title={dir}>{dir}</small>
          </div>
        ) : (
          <>
            <StudioIcon name="folder" />
            <span>{t("onboarding.drop_a_folder_here")}</span>
          </>
        )}
        <button type="button" className="onb-skip" data-action="onboarding.pick-folder" disabled={busy} onClick={() => void pick()}>
          {t(dir ? "onboarding.choose_another" : "onboarding.choose_a_folder")}
        </button>
      </div>
      {err && <div className="onb-err" role="alert">{err}</div>}
      <div className="onb-acts">
        <button className="btn onb-go" data-primary data-action="onboarding.start" disabled={!dir || busy} onClick={() => void start()}>{t("common.start")}</button>
        <button type="button" className="onb-skip" data-action="onboarding.skip" disabled={busy} onClick={onSkip}>{t("onboarding.not_now")}</button>
      </div>
    </>
  );
}
