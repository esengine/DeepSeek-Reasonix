import { useCallback, useEffect, useRef, useState } from "react";
import type { AgentPort, ModelEntry, ProviderHealth } from "../port/port";
import { t } from "../i18n";
import type { MenuStatus } from "./Menu";

export type ModelHealth = Readonly<Record<string, ProviderHealth | "checking">>;

export function useModelHealth(port: AgentPort, pulse: number) {
  const [models, setModels] = useState<ModelEntry[]>([]);
  const modelLoad = useRef(0);
  const loadModels = useCallback(() => {
    const version = ++modelLoad.current;
    return port.models().catch(() => []).then((next) => {
      if (version === modelLoad.current) setModels(next);
      return next;
    });
  }, [port]);
  useEffect(() => {
    void loadModels();
    return () => { modelLoad.current++; };
  }, [loadModels, pulse]);
  const [readings, setReadings] = useState<ModelHealth>({});
  const generation = useRef(0);
  useEffect(() => {
    generation.current++;
    setReadings({});
    return () => { generation.current++; };
  }, [port, pulse]);
  const check = useCallback(async (listing: Promise<ModelEntry[]>) => {
    const version = ++generation.current;
    setReadings({});
    const [models, settings] = await Promise.all([listing, port.providerHealthSettings().catch(() => null)]);
    if (generation.current !== version) return;
    if (!settings?.enabled) return;
    const names = [...new Set(models.filter((m) => m.kind !== "extension" && !m.provider.startsWith("plugin/")).map((m) => m.provider))];
    setReadings(Object.fromEntries(names.map((name) => [name, "checking"])));
    let next = 0;
    async function worker() {
      while (next < names.length && generation.current === version) {
        const name = names[next++];
        let result: ProviderHealth;
        try { result = await port.providerHealth(name); }
        catch { result = { connection: "unknown", wallet: "unknown", checkedAt: 0 }; }
        if (generation.current === version) setReadings((prior) => ({ ...prior, [name]: result }));
      }
    }
    await Promise.all(Array.from({ length: Math.min(3, names.length) }, worker));
  }, [port]);
  return { models, loadModels, readings, check };
}

export function modelHealthNote(health: ProviderHealth | "checking" | undefined): string | undefined {
  if (!health || health !== "checking" && health.connection === "disabled") return undefined;
  if (health === "checking") return t("models.checking_connection_and_balance");
  const connection: Record<string, string> = {
    reachable: "models.connection_reachable_model_calls_unverified",
    auth: "models.authentication_failed_check_key_or",
    missing_key: "models.api_key_not_configured",
    payment_required: "models.payment_required_check_your_account",
    quota_exhausted: "models.available_quota_exhausted",
    rate_limited: "models.temporarily_rate_limited_retry_later",
    timeout: "models.connection_check_timed_out_retry",
    unreachable: "models.connection_failed_check_network_or",
    redirect_refused: "models.redirect_refused_check_the_address",
  };
  const wallet: Record<string, string> = {
    insufficient: "models.insufficient_balance_top_up_and",
    auth: "models.balance_query_authentication_failed",
    unsupported: "models.balance_unknown_no_query_endpoint",
    unknown: "models.balance_could_not_be_confirmed",
  };
  const issue = wallet[health.wallet];
  const status = t(connection[health.connection] ?? "models.connection_status_unconfirmed");
  return issue ? `${t(issue)} · ${status}` : status;
}

export function modelHealthStatus(health: ProviderHealth | "checking" | undefined): MenuStatus | undefined {
  if (!health || health !== "checking" && health.connection === "disabled") return undefined;
  const detail = modelHealthNote(health);
  if (health === "checking") return { label: t("models.checking"), tone: "checking", detail };
  if (health.wallet === "insufficient") return { label: t("common.insufficient_balance"), tone: "warning", detail };
  const failures: Record<string, string> = {
    auth: "models.auth_failed", missing_key: "models.missing_key", payment_required: "models.payment_required",
    quota_exhausted: "models.quota_exhausted", rate_limited: "models.rate_limited", timeout: "models.timed_out",
    unreachable: "models.connection_failed", redirect_refused: "models.redirect_refused",
  };
  if (failures[health.connection]) return { label: t(failures[health.connection]), tone: "warning", detail };
  if (health.wallet === "auth") return { label: t("models.balance_check_failed"), tone: "warning", detail };
  if (health.connection === "reachable") return { label: t("models.connected"), tone: "positive", detail };
  return { label: t("models.unconfirmed"), tone: "neutral", detail };
}
