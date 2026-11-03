import { t } from "../i18n";
import type { ModPreview } from "../port/plugin";

// Keyed by the kernel's reason and kind. A key with no entry shows the key itself
// rather than nothing: an unlisted module must never read as a clean one.
const NOT_LISTED: Record<string, string> = {
  typescript: "modPreviewBlock.itIsTypeScriptWhichThis",
  too_large: "modPreviewBlock.theFileIsTooLarge",
  unparseable: "modPreviewBlock.itCouldNotBeParsed",
  timeout: "modPreviewBlock.parsingTimedOut",
  unsupported_module: "modPreviewBlock.itIsNotAModule",
};

const FINDING: Record<string, string> = {
  eval: "modPreviewBlock.usesEval",
  function_ctor: "modPreviewBlock.usesNewFunction",
  computed_member: "modPreviewBlock.reachesTheAPIByA",
  api_escapes: "modPreviewBlock.handsTheAPIObjectElsewhere",
  on_escapes: "modPreviewBlock.handsTheRegisteringFunctionElsewhere",
  destructured_api: "modPreviewBlock.destructuresTheAPIObject",
  no_register: "modPreviewBlock.noExportedRegisterWasFound",
};

// What a literal session call takes from the session (read) or puts in front of
// the model (submit). Two lines, so a module that only reads usage is not
// described as one that can queue prompts.
const SESSION_READ = new Set(["messages", "usage"]);

const IMPORT_KIND: Record<string, string> = { static: "import", require: "require", dynamic: "import()" };

const list = (xs: string[]) => xs.join(t("common.listSeparator"));

/** What the install preview says about a Claude Code mod: it runs on this
 *  machine with the user's full trust, and the listing below is read from the
 *  module's syntax tree. It is a static list and not a guarantee, and the block
 *  says so in those words; a module that could not be read is "not previewed". */
export function ModPreviewBlock({ mod }: { mod: ModPreview }) {
  const dynamic = mod.events.filter((e) => e.dynamic).length;
  const calls = mod.api.map((c) => (c.dynamic ? "$.…" : c.method ? `$.${c.namespace}.${c.method}` : `$.${c.namespace}`));
  return (
    <section className="modpreview" aria-label={t("modPreviewBlock.claudeCodeModPreview")}>
      <div className="risk" data-kind="shell">
        <span className="lb">{t("modPreviewBlock.claudeCodeModFullTrust")}</span>
        <span className="why">{t("modPreviewBlock.itsCodeRunsOnThis")}</span>
      </div>
      <p className="why">{t("modPreviewBlock.whatFollowsIsAStatic")}</p>
      {!mod.listed ? (
        <div className="risk" data-kind="secret">
          <span className="lb">{t("modPreviewBlock.notPreviewed")}</span>
          <span className="dt">{t(NOT_LISTED[mod.reason ?? ""] ?? mod.reason ?? "")}</span>
          <span className="why">{t("modPreviewBlock.thatDoesNotMeanIt")}</span>
        </div>
      ) : (
        <>
          <div className="risk"><span className="lb">{t("modPreviewBlock.eventsItHooks")}</span><span className="dt">{mod.events.filter((e) => !e.dynamic).map((e) => e.name).join(", ") || t("modPreviewBlock.noneListed")}</span></div>
          {dynamic > 0 && <div className="risk" data-kind="secret"><span className="lb">{t("modPreviewBlock.eventsWhoseNamesCannotBe")}</span><span className="dt">{dynamic}</span></div>}
          {calls.length > 0 && <div className="risk"><span className="lb">{t("modPreviewBlock.aPIItUses")}</span><span className="dt">{calls.join(", ")}</span></div>}
          {mod.session.some((m) => SESSION_READ.has(m)) && <div className="risk"><span className="lb">{t("modPreviewBlock.readsYourConversation")}</span><span className="dt">{mod.session.filter((m) => SESSION_READ.has(m)).join(", ")}</span></div>}
          {mod.session.includes("submit") && <div className="risk" data-kind="secret"><span className="lb">{t("modPreviewBlock.queuesPromptsInFrontOf")}</span><span className="dt">{t("modPreviewBlock.underItsOwnNameNever")}</span></div>}
          {mod.imports.length > 0 && (
            <div className="risk" data-kind="secret">
              <span className="lb">{t("modPreviewBlock.codeItPullsInNot")}</span>
              <span className="dt">{mod.imports.map((i) => `${IMPORT_KIND[i.kind] ?? i.kind} ${i.spec}`).join(", ")}</span>
            </div>
          )}
          {mod.findings.length > 0 && (
            <div className="risk" data-kind="secret">
              <span className="lb">{t("modPreviewBlock.constructsThatLeaveTheListing")}</span>
              <span className="dt">{list(mod.findings.map((f) => t(FINDING[f.kind] ?? f.kind)))}</span>
            </div>
          )}
          {mod.unserved.length > 0 && <div className="risk"><span className="lb">{t("modPreviewBlock.eventsThisAppNeverRaises")}</span><span className="dt">{mod.unserved.join(", ")}</span></div>}
        </>
      )}
      <div className="risk">
        <span className="lb">{t("modPreviewBlock.module")}</span>
        <span className="dt" title={mod.sha256}>{mod.module} · {mod.sha256.slice(0, 12)}</span>
      </div>
    </section>
  );
}
