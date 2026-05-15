import { readConfig, webSearchEndpoint, webSearchEngine, writeConfig } from "../../../../config.js";
import { t } from "../../../../i18n/index.js";
import type { SlashHandler } from "../dispatch.js";

export const handlers: Record<string, SlashHandler> = {
  "search-engine": (args, _loop, ctx) => {
    const engine = args[0];
    if (!engine || (engine !== "mojeek" && engine !== "searxng" && engine !== "metaso")) {
      return {
        info: [
          t("handlers.webSearchEngine.currentEngine", { engine: webSearchEngine() }),
          t("handlers.webSearchEngine.endpoint", { url: webSearchEndpoint() }),
          "",
          t("handlers.webSearchEngine.usageHeader"),
          t("handlers.webSearchEngine.usageMojeek"),
          t("handlers.webSearchEngine.usageSearxng"),
          t("handlers.webSearchEngine.usageSearxngUrl"),
          t("handlers.webSearchEngine.usageMetaso"),
          "",
          t("handlers.webSearchEngine.alias"),
          "",
          t("handlers.webSearchEngine.searxngInfo"),
          t("handlers.webSearchEngine.searxngInstall"),
        ].join("\n"),
      };
    }

    const cfg = readConfig();
    cfg.webSearchEngine = engine;
    if (engine === "searxng" && args[1]) {
      const raw = args[1];
      cfg.webSearchEndpoint = raw.includes("://") ? raw : `http://${raw}`;
    }
    writeConfig(cfg);

    const note =
      engine === "searxng"
        ? t("handlers.webSearchEngine.switchedSearxngNote", { endpoint: webSearchEndpoint() })
        : engine === "metaso"
          ? t("handlers.webSearchEngine.switchedMetasoNote")
          : "";
    ctx.postInfo?.(t("handlers.webSearchEngine.switched", { engine, note }));

    const detail =
      engine === "searxng"
        ? t("handlers.webSearchEngine.confirmedDetail", { endpoint: webSearchEndpoint() })
        : "";
    return { info: t("handlers.webSearchEngine.confirmed", { engine, detail }) };
  },
  se: (args, loop, ctx) => handlers["search-engine"]!(args, loop, ctx),
};
