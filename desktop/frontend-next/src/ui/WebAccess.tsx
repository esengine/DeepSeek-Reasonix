import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/http_error";
import type { AgentPort, SearxngProbe, WebAccessPatch, WebAccessSettings } from "../port/port";
import { Group } from "./Group";
import { Switch } from "./Switch";

type Kind = "search" | "fetch";

const NAME: Record<Kind, Record<string, string>> = {
  search: { searxng: "SearXNG", deepseek_api: "models.deepseek_search" },
  fetch: { local: "models.local_fetch", deepseek_api: "models.deepseek_fetch" },
};
const NOTE: Record<Kind, Record<string, string>> = {
  search: { searxng: "models.a_search_service_you_run", deepseek_api: "models.deepseeks_own_search_api_billed" },
  fetch: { local: "models.this_machine_downloads_the_page", deepseek_api: "models.deepseeks_own_page_fetch_api" },
};
const PROBE: Record<string, string> = {
  "search.searxng_json_disabled": "models.this_searxng_does_not_allow",
  "search.searxng_unreachable": "models.cannot_reach_this_address_check",
  "search.searxng_not_searxng": "models.this_address_answers_but_not",
  "search.searxng_redirected": "models.this_address_redirects_elsewhere_enter",
  "search.searxng_address_refused": "models.the_test_will_not_connect",
};
const probeSaid = (r: SearxngProbe) =>
  r.ok ? t("models.connected_this_searxng_returns_json")
    : PROBE[r.code ?? ""] ? t(PROBE[r.code ?? ""])
      : t("models.this_searxng_refused_the_test", { status: String(r.status ?? "") });
const said = (kind: Kind, id: string) => (NAME[kind][id] ? t(NAME[kind][id]) : id);

/** Where the agent looks things up and reads pages from: an ordered list per
 *  kind, free backends first and a paid one only after those fail. Consent to be
 *  billed is its own switch, written only by the person's click and withdrawn the
 *  same way; nothing here ever ticks it. */
export function WebAccess({ port }: { port: AgentPort }) {
  const [state, setState] = useState<WebAccessSettings | null>(null);
  const [url, setUrl] = useState("");
  const [busy, setBusy] = useState(false);
  const [probe, setProbe] = useState<SearxngProbe | null>(null);
  const [probing, setProbing] = useState(false);
  const [error, setError] = useState("");
  // A window the server has not granted editing to is read-only; the first refused
  // save is how that is learned, and nothing here asks again.
  const [readOnly, setReadOnly] = useState(false);

  useEffect(() => {
    let alive = true;
    port.webAccessSettings().then((s) => {
      if (!alive) return;
      setState(s);
      setUrl(s.searxng_url);
    }).catch(() => alive && setState(null));
    return () => {
      alive = false;
    };
  }, [port]);

  const apply = useCallback(async (patch: WebAccessPatch) => {
    setBusy(true);
    setError("");
    try {
      const next = await port.saveWebAccessSettings(patch);
      setState(next);
      setUrl(next.searxng_url);
    } catch (e) {
      if (e instanceof HttpError && e.reason?.code === "web_access.editing_disabled") setReadOnly(true);
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  }, [port]);

  const test = async () => {
    setProbing(true);
    setProbe(null);
    setError("");
    try {
      const typed = url.trim();
      if (typed !== state?.searxng_url) {
        const next = await port.saveWebAccessSettings({ searxng_url: typed });
        setState(next);
        setUrl(next.searxng_url);
      }
      setProbe(await port.probeSearxng());
    } catch (e) {
      if (e instanceof HttpError && e.reason?.code === "web_access.editing_disabled") setReadOnly(true);
      setError(reason(e));
    } finally {
      setProbing(false);
    }
  };

  if (!state) return null;
  const locked = busy || readOnly;

  const section = (kind: Kind) => {
    const known = kind === "search" ? state.known_search_backends : state.known_fetch_backends;
    const on = kind === "search" ? state.search_backends : state.fetch_backends;
    const allowed = kind === "search" ? state.search_paid_allowed : state.fetch_paid_allowed;
    const order = (next: string[]) => apply(kind === "search" ? { search_backends: next } : { fetch_backends: next });
    const consent = (next: string[]) => apply(kind === "search" ? { search_paid_allowed: next } : { fetch_paid_allowed: next });
    const rows = [...on.filter((id) => known.includes(id)), ...known.filter((id) => !on.includes(id))];
    const title = kind === "search" ? t("common.web_search") : t("models.web_fetch");
    return (
      <div className="wa-sec" role="group" aria-label={title}>
        <div className="peernote"><b>{title}</b>{" "}{kind === "search" ? t("models.tried_in_order_the_free") : t("models.tried_in_order_the_free")}</div>
        {on.length === 0 && <div className="peernote">{kind === "search" ? t("models.no_search_backend_is_on") : t("models.no_fetch_backend_is_on")}</div>}
        <ul className="wa-list">
          {rows.map((id) => {
            const idx = on.indexOf(id);
            const enabled = idx >= 0;
            const paid = state.paid_backends.includes(id);
            return (
              <li key={id}>
                <span className="tx"><span className="lb">{said(kind, id)}</span><span className="ds">{NOTE[kind][id] ? t(NOTE[kind][id]) : ""}</span></span>
                {enabled && (
                  <span className="acts">
                    <button className="act" data-action="web-access.move" data-value="up" disabled={locked || idx === 0} aria-label={t("common.move_name_up", { name: said(kind, id) })}
                      onClick={() => void order(on.map((x, i) => (i === idx - 1 ? id : i === idx ? on[idx - 1] : x)))}>↑</button>
                    <button className="act" data-action="web-access.move" data-value="down" disabled={locked || idx === on.length - 1} aria-label={t("common.move_name_down", { name: said(kind, id) })}
                      onClick={() => void order(on.map((x, i) => (i === idx + 1 ? id : i === idx ? on[idx + 1] : x)))}>↓</button>
                  </span>
                )}
                <Switch data-action="web-access.backend" data-value={id} on={enabled} busy={locked}
                  label={t("common.enable_name", { name: said(kind, id) })}
                  onClick={() => void order(enabled ? on.filter((x) => x !== id) : [...on, id])} />
                {paid && (enabled || allowed.includes(id)) && (
                  <div className="wa-consent">
                    <span className="tx">
                      <span className="lb">{kind === "search" ? t("models.agree_to_be_billed_per_2") : t("models.agree_to_be_billed_per")}</span>
                      <span className="ds">{state.deepseek_api_ready || allowed.includes(id) ? t("models.once_you_agree_it_is") : t("models.this_needs_a_deepseek_provider")}</span>
                    </span>
                    <Switch data-action="web-access.consent" data-value={`${kind}:${id}`} on={allowed.includes(id)} busy={locked || (!state.deepseek_api_ready && !allowed.includes(id))}
                      label={kind === "search" ? t("models.agree_to_be_billed_per_2") : t("models.agree_to_be_billed_per")}
                      onClick={() => void consent(allowed.includes(id) ? allowed.filter((x) => x !== id) : [...allowed, id])} />
                  </div>
                )}
              </li>
            );
          })}
        </ul>
        {kind === "search" && on.includes("searxng") && (
          <div className="wa-url">
            <label className="peernote" htmlFor="searxng-url">{t("models.searxng_address")}</label>
            <input id="searxng-url" className="peerfield" data-action-change="web-access.searxng-url" aria-label={t("models.searxng_address")} placeholder="http://127.0.0.1:8080"
              value={url} disabled={locked} onChange={(e) => setUrl(e.target.value)}
              onBlur={() => { if (url.trim() !== state.searxng_url) void apply({ searxng_url: url.trim() }); }} />
            <div className="wa-test">
              <button className="act" data-action="web-access.probe" disabled={readOnly || probing || !url.trim()} onClick={() => void test()}>{probing ? t("models.testing") : t("common.test_it")}</button>
              {probe && <div className={probe.ok ? "peernote" : "peernote bad"} role="status">{probeSaid(probe)}</div>}
            </div>
          </div>
        )}
      </div>
    );
  };

  return (
    <Group id="web-access" title={t("models.web_search_and_fetch")} hint={t("models.which_backends_the_agent_uses")}>
      <div className="box">
        {section("search")}
        {section("fetch")}
        <div className="wa-sec" role="group" aria-label={t("models.video_addresses")}>
          <div className="peernote"><b>{t("models.video_addresses")}</b>{" "}{t("models.the_model_can_hand_a")}</div>
          <ul className="wa-list">
            <li>
              <span className="tx">
                <span className="lb">{t("models.hand_video_addresses_to_the")}</span>
                <span className="ds">{t("models.when_on_the_address_is")}</span>
              </span>
              <Switch data-action="web-access.video-url" on={state.video_url_passthrough} busy={locked} label={t("models.hand_video_addresses_to_the")}
                onClick={() => void apply({ video_url_passthrough: !state.video_url_passthrough })} />
            </li>
          </ul>
        </div>
        <div className="peernote">{t("models.a_saved_change_reaches_only")}<code>{state.path}</code></div>
        {readOnly && <div className="peernote" role="status">{t("common.these_settings_are_read_only")}</div>}
        {error && <div className="why" role="alert">{error}</div>}
      </div>
    </Group>
  );
}
