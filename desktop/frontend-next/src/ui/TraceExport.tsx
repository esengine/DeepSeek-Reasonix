import { useCallback, useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError } from "../port/http_error";
import type { AgentPort, ObservabilityPatch, ObservabilitySettings } from "../port/port";
import { Group } from "./Group";
import { Switch } from "./Switch";

type Field = "endpoint" | "headerName" | "headerEnv" | "serviceName";

/** Hands each turn to the observability stack the person already runs, as
 *  OpenTelemetry traces. Off until they switch it on and name a collector; what
 *  is sent is structure, timing, counts and names, never prompts, tool
 *  arguments, results or paths. */
export function TraceExport({ port }: { port: AgentPort }) {
  const [state, setState] = useState<ObservabilitySettings | null>(null);
  const [draft, setDraft] = useState<Record<Field, string>>({ endpoint: "", headerName: "", headerEnv: "", serviceName: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [readOnly, setReadOnly] = useState(false);

  const adopt = useCallback((s: ObservabilitySettings) => {
    setState(s);
    setDraft({ endpoint: s.endpoint, headerName: s.headerName, headerEnv: s.headerEnv, serviceName: s.serviceName });
  }, []);

  useEffect(() => {
    let alive = true;
    port.observabilitySettings().then((s) => alive && adopt(s)).catch(() => alive && setState(null));
    return () => {
      alive = false;
    };
  }, [port, adopt]);

  const apply = useCallback(async (patch: ObservabilityPatch) => {
    setBusy(true);
    setError("");
    try {
      adopt(await port.saveObservabilitySettings(patch));
    } catch (e) {
      if (e instanceof HttpError && e.reason?.code === "observability.editing_disabled") setReadOnly(true);
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  }, [port, adopt]);

  if (!state) return null;
  const locked = busy || readOnly;
  const commit = (field: Field) => {
    if (draft[field].trim() !== state[field]) void apply({ [field]: draft[field].trim() });
  };

  return (
    <Group id="trace-export" title={t("data.trace_export")} hint={t("settings.trace_hint")}>
      <div className="box">
        <div className="lrow">
          <span className="tx">
            <span className="lb">{t("data.export_traces")}</span>
            <span className="ds">{!state.enabled ? t("data.off_now_nothing_is_sent") : state.endpoint ? t("data.on_sessions_opened_from_now") : t("data.on_but_there_is_no")}</span>
          </span>
          <Switch data-action="trace-export.enabled" on={state.enabled} busy={locked} label={t("data.export_traces")} onClick={() => void apply({ enabled: !state.enabled })} />
        </div>
        <div className="wa-url">
          <label className="peernote" htmlFor="trace-endpoint">{t("data.collector_address")}</label>
          <input id="trace-endpoint" className="peerfield" data-action-change="trace-export.endpoint" aria-label={t("data.collector_address")} placeholder="http://localhost:4318" value={draft.endpoint} disabled={locked}
            onChange={(e) => setDraft({ ...draft, endpoint: e.target.value })} onBlur={() => commit("endpoint")} />
        </div>
        <div className="wa-url">
          <label className="peernote" htmlFor="trace-headerName">{t("data.auth_header_name")}</label>
          <input id="trace-headerName" className="peerfield" data-action-change="trace-export.header-name" aria-label={t("data.auth_header_name")} placeholder="Authorization" value={draft.headerName} disabled={locked}
            onChange={(e) => setDraft({ ...draft, headerName: e.target.value })} onBlur={() => commit("headerName")} />
        </div>
        <div className="wa-url">
          <label className="peernote" htmlFor="trace-headerEnv">{t("data.environment_variable_holding_the_credential")}</label>
          <input id="trace-headerEnv" className="peerfield" data-action-change="trace-export.header-env" aria-label={t("data.environment_variable_holding_the_credential")} placeholder="OTEL_EXPORTER_AUTH" value={draft.headerEnv} disabled={locked}
            onChange={(e) => setDraft({ ...draft, headerEnv: e.target.value })} onBlur={() => commit("headerEnv")} />
        </div>
        <div className="peernote">
          {state.headerEnv
            ? (state.headerEnvSet ? t("data.the_variable_is_set_its") : t("data.the_variable_has_no_value"))
            : t("data.the_credential_is_never_in")}
        </div>
        <div className="wa-url">
          <label className="peernote" htmlFor="trace-serviceName">{t("data.service_name")}</label>
          <input id="trace-serviceName" className="peerfield" data-action-change="trace-export.service-name" aria-label={t("data.service_name")} placeholder="reasonix" value={draft.serviceName} disabled={locked}
            onChange={(e) => setDraft({ ...draft, serviceName: e.target.value })} onBlur={() => commit("serviceName")} />
        </div>
        <div className="peernote">{t("data.a_save_applies_to_sessions")}<code>{state.path}</code></div>
        {readOnly && <div className="peernote" role="status">{t("common.these_settings_are_read_only")}</div>}
        {error && <div className="why" role="alert">{error}</div>}
      </div>
    </Group>
  );
}
