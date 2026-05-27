import {
  loadBraveApiKey,
  loadExaApiKey,
  loadMetasoApiKey,
  loadOllamaApiKey,
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
      (engine !== "bing" &&
        engine !== "bing-intl" &&
        engine !== "searxng" &&
        engine !== "metaso" &&
        engine !== "tavily" &&
        engine !== "perplexity" &&
        engine !== "exa" &&
        engine !== "brave" &&
        engine !== "ollama")
    ) {
      return {
        info: [
          t("handlers.webSearchEngine.currentEngine", { engine: webSearchEngine() }),
          t("handlers.webSearchEngine.endpoint", { url: webSearchEndpoint() }),
          "",
          t("handlers.webSearchEngine.usageHeader"),
          t("handlers.webSearchEngine.usageBing"),
          t("handlers.webSearchEngine.usageBingIntl"),
          t("handlers.webSearchEngine.usageSearxng"),
          t("handlers.webSearchEngine.usageSearxngUrl"),
          t("handlers.webSearchEngine.usageMetaso"),
          t("handlers.webSearchEngine.usageTavily"),
          t("handlers.webSearchEngine.usagePerplexity"),
          t("handlers.webSearchEngine.usageExa"),
          t("handlers.webSearchEngine.usageOllama"),
          t("handlers.webSearchEngine.usageBrave"),
          "",
          t("handlers.webSearchEngine.alias"),
          "",
          t("handlers.webSearchEngine.searxngInfo"),
          t("handlers.webSearchEngine.searxngInstall"),
        ].join("\n"),
      };
    }

    const cfg = readConfig();

    const apiKeyEngines = new Set(["tavily", "perplexity", "exa", "metaso", "ollama", "brave"]);
    if (apiKeyEngines.has(engine)) {
      const KEY_LOADERS: Record<string, () => string | undefined> = {
        tavily: loadTavilyApiKey,
        perplexity: loadPerplexityApiKey,
        exa: loadExaApiKey,
        ollama: loadOllamaApiKey,
        brave: loadBraveApiKey,
        metaso: loadMetasoApiKey,
      };
      const loadKey = KEY_LOADERS[engine] ?? loadMetasoApiKey;

      if (args[1]) {
        cfg.webSearchEngine = engine;
        (cfg as Record<string, unknown>)[`${engine}ApiKey`] = args[1];
        writeConfig(cfg);
        return {
          info: `${t("handlers.webSearchEngine.confirmed", { engine, detail: "" })} ${t("handlers.webSearchEngine.keySaved")}`,
        };
      }

      const existingKey = loadKey();
      if (existingKey) {
        cfg.webSearchEngine = engine;
        writeConfig(cfg);
        return { info: t("handlers.webSearchEngine.confirmed", { engine, detail: "" }) };
      }

      const envVar = `${engine.toUpperCase()}_API_KEY`;
      return { info: t("handlers.webSearchEngine.keyNeeded", { engine, envVar }) };
    }

    cfg.webSearchEngine = engine;
    if (engine === "searxng" && args[1]) {
      const raw = args[1];
      cfg.webSearchEndpoint = raw.includes("://") ? raw : `http://${raw}`;
    }
    writeConfig(cfg);

    const detail =
      engine === "searxng"
        ? t("handlers.webSearchEngine.confirmedDetail", { endpoint: webSearchEndpoint() })
        : "";
    return { info: t("handlers.webSearchEngine.confirmed", { engine, detail }) };
  },
  se: (args, loop, ctx) => handlers["search-engine"]!(args, loop, ctx),
};
