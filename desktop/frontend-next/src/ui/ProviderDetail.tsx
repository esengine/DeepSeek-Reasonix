import { useState } from "react";
import { t } from "../i18n";
import type { ProviderCheck } from "../port/port";
import { EditConn } from "./EditConn";
import type { Account, Port } from "./Providers";
import { KIND_LABEL } from "./vendors";
import { PROVIDER_EDIT_DISABLED, reason } from "../i18n/kernel";
import { HttpError } from "../port/http_error";
import { checkFailure } from "./provider_check";
import { StudioIcon } from "./StudioIcon";

// The selected account, edited in place. The protocol is a switch on it rather
// than a fact on a row, because both entries are the same key at the same host;
// 测试连接 is what turns "which protocol did we record" back into a finding.
export function ProviderDetail({
  a, port, busy, setBusy, kind, onProtocol, onRemove, removeFailed, onRename, onEdited, onChecked, onDirty, declare,
}: {
  a: Account; port: Port; busy: string; setBusy: (b: string) => void;
  kind: string; onProtocol: (kind: string) => void; onRemove: (name: string) => void; removeFailed: string; onRename: () => void;
  onEdited: () => void | Promise<void>; onChecked: () => void; onDirty: (dirty: boolean) => void; declare?: string;
}) {
  const [found, setFound] = useState<ProviderCheck | null>(null);
  // A refusal is not a failed probe. The kernel withholds these routes from a
  // server reachable over the network, because adding a source writes a key
  // into the credential store of the machine running the kernel.
  const [refused, setRefused] = useState("");
  const [unanswered, setUnanswered] = useState("");
  // Revert and save both hand the form a fresh start from what is stored, so
  // the form remounts only once the list holds what the kernel has.
  const [revision, setRevision] = useState(0);
  const [saved, setSaved] = useState("");
  const entry = a.byKind[kind] ?? a.byKind[a.kinds[0]];
  const checking = busy === `check:${entry.name}`;
  const inUse = a.kinds.some((k) => a.byKind[k].inUse);

  const check = async () => {
    setBusy(`check:${entry.name}`);
    setFound(null);
    setUnanswered("");
    setRefused("");
    try {
      setFound(await port.checkProvider(entry.name));
      onChecked();
    } catch (e) {
      // Read off the code the kernel sent, never the status: 403 is also what a
      // gateway in front of it answers, and that is a different thing to do next.
      if (e instanceof HttpError && e.reason?.code === PROVIDER_EDIT_DISABLED) setRefused(reason(e));
      else setUnanswered(reason(e));
    } finally {
      setBusy("");
    }
  };

  // What the endpoint answered with that this connection does not list. The
  // vendor adds models to endpoints we already know; a stored list never finds
  // out on its own, and the probe is the one place that already has the answer.
  const unlisted = (found?.models ?? []).filter((m) => !entry.models.includes(m));
  const options = a.kinds.length > 1;
  return (
    <section className="pdetail" aria-label={a.label}>
      <header className="pdetail-hd">
        <span className="tx">
          <span className="pdetail-name">
            <h3>{a.label}</h3>
            <button className="pdetail-rename" data-action="provider.rename-start" data-target={a.key} disabled={busy !== ""}
              aria-label={t("models.rename_name_f2", { name: a.label })} title={t("models.rename_name_f2", { name: a.label })}
              onClick={onRename}><StudioIcon name="edit" /></button>
          </span>
          <small title={`${t(KIND_LABEL[entry.kind] ?? entry.kind)} · ${a.host}`}>{t(KIND_LABEL[entry.kind] ?? entry.kind)} · {a.host}</small>
        </span>
        {inUse && <i className="pdetail-use">{t("models.in_use")}</i>}
        <button className="act" data-action="provider.probe" onClick={check} disabled={busy !== ""}>
          {t(checking ? "models.testing_2" : "common.test_it")}
        </button>
        <button className="act" data-danger data-action="provider.remove" data-target={entry.name} onClick={() => onRemove(entry.name)} disabled={busy !== ""}>
          {t("common.delete")}
        </button>
      </header>
      {removeFailed && (
        <div className="find" data-lvl="warn" role="alert">
          <span className="t">{t("common.that_did_not_go_through")}</span>
          <span className="why">{removeFailed}</span>
        </div>
      )}
      {refused && (
        <div className="find" data-lvl="warn" role="status">
          <span className="t">{refused}</span>
          <span className="why">{t("models.model_sources_are_configured_on")}</span>
        </div>
      )}
      {unanswered && (
        <div className="find" data-lvl="warn" role="status">
          <span className="t">{t("models.unreachable")}</span>
          <span className="why">{unanswered}</span>
        </div>
      )}
      {found && (
        <div className="find" data-lvl={found.ok ? "ok" : "warn"} role="status">
          <span className="t">
            {found.ok
              ? `${t("common.connected")} · ${t(KIND_LABEL[found.kind ?? ""] ?? found.kind ?? "")} · ${t("models.n_models", { n: found.models?.length ?? 0 })}`
              : t("models.unreachable")}
          </span>
          <span className="why">
            {!found.ok && checkFailure(found)}
            {found.ok && found.matches === false &&
              t("models.recorded_as_had_but_it", { had: t(KIND_LABEL[entry.kind] ?? entry.kind), got: t(KIND_LABEL[found.kind ?? ""] ?? found.kind ?? "") })}
            {found.ok && found.matches !== false && t("models.the_key_works_and_the")}
            {found.ok && found.baseUrl && " " + t("models.the_model_list_only_answers", { url: found.baseUrl })}
            {found.ok && found.noProxy && " " + t("models.the_proxy_cannot_reach_it")}
            {found.ok && unlisted.length > 0 &&
              " " + t("models.this_endpoint_has_n_more", {
                n: unlisted.length,
                names: unlisted.slice(0, 3).join("、") + (unlisted.length > 3 ? "…" : ""),
              })}
          </span>
        </div>
      )}
      {options && (
        <div className="pdetail-options">
          {a.kinds.length > 1 && (
            <div className="vway">
              <span className="lb">{t("common.protocol")}</span>
              <div className="seg" role="group" aria-label={t("models.protocol_for_name", { name: a.label })}>
                {a.kinds.map((k) => (
                  <button key={k} data-action="provider.protocol" data-target={entry.name} data-value={k} aria-pressed={k === kind} disabled={busy !== ""} onClick={() => onProtocol(k)}>
                    {t(KIND_LABEL[k] ?? k)}
                    {/* A door that carries a capability the other lacks has to say
                        so on itself: switching is otherwise a silent downgrade. */}
                    {a.byKind[k].canWebSearch && <i className="perk">{t("models.web_search")}</i>}
                  </button>
                ))}
              </div>
              <span className="why">
                {t(a.kinds.some((k) => a.byKind[k].canWebSearch) && !entry.canWebSearch ? "models.two_ways_in_to_the_2" : "models.two_ways_in_to_the")}
              </span>
            </div>
          )}
        </div>
      )}
      <EditConn
        key={`${entry.name}:${revision}`}
        entry={entry}
        label={a.label}
        wireFixed={a.kinds.length > 1}
        initialCheck={found?.ok ? found : undefined}
        port={port}
        busy={busy}
        setBusy={setBusy}
        declare={!!declare && entry.name === declare}
        justSaved={saved === entry.name}
        onDirty={onDirty}
        onDone={async () => {
          await onEdited();
          setSaved(entry.name);
          setRevision((r) => r + 1);
        }}
        onRevert={() => {
          setSaved("");
          setRevision((r) => r + 1);
        }}
        onSaved={onEdited}
      />
    </section>
  );
}
