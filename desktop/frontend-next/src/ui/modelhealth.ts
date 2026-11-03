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
  if (health === "checking") return t("modelhealth.checkingConnectionAndBalance");
  const connection: Record<string, string> = {
    reachable: "modelhealth.connectionReachableModelCallsUnverif",
    auth: "modelhealth.authenticationFailedCheckKeyOr",
    missing_key: "modelhealth.aPIKeyNotConfigured",
    payment_required: "modelhealth.paymentRequiredCheckYourAccount",
    quota_exhausted: "modelhealth.availableQuotaExhausted",
    rate_limited: "modelhealth.temporarilyRateLimitedRetryLater",
    timeout: "modelhealth.connectionCheckTimedOutRetry",
    unreachable: "modelhealth.connectionFailedCheckNetworkOr",
    redirect_refused: "modelhealth.redirectRefusedCheckTheAddress",
  };
  const wallet: Record<string, string> = {
    insufficient: "modelhealth.insufficientBalanceTopUpAnd",
    auth: "modelhealth.balanceQueryAuthenticationFailed",
    unsupported: "modelhealth.balanceUnknownNoQueryEndpoint",
    unknown: "modelhealth.balanceCouldNotBeConfirmed",
  };
  const issue = wallet[health.wallet];
  const status = t(connection[health.connection] ?? "modelhealth.connectionStatusUnconfirmed");
  return issue ? `${t(issue)} · ${status}` : status;
}

export function modelHealthStatus(health: ProviderHealth | "checking" | undefined): MenuStatus | undefined {
  if (!health || health !== "checking" && health.connection === "disabled") return undefined;
  const detail = modelHealthNote(health);
  if (health === "checking") return { label: t("modelhealth.checking"), tone: "checking", detail };
  if (health.wallet === "insufficient") return { label: t("common.insufficientBalance"), tone: "warning", detail };
  const failures: Record<string, string> = {
    auth: "modelhealth.authFailed", missing_key: "modelhealth.missingKey", payment_required: "modelhealth.paymentRequired",
    quota_exhausted: "modelhealth.quotaExhausted", rate_limited: "modelhealth.rateLimited", timeout: "modelhealth.timedOut",
    unreachable: "modelhealth.connectionFailed", redirect_refused: "modelhealth.redirectRefused",
  };
  if (failures[health.connection]) return { label: t(failures[health.connection]), tone: "warning", detail };
  if (health.wallet === "auth") return { label: t("modelhealth.balanceCheckFailed"), tone: "warning", detail };
  if (health.connection === "reachable") return { label: t("modelhealth.connected"), tone: "positive", detail };
  return { label: t("modelhealth.unconfirmed"), tone: "neutral", detail };
}
