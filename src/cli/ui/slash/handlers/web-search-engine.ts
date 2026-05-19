import {
  loadExaApiKey,
  loadMetasoApiKey,
  loadPerplexityApiKey,
  loadTavilyApiKey,
  readConfig,
  webSearchEndpoint,
  webSearchEngine,
  writeConfig,
} from "../../../../config.js";
import { t } from "../../../../i18n/index.js";
import type { SlashHandler } from "../dispatch.js";

export const handlers: Record<string, SlashHandler> = {
  "search-engine": (args, _loop, ctx) => {
    const engine = args[0];
    if (
      !engine ||
      (engine !== "mojeek" &&
        engine !== "searxng" &&
        engine !== "metaso" &&
        engine !== "tavily" &&
        engine !== "perplexity" &&
        engine !== "exa")
    ) {
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
          t("handlers.webSearchEngine.usageTavily"),
          t("handlers.webSearchEngine.usagePerplexity"),
          t("handlers.webSearchEngine.usageExa"),
          "",
          t("handlers.webSearchEngine.alias"),
          "",
          t("handlers.webSearchEngine.searxngInfo"),
          t("handlers.webSearchEngine.searxngInstall"),
        ].join("\n"),
      };
    }

    const cfg = readConfig();

    // ── API-key engines ──
    const apiKeyEngines = new Set(["tavily", "perplexity", "exa", "metaso"]);
    if (apiKeyEngines.has(engine)) {
      // Resolve the loader for the current engine
      const loadKey =
        engine === "tavily"
          ? loadTavilyApiKey
          : engine === "perplexity"
            ? loadPerplexityApiKey
            : engine === "exa"
              ? loadExaApiKey
              : loadMetasoApiKey;

      if (args[1]) {
        // User provided key inline → save to config + switch
        cfg.webSearchEngine = engine;
        (cfg as Record<string, unknown>)[`${engine}ApiKey`] = args[1];
        writeConfig(cfg);
        return {
          info: `${t("handlers.webSearchEngine.confirmed", { engine, detail: "" })} ${t("handlers.webSearchEngine.keySaved")}`,
        };
      }

      const existingKey = loadKey();
      if (existingKey) {
        // Key already configured → switch normally, no config hint needed
        cfg.webSearchEngine = engine;
        writeConfig(cfg);
        return { info: t("handlers.webSearchEngine.confirmed", { engine, detail: "" }) };
      }

      // No key configured → don't switch, show guidance with env var name
      const envVar = `${engine.toUpperCase()}_API_KEY`;
      return { info: t("handlers.webSearchEngine.keyNeeded", { engine, envVar }) };
    }

    // ── Engines without API keys (mojeek, searxng) ──
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
          : engine === "tavily"
            ? t("handlers.webSearchEngine.switchedTavilyNote")
            : engine === "perplexity"
              ? t("handlers.webSearchEngine.switchedPerplexityNote")
              : engine === "exa"
                ? t("handlers.webSearchEngine.switchedExaNote")
                : "";
    const detail =
      engine === "searxng"
        ? t("handlers.webSearchEngine.confirmedDetail", { endpoint: webSearchEndpoint() })
        : "";
    return { info: t("handlers.webSearchEngine.confirmed", { engine, detail }) };
  },
  se: (args, loop, ctx) => handlers["search-engine"]!(args, loop, ctx),
};
