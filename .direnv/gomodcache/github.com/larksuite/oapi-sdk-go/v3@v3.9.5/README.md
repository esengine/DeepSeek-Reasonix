# 飞书开放接口SDK/Feishu OpenPlatform Server SDK

旨在让开发者便捷的调用飞书开放API、处理订阅的事件、处理服务端推送的卡片行为等。

Feishu Open Platform offers a series of server-side atomic APIs to achieve diverse functionalities. However, actual coding requires additional work, such as obtaining and maintaining access tokens, encrypting and decrypting data, and verifying request signatures. Furthermore, the lack of semantic descriptions for function calls and type system support can increase coding burdens.

To address these issues, Feishu Open Platform has developed the Open Interface SDK, which incorporates all lengthy logic processes, provides a comprehensive type system, and offers a semantic programming interface to enhance the coding experience.

## 介绍文档 Introduction Documents

- [开发前准备（安装） / Preparations before development(Install SDK)](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/server-side-sdk/golang-sdk-guide/preparations)
- [调用服务端 API / Calling Server-side APIs](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/server-side-sdk/golang-sdk-guide/calling-server-side-apis)
- [处理事件订阅 / Handle Events](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/server-side-sdk/golang-sdk-guide/handle-events)
- [处理卡片回调 / Handle Card Callbacks](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/server-side-sdk/golang-sdk-guide/handle-callback)
- [常见问题 / SDK FAQs](https://open.feishu.cn/document/uAjLw4CM/ukTMukTMukTM/server-side-sdk/faq)

## 高级封装 Channel 模块 / Channel Module

SDK 提供了一个基于 WebSocket 和 API Client 封装的 Channel 模块。它将飞书机器人接入过程中的事件监听、消息归一化、发送流式回复、上传媒体等操作进行了高层封装，让开发者能更专注业务逻辑。

The SDK provides a `Channel` module built on top of WebSocket and the API Client. It encapsulates event listening, message normalization, streaming replies, and media uploads, allowing developers to focus purely on business logic.

- [Channel 模块文档 / Channel Module Documentation (中文)](./doc/channel.zh.md)
- [Channel Module Documentation (English)](./doc/channel.md)

## 一键创建应用 / One-Click App Registration

SDK 提供了 `registration.RegisterApp` 方法，基于 OAuth 2.0 Device Authorization Grant（RFC 8628）协议实现一键创建应用。调用后会返回一个验证链接，用户在飞书/Lark 中打开链接或扫码完成授权后，即可自动注册应用并获取 `App ID` 与 `App Secret`，无需手动前往开发者后台创建。

The SDK provides `registration.RegisterApp` for one-click app creation based on OAuth 2.0 Device Authorization Grant (RFC 8628). It returns a verification URL that users can open in Feishu/Lark or render as a QR code. After authorization, the app is created automatically and the SDK returns the `App ID` and `App Secret`.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/scene/registration"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	result, err := registration.RegisterApp(ctx, &registration.Options{
		OnQRCode: func(info *registration.QRCodeInfo) {
			fmt.Printf("open or scan this url: %s\n", info.URL)
			fmt.Printf("the link expires in %d seconds\n", info.ExpireIn)
		},
		OnStatusChange: func(info *registration.StatusChangeInfo) {
			// status: polling | slow_down | domain_switched
			fmt.Printf("registration status: %s", info.Status)
			if info.Interval > 0 {
				fmt.Printf(", next poll after %d seconds", info.Interval)
			}
			fmt.Println()
		},
	})
	if err != nil {
		var regErr *registration.RegisterAppError
		if errors.As(err, &regErr) {
			fmt.Printf("register app failed: code=%s, description=%s\n", regErr.Code, regErr.Description)
			return
		}
		panic(err)
	}

	fmt.Println("App ID:", result.ClientID)
	fmt.Println("App Secret:", result.ClientSecret)

	client := lark.NewClient(result.ClientID, result.ClientSecret)
	_ = client
}
```

### `registration.RegisterApp` 参数 / Parameters

| 参数 Parameter | 描述 Description | 类型 Type | 必填 Required | 默认值 Default |
| ---- | ---- | ---- | ---- | ---- |
| `ctx` | 控制注册流程的超时与取消；取消 `context` 会终止轮询。 Controls timeout and cancellation for the registration flow; canceling the `context` stops polling. | `context.Context` | 是 Yes | - |
| `Options.Source` | 来源标识，会拼入二维码 URL 的 `source` 参数，格式为 `go-sdk/{source}`。 Source identifier appended to the QR URL as `go-sdk/{source}`. | `string` | 否 No | `go-sdk` |
| `Options.Domain` | 自定义飞书认证域名，支持传完整前缀，如 `https://accounts.feishu.cn`。 Custom Feishu accounts domain. A full base URL such as `https://accounts.feishu.cn` is supported. | `string` | 否 No | `https://accounts.feishu.cn` |
| `Options.LarkDomain` | 自定义 Lark 认证域名；检测到 `tenant_brand=lark` 时自动切换。 Custom Lark accounts domain used when `tenant_brand=lark` is detected. | `string` | 否 No | `https://accounts.larksuite.com` |
| `Options.AppPreset` | 预设应用信息，仅用于初始化创建页；用户仍可在页面修改，最终以页面提交为准。 Pre-filled app creation values; users can still edit them on the page. | `*registration.AppPreset` | 否 No | - |
| `Options.AppPreset.Avatar` | 应用头像 URL，支持 1-6 个；第一个默认选中。传原始 URL，SDK 会编码。头像展示、图片可访问性、GIF 取帧等由创建页处理。 App avatar URLs, 1-6 entries; first entry is selected by default. Pass raw URLs and the SDK encodes them. Page-side display rules are handled by the app creation page. | `[]string` | 否 No | - |
| `Options.AppPreset.Name` | 应用名称，支持 `{user}` 占位符；传原始值，SDK 会编码。 App name with `{user}` placeholder support; pass raw value and the SDK encodes it. | `string` | 否 No | - |
| `Options.AppPreset.Desc` | 应用描述，支持 `{user}` 占位符；传原始值，SDK 会编码。 App description with `{user}` placeholder support; pass raw value and the SDK encodes it. | `string` | 否 No | - |
| `Options.OnQRCode` | 验证链接就绪时的回调，参数为 `{ URL, ExpireIn }`。可直接展示链接，或将其渲染为二维码供用户扫码。 Callback invoked when the verification URL is ready. | `func(info *registration.QRCodeInfo)` | 是 Yes | - |
| `Options.OnStatusChange` | 轮询状态变化回调，参数为 `{ Status, Interval }`。`Status` 可能为 `polling`、`slow_down`、`domain_switched`。 Callback for polling status changes. | `func(info *registration.StatusChangeInfo)` | 否 No | - |

### 返回值 / Return Value

| 字段 Field | 类型 Type | 描述 Description |
| ---- | ---- | ---- |
| `ClientID` | `string` | 应用的 `App ID` / App ID |
| `ClientSecret` | `string` | 应用的 `App Secret` / App Secret |
| `UserInfo` | `*registration.UserInfo` | 扫码用户信息 / Scanning user info |
| `UserInfo.OpenID` | `string` | 扫码用户的 `open_id` / User `open_id` |
| `UserInfo.TenantBrand` | `string` | `"feishu"` 或 `"lark"` / `"feishu"` or `"lark"` |

### 错误处理 / Error Handling

返回的错误通常可以通过 `errors.As(err, &registration.RegisterAppError)` 获取 `Code` 与 `Description` 字段。更具体的错误类型还包括 `registration.AccessDeniedError` 和 `registration.ExpiredError`。

Returned errors usually expose `Code` and `Description` through `registration.RegisterAppError`. More specific types include `registration.AccessDeniedError` and `registration.ExpiredError`.

| `Code` | 描述 Description |
| ---- | ---- |
| `access_denied` | 用户拒绝授权 / User denied authorization |
| `expired_token` | 二维码过期或轮询超时 / QR code expired or polling timed out |
| `invalid_response` | 接口返回缺少必要字段或响应为空 / Response is empty or missing required fields |

## 扩展示例

我们还基于 SDK 封装了常用的 API 组合调用及业务场景示例，如：

* 消息
    * [发送文件消息](https://github.com/larksuite/oapi-sdk-go-demo/blob/main/composite_api/im/send_file.go)
    * [发送图片消息](https://github.com/larksuite/oapi-sdk-go-demo/blob/main/composite_api/im/send_image.go)
* 通讯录
    * [获取部门下所有用户列表](https://github.com/larksuite/oapi-sdk-go-demo/blob/main/composite_api/contact/list_user_by_department.go)
* 多维表格
    * [创建多维表格同时添加数据表](https://github.com/larksuite/oapi-sdk-go-demo/blob/main/composite_api/base/create_app_and_tables.go)
* 电子表格
    * [复制粘贴某个范围的单元格数据](https://github.com/larksuite/oapi-sdk-go-demo/blob/main/composite_api/sheets/copy_and_paste_by_range.go)
    * [下载指定范围单元格的所有素材列表](https://github.com/larksuite/oapi-sdk-go-demo/blob/main/composite_api/sheets/download_media_by_range.go)
* 教程
    * [机器人自动拉群报警](https://github.com/larksuite/oapi-sdk-go-demo/blob/main/quick_start/robot) ([开发教程](https://open.feishu.cn/document/home/message-development-tutorial/introduction))

更多示例可参考：https://github.com/larksuite/oapi-sdk-go-demo

## 加入交流互助群

[单击加入交流互助](https://applink.feishu.cn/client/chat/chatter/add_by_link?link_token=985nb30c-787a-4fbb-904d-2cf945534078)

## License

使用 MIT

