package callback

import larkevent "github.com/larksuite/oapi-sdk-go/v3/event"

type CardActionTriggerEvent struct {
	*larkevent.EventV2Base                           // 事件基础数据
	*larkevent.EventReq                              // 请求原生数据
	Event                  *CardActionTriggerRequest `json:"event"` // 事件内容
}

func (m *CardActionTriggerEvent) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type CardActionTriggerRequest struct {
	Operator     *Operator       `json:"operator,omitempty"`
	Token        string          `json:"token,omitempty"` // 更新卡片用的token(凭证)
	Action       *CallBackAction `json:"action,omitempty"`
	Host         string          `json:"host,omitempty"`          // 宿主: im_message/im_top_notice
	DeliveryType string          `json:"delivery_type,omitempty"` // 卡片发送渠道: url_preview/
	Context      *Context        `json:"context,omitempty"`
}

type Operator struct {
	TenantKey *string `json:"tenant_key,omitempty"`
	UserID    *string `json:"user_id,omitempty"`
	OpenID    string  `json:"open_id,omitempty"`
}

type CallBackAction struct {
	Value      map[string]interface{} `json:"value"`
	Tag        string                 `json:"tag"`
	Option     string                 `json:"option"`
	Timezone   string                 `json:"timezone"`
	Name       string                 `json:"name"`
	FormValue  map[string]interface{} `json:"form_value"`
	InputValue string                 `json:"input_value"`
	Options    []string               `json:"options"`
	Checked    bool                   `json:"checked"`
}

type Context struct {
	URL           string `json:"url,omitempty"`
	PreviewToken  string `json:"preview_token,omitempty"`
	OpenMessageID string `json:"open_message_id,omitempty"`
	OpenChatID    string `json:"open_chat_id,omitempty"`
}

type CardActionTriggerResponse struct {
	Toast *Toast `json:"toast,omitempty"`
	Card  *Card  `json:"card,omitempty"`
}

type Toast struct {
	Type        string            `json:"type,omitempty"`
	Content     string            `json:"content,omitempty"`
	I18nContent map[string]string `json:"i18n,omitempty"`
}

type Card struct {
	// template/raw
	Type string `json:"type,omitempty"`
	// type为card_json时：data为larkcard.MessageCard; type为template时，data为TemplateCard
	Data interface{} `json:"data,omitempty"`
}

type TemplateCard struct {
	TemplateID          string                 `json:"template_id,omitempty"`
	TemplateVariable    map[string]interface{} `json:"template_variable,omitempty"`
	TemplateVersionName string                 `json:"template_version_name,omitempty"`
}

type URLPreviewGetEvent struct {
	*larkevent.EventV2Base                       // 事件基础数据
	*larkevent.EventReq                          // 请求原生数据
	Event                  *URLPreviewGetRequest `json:"event"` // 事件内容
}

func (m *URLPreviewGetEvent) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type URLPreviewGetRequest struct {
	Operator *Operator `json:"operator,omitempty"`
	Host     string    `json:"host,omitempty"` // 宿主: im_message/im_top_notice
	Context  *Context  `json:"context,omitempty"`
}

type URLPreviewGetResponse struct {
	Inline *Inline `json:"inline,omitempty"`
	Card   *Card   `json:"card,omitempty"`
}

type Inline struct {
	Title     string            `json:"title,omitempty"`
	I18nTitle map[string]string `json:"i18n_title,omitempty"`
	ImageKey  string            `json:"image_key,omitempty"`
	URL       *URL              `json:"url,omitempty"`
}

type URL struct {
	CopyURL string `json:"copy_url,omitempty"`
	IOS     string `json:"ios,omitempty"`
	Android string `json:"android,omitempty"`
	PC      string `json:"pc,omitempty"`
	Web     string `json:"web,omitempty"`
}
