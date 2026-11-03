import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/http_error";
import type { AgentPort, SearxngProbe, WebAccessPatch, WebAccessSettings } from "../port/port";
import { Group } from "./Group";
import { Switch } from "./Switch";

type Kind = "search" | "fetch";

const NAME: Record<Kind, Record<string, string>> = {
  search: { searxng: "SearXNG", deepseek_api: "webAccess.deepSeekSearch" },
  fetch: { local: "webAccess.localFetch", deepseek_api: "webAccess.deepSeekFetch" },
};
const NOTE: Record<Kind, Record<string, string>> = {
  search: { searxng: "webAccess.aSearchServiceYouRun", deepseek_api: "webAccess.deepSeekSOwnSearchAPI" },
  fetch: { local: "webAccess.thisMachineDownloadsThePage", deepseek_api: "webAccess.deepSeekSOwnPageFetch" },
};
const PROBE: Record<string, string> = {
  "search.searxng_json_disabled": "webAccess.thisSearXNGDoesNotAllow",
  "search.searxng_unreachable": "webAccess.cannotReachThisAddressCheck",
  "search.searxng_not_searxng": "webAccess.thisAddressAnswersButNot",
  "search.searxng_redirected": "webAccess.thisAddressRedirectsElsewhereEnter",
  "search.searxng_address_refused": "webAccess.theTestWillNotConnect",
};
const probeSaid = (r: SearxngProbe) =>
  r.ok ? t("webAccess.connectedThisSearXNGReturnsJSON")
    : PROBE[r.code ?? ""] ? t(PROBE[r.code ?? ""])
      : t("webAccess.thisSearXNGRefusedTheTest", { status: String(r.status ?? "") });
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
    const title = kind === "search" ? t("common.webSearch2") : t("webAccess.webFetch");
    return (
      <div className="wa-sec" role="group" aria-label={title}>
        <div className="peernote"><b>{title}</b>{" "}{kind === "search" ? t("webAccess.triedInOrderTheFree") : t("webAccess.triedInOrderTheFree")}</div>
        {on.length === 0 && <div className="peernote">{kind === "search" ? t("webAccess.noSearchBackendIsOn") : t("webAccess.noFetchBackendIsOn")}</div>}
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
                    <button className="act" data-action="web-access.move" data-value="up" disabled={locked || idx === 0} aria-label={t("common.moveUp2", { name: said(kind, id) })}
                      onClick={() => void order(on.map((x, i) => (i === idx - 1 ? id : i === idx ? on[idx - 1] : x)))}>↑</button>
                    <button className="act" data-action="web-access.move" data-value="down" disabled={locked || idx === on.length - 1} aria-label={t("common.moveDown2", { name: said(kind, id) })}
                      onClick={() => void order(on.map((x, i) => (i === idx + 1 ? id : i === idx ? on[idx + 1] : x)))}>↓</button>
                  </span>
                )}
                <Switch data-action="web-access.backend" data-value={id} on={enabled} busy={locked}
                  label={t("common.enable", { name: said(kind, id) })}
                  onClick={() => void order(enabled ? on.filter((x) => x !== id) : [...on, id])} />
                {paid && (enabled || allowed.includes(id)) && (
                  <div className="wa-consent">
                    <span className="tx">
                      <span className="lb">{kind === "search" ? t("webAccess.agreeToBeBilledPer") : t("webAccess.agreeToBeBilledPer2")}</span>
                      <span className="ds">{state.deepseek_api_ready || allowed.includes(id) ? t("webAccess.onceYouAgreeItIs") : t("webAccess.thisNeedsADeepSeekProvider")}</span>
                    </span>
                    <Switch data-action="web-access.consent" data-value={`${kind}:${id}`} on={allowed.includes(id)} busy={locked || (!state.deepseek_api_ready && !allowed.includes(id))}
                      label={kind === "search" ? t("webAccess.agreeToBeBilledPer") : t("webAccess.agreeToBeBilledPer2")}
                      onClick={() => void consent(allowed.includes(id) ? allowed.filter((x) => x !== id) : [...allowed, id])} />
                  </div>
                )}
              </li>
            );
          })}
        </ul>
        {kind === "search" && on.includes("searxng") && (
          <div className="wa-url">
            <label className="peernote" htmlFor="searxng-url">{t("webAccess.searXNGAddress")}</label>
            <input id="searxng-url" className="peerfield" data-action-change="web-access.searxng-url" aria-label={t("webAccess.searXNGAddress")} placeholder="http://127.0.0.1:8080"
              value={url} disabled={locked} onChange={(e) => setUrl(e.target.value)}
              onBlur={() => { if (url.trim() !== state.searxng_url) void apply({ searxng_url: url.trim() }); }} />
            <div className="wa-test">
              <button className="act" data-action="web-access.probe" disabled={readOnly || probing || !url.trim()} onClick={() => void test()}>{probing ? t("webAccess.testing") : t("common.testIt")}</button>
              {probe && <div className={probe.ok ? "peernote" : "peernote bad"} role="status">{probeSaid(probe)}</div>}
            </div>
          </div>
        )}
      </div>
    );
  };

  return (
    <Group id="web-access" title={t("common.webSearchAndFetch")} hint={t("webAccess.whichBackendsTheAgentUses")}>
      <div className="box">
        {section("search")}
        {section("fetch")}
        <div className="wa-sec" role="group" aria-label={t("webAccess.videoAddresses")}>
          <div className="peernote"><b>{t("webAccess.videoAddresses")}</b>{" "}{t("webAccess.theModelCanHandA")}</div>
          <ul className="wa-list">
            <li>
              <span className="tx">
                <span className="lb">{t("webAccess.handVideoAddressesToThe")}</span>
                <span className="ds">{t("webAccess.whenOnTheAddressIs")}</span>
              </span>
              <Switch data-action="web-access.video-url" on={state.video_url_passthrough} busy={locked} label={t("webAccess.handVideoAddressesToThe")}
                onClick={() => void apply({ video_url_passthrough: !state.video_url_passthrough })} />
            </li>
          </ul>
        </div>
        <div className="peernote">{t("webAccess.aSavedChangeReachesOnly")}<code>{state.path}</code></div>
        {readOnly && <div className="peernote" role="status">{t("common.theseSettingsAreReadOnly")}</div>}
        {error && <div className="why" role="alert">{error}</div>}
      </div>
    </Group>
  );
}
