import { Fragment, useState } from "react";
import { t } from "../i18n";
import { FOLD_CHOICES, FOLD_DEFAULTS, setFoldModes, setShowsThinkingPreview, type Fold, type FoldMode, type FoldModes } from "../state/prefs";
import { startsOpen, useFoldModes, useThinkingPreview } from "../state/foldpref";
import { ApplyNote } from "./Group";
import { Switch } from "./Switch";

const PARTS: [Fold, string, string][] = [
  ["thinking", "chat.thinking_2", "chat.the_models_reasoning_before_it"],
  ["activity", "chat.activity", "chat.a_run_of_tool_calls"],
  ["steps", "chat.step_details", "chat.each_steps_arguments_output_and"],
  ["output", "chat.long_output", "chat.command_output_and_file_contents"],
  ["compaction", "chat.compaction_brief", "chat.the_brief_the_model_works"],
];

const MODE: Record<FoldMode, string> = {
  folded: "common.collapse",
  live: "chat.open_while_running",
  failed: "chat.open_on_failure",
  changed: "chat.open_on_file_change",
  open: "common.expand",
};

// Long output is clipped rather than folded, so its two answers say so.
const OUTPUT_MODE: Partial<Record<FoldMode, string>> = { folded: "chat.clipped", open: "look.full" };

const PRESETS: [string, string, FoldModes][] = [
  ["compact", "chat.compact", { thinking: "folded", activity: "folded", steps: "failed", output: "folded", compaction: "folded" }],
  ["standard", "look.standard", FOLD_DEFAULTS],
  ["changes", "chat.changes_only", { thinking: "folded", activity: "changed", steps: "changed", output: "folded", compaction: "folded" }],
  ["full", "chat.detailed", { thinking: "open", activity: "open", steps: "open", output: "open", compaction: "open" }],
];

const same = (a: FoldModes, b: FoldModes) => PARTS.every(([k]) => a[k] === b[k]);

/** How each foldable part of a conversation starts on this machine. A block the
 *  reader has already opened or closed keeps that choice. */
export function Folding() {
  const modes = useFoldModes();
  const thinkingPreview = useThinkingPreview();
  const [hot, setHot] = useState<Fold | null>(null);
  const [live, setLive] = useState(false);
  const preset = PRESETS.find(([, , p]) => same(p, modes))?.[0];

  return (
    <section className="grp" id="set-folding" data-setting="folding">
      <div className="grp-hd">
        <h3>{t("chat.transcript_folding")}</h3>
        {!preset && <span className="now">{t("common.custom")}</span>}
      </div>
      <p className="hint">{t("chat.whether_each_part_of_a")}</p>

      <div className="seg" data-text role="radiogroup" aria-label={t("chat.folding_preset")}>
        {PRESETS.map(([id, name, p]) => (
          <button key={id} role="radio" data-action="transcript.fold-preset" data-value={id} aria-checked={preset === id} onClick={() => setFoldModes(p)}>
            {t(name)}
          </button>
        ))}
      </div>

      <FoldPreview modes={modes} thinkingPreview={thinkingPreview} hot={hot} live={live} onLive={setLive} />

      <div className="grp-items fold-rows" onMouseLeave={() => setHot(null)}>
        {PARTS.map(([kind, label, why]) => (
          <Fragment key={kind}>
            <div className="fold-row" data-hot={hot === kind ? "" : undefined}
              onMouseEnter={() => setHot(kind)} onFocus={() => setHot(kind)}>
              <span className="tx">
                <span className="lb">{t(label)}</span>
                <span className="ds">{t(why)}</span>
              </span>
              <div className="seg" data-text role="radiogroup" aria-label={t(label)}>
                {FOLD_CHOICES[kind].map((mode) => (
                  <button key={mode} role="radio" data-action="transcript.fold" data-value={`${kind}:${mode}`}
                    aria-checked={modes[kind] === mode} onClick={() => setFoldModes({ [kind]: mode })}>
                    {t((kind === "output" && OUTPUT_MODE[mode]) || MODE[mode])}
                  </button>
                ))}
              </div>
            </div>
            {kind === "thinking" && (
              <div className="lrow" onMouseEnter={() => setHot(kind)} onFocus={() => setHot(kind)}>
                <span className="tx">
                  <span className="lb">{t("chat.preview_the_beginning_when_collapsed")}</span>
                  <span className="ds">{t("chat.show_one_line_of_opening")}</span>
                </span>
                <Switch data-action="transcript.thinking-preview" on={thinkingPreview} label={t("chat.preview_the_beginning_when_collapsed")}
                  onClick={() => setShowsThinkingPreview(!thinkingPreview)} />
              </div>
            )}
          </Fragment>
        ))}
      </div>
      <ApplyNote id="folding" />
    </section>
  );
}

// A schematic turn rather than real cards: it has to show a failed step and a
// running one on demand, which no real conversation holds still for.
function FoldPreview({ modes, thinkingPreview, hot, live, onLive }: { modes: FoldModes; thinkingPreview: boolean; hot: Fold | null; live: boolean; onLive: (v: boolean) => void }) {
  const think = startsOpen(modes.thinking, live);
  const work = startsOpen(modes.activity, live, false, true);
  const step = (failed: boolean, changed = false) => startsOpen(modes.steps, false, failed, changed);
  const part = (kind: Fold) => ({ "data-part": kind, "data-hot": hot === kind ? "" : undefined });
  return (
    <figure className="fold-prev" aria-hidden="true">
      <figcaption>
        <span>{t("common.preview")}</span>
        <div className="seg" data-text role="group">
          <button tabIndex={-1} data-action="transcript.fold-preview" data-value="live" aria-pressed={live} onClick={() => onLive(true)}>{t("common.running")}</button>
          <button tabIndex={-1} data-action="transcript.fold-preview" data-value="done" aria-pressed={!live} onClick={() => onLive(false)}>{t("common.done_2")}</button>
        </div>
      </figcaption>
      <div className="fp-turn">
        <div className="fp-who">reasonix</div>
        <div className="fp-block" {...part("thinking")} data-open={think ? "" : undefined}>
          <span className="fp-sum">{live ? t("chat.thinking") : t("chat.thought_for_6s")}</span>
          {think && <Lines n={2} dim />}
          {!think && thinkingPreview && <span className="thinking-preview">{t("chat.check_the_configuration_first_then")}</span>}
        </div>
        {!live && <Lines n={2} />}
        <div className="fp-block fp-work" {...part("activity")} data-open={work ? "" : undefined}>
          <span className="fp-sum">
            {t("chat.activity")}
            <i>{live ? t("common.running") : t("chat.n_3_actions_1_failed")}</i>
          </span>
          {work && (
            <div className="fp-steps">
              <Step name="read_file" arg="src/app.ts" open={step(false)} hot={hot} />
              <Step name="edit_file" arg="src/app.ts" open={step(false, true)} hot={hot} />
              <Step name="bash" arg="npm test" open={step(!live)} failed={!live} long hot={hot} output={modes.output === "open"} />
            </div>
          )}
        </div>
        <div className="fp-block" {...part("compaction")} data-open={startsOpen(modes.compaction) ? "" : undefined}>
          <span className="fp-sum">{t("chat.see_the_digest_it_carries")}</span>
          {startsOpen(modes.compaction) && <Lines n={2} dim />}
        </div>
      </div>
    </figure>
  );
}

function Step({ name, arg, open, failed, long, output, hot }: {
  name: string; arg: string; open: boolean; failed?: boolean; long?: boolean; output?: boolean; hot: Fold | null;
}) {
  return (
    <div className="fp-block fp-step" data-part="steps" data-hot={hot === "steps" ? "" : undefined}
      data-open={open ? "" : undefined} data-failed={failed ? "" : undefined}>
      <span className="fp-sum"><b>{name}</b> {arg}</span>
      {open && (
        <div className="fp-out" data-part="output" data-hot={hot === "output" ? "" : undefined} data-clip={long && !output ? "" : undefined}>
          <Lines n={long ? (output ? 6 : 3) : 1} mono />
          {long && <span className="fp-more">{output ? t("common.collapse") : t("chat.show_all")}</span>}
        </div>
      )}
    </div>
  );
}

function Lines({ n, dim, mono }: { n: number; dim?: boolean; mono?: boolean }) {
  return (
    <span className="fp-lines" data-dim={dim ? "" : undefined} data-mono={mono ? "" : undefined}>
      {Array.from({ length: n }, (_, i) => <span key={i} style={{ width: `${[92, 74, 86, 60, 80, 48][i % 6]}%` }} />)}
    </span>
  );
}
