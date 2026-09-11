# Preset protocol endpoint audit

2026-09-07

Documentation review, not authenticated live verification. Missing routes are unconfirmed and remain manually configurable. Existing connections are not migrated.

Base URL excludes the request suffix: Chat = /chat/completions; Responses = /responses; Anthropic = /v1/messages.

| Scope | Chat Completions | Responses | Anthropic Messages |
|---|---|---|---|
| amd|cn|api | [https://developer.amd.com.cn/radeon/api/v1](https://amd-aim.github.io/radeon-cloud-docs/quickstart/) | — | — |
| anthropic|global|api | — | — | [https://api.anthropic.com](https://platform.claude.com/docs/en/api/messages/create) |
| baidu|cn|api | [https://qianfan.baidubce.com/v2](https://cloud.baidu.com/doc/qianfan/s/Smoghsq3g) | — | [https://qianfan.baidubce.com/anthropic](https://cloud.baidu.com/doc/qianfan/s/Smoghsq3g) |
| cerebras|global|api | [https://api.cerebras.ai/v1](https://inference-docs.cerebras.ai/resources/openai) | — | — |
| deepseek|global|api | [https://api.deepseek.com/v1](https://api-docs.deepseek.com/) | [https://api.deepseek.com](https://api-docs.deepseek.com/) | [https://api.deepseek.com/anthropic](https://api-docs.deepseek.com/guides/anthropic_api) |
| doubao|cn|api | [https://ark.cn-beijing.volces.com/api/v3](https://www.volcengine.com/docs/82379/1795150) | [https://ark.cn-beijing.volces.com/api/v3](https://www.volcengine.com/docs/82379/1795150) | — |
| fireworks|global|api | [https://api.fireworks.ai/inference/v1](https://docs.fireworks.ai/getting-started/quickstart) | [https://api.fireworks.ai/inference/v1](https://docs.fireworks.ai/guides/response-api) | [https://api.fireworks.ai/inference](https://docs.fireworks.ai/getting-started/quickstart) |
| gemini|global|api | [https://generativelanguage.googleapis.com/v1beta/openai](https://ai.google.dev/gemini-api/docs/openai) | — | — |
| gmi|global|api | [https://api.gmi-serving.com/v1](https://docs.gmicloud.ai/inference-engine/api-reference/llm-api-reference) | — | — |
| groq|global|api | [https://api.groq.com/openai/v1](https://console.groq.com/docs/responses-api) | [https://api.groq.com/openai/v1](https://console.groq.com/docs/responses-api) | — |
| huggingface|global|api | [https://router.huggingface.co/v1](https://huggingface.co/docs/inference-providers/en/guides/responses-api) | [https://router.huggingface.co/v1](https://huggingface.co/docs/inference-providers/en/guides/responses-api) | — |
| kilocode|global|api | [https://api.kilo.ai/api/gateway](https://kilo.ai/docs/gateway/api-reference) | — | — |
| kimi|cn|api | [https://api.moonshot.cn/v1](https://www.kimi.com/code/docs/en/) | — | — |
| kimi|global|api | [https://api.moonshot.ai/v1](https://platform.kimi.ai/docs/api/overview) | — | — |
| kimi|global|coding | [https://api.kimi.com/coding/v1](https://www.kimi.com/code/docs/en/) | — | [https://api.kimi.com/coding/](https://www.kimi.com/code/docs/en/) |
| lmstudio|local|local | [http://localhost:1234/v1](https://lmstudio.ai/docs/developer/openai-compat) | [http://localhost:1234/v1](https://lmstudio.ai/docs/developer/openai-compat) | [http://localhost:1234](https://lmstudio.ai/docs/developer/anthropic-compat) |
| longcat|global|api | [https://api.longcat.chat/openai/v1](https://longcat.chat/platform/docs/APIDocs.html) | — | [https://api.longcat.chat/anthropic](https://longcat.chat/platform/docs/APIDocs.html) |
| mimo|ams|token | [https://token-plan-ams.xiaomimimo.com/v1](https://mimo.mi.com/docs/en-US/tokenplan/Token%20Plan/quick-access) | — | [https://token-plan-ams.xiaomimimo.com/anthropic](https://mimo.mi.com/docs/en-US/tokenplan/Token%20Plan/quick-access) |
| mimo|cn|token | [https://token-plan-cn.xiaomimimo.com/v1](https://mimo.mi.com/docs/en-US/tokenplan/Token%20Plan/quick-access) | [https://token-plan-cn.xiaomimimo.com/v1](https://mimo.mi.com/docs/en-US/tokenplan/integration/codex-configuration) | [https://token-plan-cn.xiaomimimo.com/anthropic](https://mimo.mi.com/docs/en-US/tokenplan/Token%20Plan/quick-access) |
| mimo|global|api | [https://api.xiaomimimo.com/v1](https://mimo.mi.com/docs/en-US/tokenplan/integration/tools-overview) | [https://api.xiaomimimo.com/v1](https://mimo.mi.com/docs/en-US/api/chat/responses) | [https://api.xiaomimimo.com/anthropic](https://mimo.mi.com/docs/en-US/tokenplan/integration/tools-overview) |
| mimo|sgp|token | [https://token-plan-sgp.xiaomimimo.com/v1](https://mimo.mi.com/docs/en-US/tokenplan/Token%20Plan/quick-access) | — | [https://token-plan-sgp.xiaomimimo.com/anthropic](https://mimo.mi.com/docs/en-US/tokenplan/Token%20Plan/quick-access) |
| minimax|cn|api | [https://api.minimaxi.com/v1](https://platform.minimaxi.com/docs/guides/text-generation) | — | [https://api.minimaxi.com/anthropic](https://platform.minimaxi.com/docs/guides/text-generation) |
| minimax|global|api | [https://api.minimax.io/v1](https://platform.minimax.io/docs/api-reference/text-openai-api) | — | [https://api.minimax.io/anthropic](https://platform.minimax.io/docs/api-reference/text-anthropic-api) |
| mistral|global|api | [https://api.mistral.ai/v1](https://docs.mistral.ai/api/endpoint/chat) | — | — |
| modelscope|global|api | [https://api-inference.modelscope.cn/v1](https://modelscope.cn/models/Qwen/Qwen3.5-397B-A17B) | — | — |
| novita|global|api | [https://api.novita.ai/openai/v1](https://novita.ai/docs/api-reference/model-apis-llm-create-chat-completion) | — | [https://api.novita.ai/anthropic](https://novita.ai/docs/guides/llm-anthropic-compatibility) |
| nvidia|global|api | [https://integrate.api.nvidia.com/v1](https://docs.api.nvidia.com/nim/reference/meta-llama2-70b-infer) | — | — |
| ollama|global|api | [https://ollama.com/v1](https://docs.ollama.com/integrations/droid) | — | — |
| ollama|local|local | [http://localhost:11434/v1](https://docs.ollama.com/api/openai-compatibility) | [http://localhost:11434/v1](https://docs.ollama.com/api/openai-compatibility) | [http://localhost:11434](https://docs.ollama.com/api/anthropic-compatibility) |
| openai|global|api | [https://api.openai.com/v1](https://platform.openai.com/docs/api-reference/responses) | [https://api.openai.com/v1](https://platform.openai.com/docs/api-reference/responses) | — |
| opencode|global|go | [https://opencode.ai/zen/go/v1](https://opencode.ai/docs/go/) | [https://opencode.ai/zen/go/v1](https://opencode.ai/docs/go/) | [https://opencode.ai/zen/go](https://opencode.ai/docs/go/) |
| opencode|global|zen | [https://opencode.ai/zen/v1](https://opencode.ai/docs/zen/) | [https://opencode.ai/zen/v1](https://opencode.ai/docs/zen/) | [https://opencode.ai/zen](https://opencode.ai/docs/zen/) |
| openrouter|global|api | [https://openrouter.ai/api/v1](https://openrouter.ai/docs/api/api-reference/responses/create-responses) | [https://openrouter.ai/api/v1](https://openrouter.ai/docs/api/api-reference/responses/create-responses) | [https://openrouter.ai/api](https://openrouter.ai/docs/api/api-reference/anthropic-messages/create-messages) |
| ppio|cn|api | [https://api.ppio.com/openai](https://ppio.com/docs/third-party/OpenAI-Agents-SDK) | — | [https://api.ppio.com/anthropic](https://resource.ppio.com/docs/model/llm-anthropic-compatibility) |
| qiniu|cn|api | [https://api.qnaigc.com/v1](https://developer.qiniu.com/aitokenapi/13379/real-time-ai-interface-api) | — | [https://api.qnaigc.com](https://developer.qiniu.com/aitokenapi/13379/real-time-ai-interface-api) |
| qwen|cn|api | [https://dashscope.aliyuncs.com/compatible-mode/v1](https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope) | [https://dashscope.aliyuncs.com/compatible-mode/v1](https://help.aliyun.com/zh/model-studio/compatibility-with-openai-responses-api) | [https://dashscope.aliyuncs.com/apps/anthropic](https://help.aliyun.com/zh/model-studio/claude-code) |
| qwen|cn|coding | [https://coding.dashscope.aliyuncs.com/v1](https://help.aliyun.com/zh/model-studio/coding-plan-faq) | — | [https://coding.dashscope.aliyuncs.com/apps/anthropic](https://help.aliyun.com/zh/model-studio/coding-plan-faq) |
| qwen|global|api | [https://dashscope-intl.aliyuncs.com/compatible-mode/v1](https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope) | [https://dashscope-intl.aliyuncs.com/compatible-mode/v1](https://help.aliyun.com/zh/model-studio/compatibility-with-openai-responses-api) | — |
| qwen|global|coding | [https://coding-intl.dashscope.aliyuncs.com/v1](https://help.aliyun.com/zh/model-studio/coding-plan-faq) | — | [https://coding-intl.dashscope.aliyuncs.com/apps/anthropic](https://help.aliyun.com/zh/model-studio/coding-plan-faq) |
| scnet|global|token | [https://api.scnet.cn/api/llm/v1](https://www2.scnet.cn/ac/openapi/doc/2.0/moduleapi/tutorial/quickstart.html) | — | [https://api.scnet.cn/api/llm/anthropic](https://www2.scnet.cn/ac/openapi/doc/2.0/moduleapi/tutorial/quickstart.html) |
| siliconflow|cn|api | [https://api.siliconflow.cn/v1](https://docs.siliconflow.cn/docs/api/chat-completions-post) | — | [https://api.siliconflow.cn](https://docs.siliconflow.cn/cn/api-reference/chat-completions/messages) |
| stepfun|global|api | [https://api.stepfun.com/v1](https://platform.stepfun.com/docs/zh/api-reference/chat/chat-completion-create) | — | [https://api.stepfun.com](https://platform.stepfun.com/docs/zh/api-reference/chat/messages-create) |
| stepfun|global|coding | [https://api.stepfun.com/step_plan/v1](https://platform.stepfun.com/docs/zh/step-plan/quick-start) | — | [https://api.stepfun.com/step_plan](https://platform.stepfun.com/docs/zh/step-plan/quick-start) |
| together|global|api | [https://api.together.ai/v1](https://docs.together.ai/docs/inference/openai-compatibility) | — | — |
| token-rhythm|global|api | [https://tokenrhythm.studio/v1](https://tokenrhythm.studio/docs/api-integration) | — | [https://tokenrhythm.studio](https://tokenrhythm.studio/docs/api-integration) |
| vercel-ai-gateway|global|api | [https://ai-gateway.vercel.sh/v1](https://vercel.com/docs/ai-gateway/sdks-and-apis) | [https://ai-gateway.vercel.sh/v1](https://vercel.com/docs/ai-gateway/sdks-and-apis) | [https://ai-gateway.vercel.sh](https://vercel.com/docs/ai-gateway/sdks-and-apis) |
| xai|global|api | [https://api.x.ai/v1](https://docs.x.ai/developers/model-capabilities/text/comparison) | [https://api.x.ai/v1](https://docs.x.ai/developers/model-capabilities/text/comparison) | — |
| zai|cn|api | [https://open.bigmodel.cn/api/paas/v4](https://docs.bigmodel.cn/cn/guide/develop/claude/introduction) | — | [https://open.bigmodel.cn/api/anthropic](https://docs.bigmodel.cn/cn/guide/develop/claude/introduction) |
| zai|cn|coding | [https://open.bigmodel.cn/api/coding/paas/v4](https://docs.bigmodel.cn/cn/coding-plan/quick-start) | — | [https://open.bigmodel.cn/api/anthropic](https://docs.bigmodel.cn/cn/coding-plan/quick-start) |
| zai|global|api | [https://api.z.ai/api/paas/v4](https://docs.z.ai/guides/overview/quick-start) | — | — |
| zai|global|coding | [https://api.z.ai/api/coding/paas/v4](https://docs.z.ai/devpack/tool/others) | — | [https://api.z.ai/api/anthropic](https://docs.z.ai/devpack/tool/others) |

## Notes

- Unconfirmed existing routes are retained; absence is not evidence of lack of support.
- Qwen Responses uses the current /compatible-mode/v1 path. The documented workspace-specific domain can replace the shared host; never ship a literal {WorkspaceId} placeholder.
- OpenCode routes are model-specific. Changing protocol does not prove every model supports that route.
- xAI Anthropic is documented as deprecated and is intentionally not newly recommended.
- MiMo international Token Plan Responses endpoints were not confirmed; do not extrapolate from the China endpoint.
- The legacy StepFun Responses route is categorized under the ordinary API, not Step Plan. Its address is retained but not marked verified.
- The executable source of truth is internal/config/provider_protocol_endpoints.json. Sources and checkedOn are stored per route.
