import { t } from "../i18n";
import type { ModPreview } from "../port/plugin";

// Keyed by the kernel's reason and kind. A key with no entry shows the key itself
// rather than nothing: an unlisted module must never read as a clean one.
const NOT_LISTED: Record<string, string> = {
  typescript: "它是 TypeScript，本应用不读",
  too_large: "文件太大",
  unparseable: "语法解析失败",
  timeout: "解析超时",
  unsupported_module: "不是本宿主支持的模块形式",
};

const FINDING: Record<string, string> = {
  eval: "用了 eval",
  function_ctor: "用了 new Function",
  computed_member: "用计算出来的名字访问接口",
  api_escapes: "把接口对象交给了别处",
  on_escapes: "把注册函数交给了别处",
  destructured_api: "把接口对象解构了出来",
  no_register: "没有找到导出的 register",
};

// What a literal session call takes from the session (read) or puts in front of
// the model (submit). Two lines, so a module that only reads usage is not
// described as one that can queue prompts.
const SESSION_READ = new Set(["messages", "usage"]);

const IMPORT_KIND: Record<string, string> = { static: "import", require: "require", dynamic: "import()" };

const list = (xs: string[]) => xs.join("、");

/** What the install preview says about a Claude Code mod: it runs on this
 *  machine with the user's full trust, and the listing below is read from the
 *  module's syntax tree. It is a static list and not a guarantee, and the block
 *  says so in those words; a module that could not be read is "not previewed". */
export function ModPreviewBlock({ mod }: { mod: ModPreview }) {
  const dynamic = mod.events.filter((e) => e.dynamic).length;
  const calls = mod.api.map((c) => (c.dynamic ? "$.…" : c.method ? `$.${c.namespace}.${c.method}` : `$.${c.namespace}`));
  return (
    <section className="modpreview" aria-label={t("ext.claude_code_mod_preview")}>
      <div className="risk" data-kind="shell">
        <span className="lb">{t("ext.claude_code_mod_full_trust")}</span>
        <span className="why">{t("ext.its_code_runs_on_this")}</span>
      </div>
      <p className="why">{t("ext.what_follows_is_a_static")}</p>
      {!mod.listed ? (
        <div className="risk" data-kind="secret">
          <span className="lb">{t("ext.not_previewed")}</span>
          <span className="dt">{t(NOT_LISTED[mod.reason ?? ""] ?? mod.reason ?? "")}</span>
          <span className="why">{t("ext.that_does_not_mean_it")}</span>
        </div>
      ) : (
        <>
          <div className="risk"><span className="lb">{t("ext.events_it_hooks")}</span><span className="dt">{mod.events.filter((e) => !e.dynamic).map((e) => e.name).join(", ") || t("ext.none_listed")}</span></div>
          {dynamic > 0 && <div className="risk" data-kind="secret"><span className="lb">{t("ext.events_whose_names_cannot_be")}</span><span className="dt">{dynamic}</span></div>}
          {calls.length > 0 && <div className="risk"><span className="lb">{t("ext.api_it_uses")}</span><span className="dt">{calls.join(", ")}</span></div>}
          {mod.session.some((m) => SESSION_READ.has(m)) && <div className="risk"><span className="lb">{t("ext.reads_your_conversation")}</span><span className="dt">{mod.session.filter((m) => SESSION_READ.has(m)).join(", ")}</span></div>}
          {mod.session.includes("submit") && <div className="risk" data-kind="secret"><span className="lb">{t("ext.queues_prompts_in_front_of")}</span><span className="dt">{t("ext.under_its_own_name_never")}</span></div>}
          {mod.imports.length > 0 && (
            <div className="risk" data-kind="secret">
              <span className="lb">{t("ext.code_it_pulls_in_not")}</span>
              <span className="dt">{mod.imports.map((i) => `${IMPORT_KIND[i.kind] ?? i.kind} ${i.spec}`).join(", ")}</span>
            </div>
          )}
          {mod.findings.length > 0 && (
            <div className="risk" data-kind="secret">
              <span className="lb">{t("ext.constructs_that_leave_the_listing")}</span>
              <span className="dt">{list(mod.findings.map((f) => t(FINDING[f.kind] ?? f.kind)))}</span>
            </div>
          )}
          {mod.unserved.length > 0 && <div className="risk"><span className="lb">{t("ext.events_this_app_never_raises")}</span><span className="dt">{mod.unserved.join(", ")}</span></div>}
        </>
      )}
      <div className="risk">
        <span className="lb">{t("ext.module")}</span>
        <span className="dt" title={mod.sha256}>{mod.module} · {mod.sha256.slice(0, 12)}</span>
      </div>
    </section>
  );
}
