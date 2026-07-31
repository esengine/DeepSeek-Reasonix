package ws

const (
	GenEndpointUri = "/callback/ws/endpoint"
	DeviceID       = "device_id"
	ServiceID      = "service_id"
)

const (
	HeaderTimestamp            = "timestamp"   // 消息时间戳, 单位ms
	HeaderType                 = "type"        // 消息类型, Event/Card
	HeaderMessageID            = "message_id"  // 消息ID, 拆包后继承
	HeaderSum                  = "sum"         // 拆包数, 未拆包为1
	HeaderSeq                  = "seq"         // 包序号, 未拆包为0
	HeaderTraceID              = "trace_id"    // 链路ID
	HeaderInstanceID           = "instance_id" // 标识下行消息来源及上行消息去向的机器实例地址, 由 ip、port、pod_name 等信息加密获得
	HeaderBizRt                = "biz_rt"      // 业务处理时长，单位ms
	HeaderHandshakeStatus      = "Handshake-Status"
	HeaderHandshakeMsg         = "Handshake-Msg"
	HeaderHandshakeAuthErrCode = "Handshake-Autherrcode"
)

type MessageType string

const (
	MessageTypeEvent MessageType = "event"
	MessageTypeCard  MessageType = "card"
	MessageTypePing  MessageType = "ping"
	MessageTypePong  MessageType = "pong"
)

type FrameType int

const (
	FrameTypeControl FrameType = 0
	FrameTypeData    FrameType = 1
)

const (
	OK              = 0
	SystemBusy      = 1
	Forbidden       = 403
	AuthFailed      = 514
	ExceedConnLimit = 1000040350
	InternalError   = 1000040343
)
