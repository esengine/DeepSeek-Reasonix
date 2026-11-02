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
  if (health === "checking") return t("正在检测连接与余额…");
  const connection: Record<string, string> = {
    reachable: "连接正常，模型调用未验证",
    auth: "连接鉴权失败，请检查密钥或权限",
    missing_key: "未配置 API 密钥",
    payment_required: "服务商要求付费，请检查账户",
    quota_exhausted: "可用配额已耗尽",
    rate_limited: "请求暂时被限流，请稍后重试",
    timeout: "连接检测超时，请稍后重试",
    unreachable: "连接异常，请检查网络或服务商",
    redirect_refused: "连接跳转被拒绝，请检查地址",
  };
  const wallet: Record<string, string> = {
    insufficient: "余额不足，请充值后重试",
    auth: "余额查询鉴权失败",
    unsupported: "余额未知（未配置查询接口）",
    unknown: "余额暂时无法确认",
  };
  const issue = wallet[health.wallet];
  const status = t(connection[health.connection] ?? "连接状态尚未确认");
  return issue ? `${t(issue)} · ${status}` : status;
}

export function modelHealthStatus(health: ProviderHealth | "checking" | undefined): MenuStatus | undefined {
  if (!health || health !== "checking" && health.connection === "disabled") return undefined;
  const detail = modelHealthNote(health);
  if (health === "checking") return { label: t("检测中"), tone: "checking", detail };
  if (health.wallet === "insufficient") return { label: t("余额不足"), tone: "warning", detail };
  const failures: Record<string, string> = {
    auth: "鉴权失败", missing_key: "缺少密钥", payment_required: "需要付费",
    quota_exhausted: "配额耗尽", rate_limited: "暂时限流", timeout: "连接超时",
    unreachable: "连接异常", redirect_refused: "跳转被拒",
  };
  if (failures[health.connection]) return { label: t(failures[health.connection]), tone: "warning", detail };
  if (health.wallet === "auth") return { label: t("余额查询失败"), tone: "warning", detail };
  if (health.connection === "reachable") return { label: t("连接正常"), tone: "positive", detail };
  return { label: t("连接待确认"), tone: "neutral", detail };
}
