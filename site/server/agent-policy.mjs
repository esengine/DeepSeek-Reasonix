const MAX_PROMPT_LENGTH = 4_000;
const MAX_ASSET_IDS = 10;
const ASSET_ID = /^IP-[A-Za-z0-9-]{2,96}$/;

const BOUNDARY_MESSAGE = "IP 任务助手仅处理已授权的文档、IP 资产、证据和 Wiki 分析；不会执行代码、系统操作、外部通信或正式知识变更。";

const RULES = [
  {
    code: "coding",
    pattern: /(?:帮我|请|直接|执行|需要|替我|给我)?\s*(?:写|编写|生成|开发|修改|调试|修复|运行|测试)\s*(?:一个|一段|这段|该)?\s*(?:代码|脚本|程序|应用|接口|前端|后端|python|javascript|typescript|java|go\b)|\b(?:write|develop|modify|debug|fix|run|test)\b.{0,24}\b(?:code|script|program|app|api|frontend|backend|python|javascript|typescript|java|golang)\b/iu,
  },
  {
    code: "system_access",
    pattern: /(?:执行|运行|调用|打开|连接|登录|读取|查询)\s*(?:一下|这个|该|服务器上的)?\s*(?:shell|bash|powershell|命令行|命令|sql|数据库|终端|服务器文件|本地文件|文件系统)|\b(?:execute|run|open|connect|login|read|query)\b.{0,24}\b(?:shell|bash|powershell|command|sql|database|terminal|server files?|filesystem)\b/iu,
  },
  {
    code: "external_network",
    pattern: /(?:访问|打开|抓取|爬取|下载|请求|调用)\s*(?:这个|该|以下)?\s*(?:https?:\/\/|网址|网站|url|网页|外部接口|api\b)|\b(?:visit|open|crawl|scrape|download|request|call)\b.{0,24}(?:https?:\/\/|\burl\b|\bwebsite\b|\bwebpage\b|\bexternal api\b)/iu,
  },
  {
    code: "destructive",
    pattern: /(?:删除|清空|销毁|抹除|覆盖|回滚)\s*(?:这份|该|所有|正式|当前)?\s*(?:文档|资产|wiki|知识|版本|数据|记录)|\b(?:delete|clear|destroy|erase|overwrite|rollback)\b.{0,24}\b(?:documents?|assets?|wiki|knowledge|versions?|data|records?)\b/iu,
  },
  {
    code: "publication",
    pattern: /(?:直接|立即|自动|帮我|请)?\s*(?:发布|上线|(?<!已)确认)\s*(?:所有|这份|该|正式|资产|wiki|关系|知识)|\b(?:publish|deploy|confirm)\b.{0,24}\b(?:assets?|wiki|relationships?|knowledge)\b/iu,
  },
  {
    code: "external_message",
    pattern: /(?:发|发送|投递|推送|通知)\s*(?:一封|这个|该|结果|报告|消息|邮件|短信|微信|飞书|给客户|给用户)|\b(?:send|email|message|notify)\b.{0,24}\b(?:results?|reports?|customers?|users?|email|message)\b/iu,
  },
  {
    code: "identity_admin",
    pattern: /(?:添加|删除|禁用|启用|邀请|修改|更改|设置|改成|绕过)\s*(?:成员|用户|账号|角色|管理员|权限|认证|安全控制)|\b(?:add|delete|disable|enable|invite|modify|change|set|bypass)\b.{0,24}\b(?:members?|users?|accounts?|roles?|admins?|permissions?|authentication|security controls?)\b/iu,
  },
];

function policyError(message) {
  const error = new Error(message);
  error.code = "INVALID_AGENT_REQUEST";
  return error;
}

function cleanText(value, maxLength) {
  return String(value ?? "").normalize("NFKC").replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/g, " ").replace(/\s+/g, " ").trim().slice(0, maxLength);
}

function withoutExplicitNegations(prompt) {
  return prompt
    .replace(/(?:不|无需|不要|禁止|不会|仅分析[^，。；]{0,24}不)(?:执行|运行|部署|发布|删除|发送|访问|抓取|修改|确认)[^，。；]{0,24}/gu, " ")
    .replace(/(?:只|仅)(?:分析|梳理|核查|比较|评估)[^，。；]{0,32}/gu, " 分析 ")
    .replace(/\b(?:do not|don't|never|without)\s+(?:execute|run|deploy|publish|delete|send|visit|scrape|modify|confirm)\b[^,.;]{0,24}/giu, " ")
    .replace(/\b(?:only|just)\s+(?:analyze|review|compare|assess)\b[^,.;]{0,32}/giu, " analyze ");
}

export function evaluateAgentRequest(value) {
  const prompt = cleanText(typeof value === "string" ? value : value?.prompt, MAX_PROMPT_LENGTH);
  if (!prompt) return { allowed: false, code: "invalid", message: "请输入要完成的文档 IP 或 Wiki 任务。" };
  const actionable = withoutExplicitNegations(prompt);
  const matched = RULES.find((rule) => rule.pattern.test(actionable));
  return matched
    ? { allowed: false, code: matched.code, message: BOUNDARY_MESSAGE }
    : { allowed: true, code: "allowed", message: "请求位于文档 IP 与 Wiki 分析边界内。" };
}

export function normalizeAgentRequest(input) {
  const prompt = cleanText(input?.prompt, MAX_PROMPT_LENGTH);
  if (!prompt) throw policyError("Agent task prompt is required");
  const templateId = cleanText(input?.templateId, 80).replace(/[^a-z_]/g, "");
  const assetIds = [...new Set((Array.isArray(input?.assetIds) ? input.assetIds : [])
    .map((id) => cleanText(id, 100))
    .filter((id) => ASSET_ID.test(id)))]
    .slice(0, MAX_ASSET_IDS);
  return { prompt, templateId, assetIds };
}

export const AGENT_BOUNDARY_MESSAGE = BOUNDARY_MESSAGE;
