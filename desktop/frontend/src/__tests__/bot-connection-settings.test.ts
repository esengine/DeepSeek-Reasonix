import { botConnectionLabel } from "../components/botConnectionSettings";
import type { useT } from "../lib/i18n";
import type { BotConnectionView } from "../lib/types";

const t = ((key: string) => key === "settings.botDingtalk" ? "DingTalk" : key) as ReturnType<typeof useT>;
const connection = {
  provider: "dingtalk",
  domain: "dingtalk",
} as BotConnectionView;

const label = botConnectionLabel(connection, t);
if (label !== "DingTalk") {
  throw new Error(`DingTalk connection label = ${JSON.stringify(label)}, want \"DingTalk\"`);
}

console.log("bot connection settings: DingTalk label passed");
