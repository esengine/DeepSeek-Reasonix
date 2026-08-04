# 语音输入（`/voice`）

`/voice` 将语音听写到输入框。音频在本地采集，发送到兼容 OpenAI 的
`POST /v1/audio/transcriptions` 接口，因此本地服务和云端服务都可使用 —— 区别只在配置，不在代码。

输入 `/voice`（别名 `/dictate`）开始。

**按住说话**（支持 Kitty 键盘协议的终端 —— kitty、Ghostty、WezTerm、foot 等）：
**按住空格键**说话，松开即停止。每次按住的内容都会追加到输入框，因此可以分段口述。
**Enter 直接发送**，且语音模式保持开启 —— 下一次按住即可口述下一轮，
整段对话都无需碰键盘。**Esc** 退出语音模式。

**开关模式**（其他终端）：立即开始录音，直到 **Enter** 采用或 **Esc** 放弃。

模式会自动选择。「按住」需要按键**释放**事件才知道何时结束，而传统终端只发送按下事件，
因此在无法获得释放事件时回退为开关模式，而不是绑定一个永远无法松开的键。
设置 `no_push_to_talk = true` 可强制使用开关模式。

按住说话模式下，空格是说话键，不会输入空格；其他按键仍正常进入输入框。
用 Enter 或 Esc 退出该模式。

## 配置

在配置中加入 `[voice]` 段。**没有默认接口地址**，必须指定音频发送到哪里。

### 云端（OpenAI、Groq、Fireworks 等）

```toml
[voice]
enabled     = true
url         = "https://api.openai.com/v1/audio/transcriptions"
model       = "whisper-1"
api_key_env = "OPENAI_API_KEY"
```

Groq 结构相同，只是主机和模型不同：

```toml
[voice]
enabled     = true
url         = "https://api.groq.com/openai/v1/audio/transcriptions"
model       = "whisper-large-v3-turbo"
api_key_env = "GROQ_API_KEY"
```

### 本地（Speaches、LocalAI、vLLM，或任意 faster-whisper 封装）

```toml
[voice]
enabled = true
url     = "http://127.0.0.1:8000/v1/audio/transcriptions"
model   = "Systran/faster-whisper-large-v3"
```

不设置 `api_key_env` 就不会发送 `Authorization` 头。

## 选项

| 键 | 默认值 | 说明 |
|---|---|---|
| `enabled` | `false` | 总开关。 |
| `url` | *(无)* | 必填。完整的转写接口地址。 |
| `model` | `whisper-1` | 本地服务通常接受并忽略此值。 |
| `api_key_env` | *(无)* | 用于 `Authorization: Bearer` 的凭据名称。与模型供应商密钥走同一套凭据存储 —— 密钥不会写入配置文件。 |
| `headers` | *(无)* | 为使用自定义鉴权方式的网关附加请求头。 |
| `language` | *(自动)* | ISO-639-1 语言提示，如 `"en"`、`"zh"`。显式设置比自动检测更快，且在短片段上更稳定。 |
| `prompt` | *(无)* | 引导模型正确拼写它不认识的词 —— 项目名、命令行参数、人名。建议设置。 |
| `temperature` | `0` | 非零时透传。 |
| `poll_ms` | `500` | 重新发送缓冲区的间隔，最小 200。 |
| `max_seconds` | `300` | 录音达到该长度自动停止。 |
| `no_push_to_talk` | `false` | 即使终端支持也强制使用开关模式。 |
| `device` | *(默认)* | 内置录音器的采集设备（如 ALSA 的 `hw:1,0`）。 |
| `record_cmd` | *(按平台)* | 完全覆盖采集命令。 |

## 依赖

`PATH` 中需要有采集工具：

- **Linux** —— `arecord`（`alsa-utils`）
- **macOS** —— `sox`
- **其他 / 自定义** —— 设置 `record_cmd`

`record_cmd` 必须在标准输出上产生
**16 kHz、单声道、16 位有符号小端 raw PCM**。例如 Linux 上使用 ffmpeg：

```toml
record_cmd = ["ffmpeg", "-f", "alsa", "-i", "default",
              "-f", "s16le", "-ar", "16000", "-ac", "1", "-"]
```

## 原理

录音过程中，客户端每个周期重发**整个已累积的缓冲区**，并用返回结果替换输入框内容，
而不是拼接片段。这让客户端保持简单，也让模型能在获得更多上下文后修正先前的词。

这样做之所以可行，是因为 Whisper 系列的编码器总是填充到固定的 30 秒窗口：
转写 1 秒和 30 秒的开销相近。在一块消费级 GPU 上运行 faster-whisper large-v3-turbo，
经 HTTP 端到端实测：

| 缓冲区 | 1s | 5s | 10s | 20s | 30s |
|---|---|---|---|---|---|
| 延迟 | 0.128s | 0.138s | 0.150s | 0.188s | 0.297s |

这种「平坦」是编码器的特性，并非所有 ASR 都如此 —— 同一硬件上，某个自回归语音模型
在相同区间实测为 0.43s → 4.35s。两道保护让客户端在任何后端上都保持稳健：

- **中间结果被限制在 30 秒尾窗口**，无论说多久，单次请求开销都有上界；
  最终一次会发送完整录音。
- **单请求在途**：同一时刻最多一个请求。若周期触发时仍有请求未返回，则**丢弃**该次而非排队，
  慢速接口因此不会堆积。响应携带其对应的缓冲区长度，若更新的结果已渲染则被丢弃。

开始录音时会先发一个预热请求：冷接口的首次调用明显更慢
（同一段音频实测冷启动 569ms，预热后约 150ms）。

## 排查

**「set url under [voice]」** —— 未配置接口地址，见上文「配置」。

**「`<NAME>` is not set」** —— `api_key_env` 指向的凭据没有值。
该错误在发出请求前就会报出，而不是等接口返回 `401`。

**专有名词转写错误** —— 模型没见过的词属预期情况，用 `prompt` 列出它们。

**找不到录音器** —— 安装 `alsa-utils`（Linux）或 `sox`（macOS），或设置 `record_cmd`。

**中间结果跟不上** —— 接口比 `poll_ms` 慢。可调大 `poll_ms`、换更快的模型，
或改用更近的服务。

### 哪些终端会上报按键释放事件

按住说话需要 Kitty 键盘协议并启用事件类型上报。**声称支持不等于支持**：某些终端
会回应能力查询，却从不发送释放事件，这会让一次按住永远无法结束。因此 `/voice`
只有在**真正收到过**释放事件之后才会启用按住模式；万一仍然卡住，45 秒后会自动
停止、保留转写结果，并在本次会话余下时间回退到切换模式。

| 终端 | 按住说话 |
|---|---|
| Kitty | 支持（实测） |
| Ghostty | 支持（实测） |
| WezTerm | 不支持，回退切换模式（实测；即使设置 `enable_kitty_keyboard = true`，它会声称支持事件类型，但仍不发送释放事件） |
| xterm、Terminal.app 等传统终端 | 不支持，使用切换模式 |

短按空格仍然输入普通空格：只有按住超过 `ptt_hold_ms`（默认 1 秒）才算说话，
因此语音模式开启时依然可以正常打字。`/voice` 会明确告知当前使用的是哪种模式，
按键行为不会静默改变。
