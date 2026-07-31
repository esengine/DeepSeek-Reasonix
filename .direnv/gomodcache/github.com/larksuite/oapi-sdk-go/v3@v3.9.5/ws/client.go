package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	ws "github.com/gorilla/websocket"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcache "github.com/larksuite/oapi-sdk-go/v3/cache"
	larkcard "github.com/larksuite/oapi-sdk-go/v3/card"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
)

type Client struct {
	appID                   string
	appSecret               string
	clientAssertionProvider larkcore.ClientAssertionProvider
	logLevel                larkcore.LogLevel
	logger                  larkcore.Logger
	eventHandler            *dispatcher.EventDispatcher
	cardHandler             *larkcard.CardActionHandler
	domain                  string
	headers                 http.Header
	source                  string
	conn                    *ws.Conn
	connUrl                 *url.URL
	serviceID               string
	connID                  string
	autoReconnect           bool          // 是否自动重连，默认开启
	reconnectNonce          int           // 首次重连抖动，单位秒
	reconnectCount          int           // 重连次数，负数无限次
	reconnectInterval       time.Duration // 重连间隔
	pingInterval            time.Duration // Ping间隔
	cache                   *larkcache.Cache
	mu                      sync.Mutex
	onReady                 func()
	onError                 func(err error)
	onReconnecting          func()
	onReconnected           func()
	onDisconnected          func()
}

var bootstrapHTTPClient = http.DefaultClient

type bootstrapErrorResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type ClientOption func(cli *Client)

func WithEventHandler(handler *dispatcher.EventDispatcher) ClientOption {
	return func(cli *Client) {
		cli.eventHandler = handler
	}
}

//func WithCardHandler(handler *larkcard.CardActionHandler) ClientOption {
//	return func(cli *Client) {
//		cli.cardHandler = handler
//	}
//}

func WithLogLevel(level larkcore.LogLevel) ClientOption {
	return func(cli *Client) {
		cli.logLevel = level
	}
}

func WithLogger(logger larkcore.Logger) ClientOption {
	return func(cli *Client) {
		cli.logger = logger
	}
}

func WithAutoReconnect(b bool) ClientOption {
	return func(cli *Client) {
		cli.autoReconnect = b
	}
}

func WithDomain(domain string) ClientOption {
	return func(cli *Client) {
		cli.domain = domain
	}
}

func WithHeaders(header http.Header) ClientOption {
	return func(cli *Client) {
		cli.headers = header
	}
}

func WithSource(source string) ClientOption {
	return func(cli *Client) {
		cli.source = source
	}
}

func WithClientAssertionProvider(provider larkcore.ClientAssertionProvider) ClientOption {
	return func(cli *Client) {
		cli.clientAssertionProvider = provider
	}
}
func WithOnReady(f func()) ClientOption {
	return func(cli *Client) {
		cli.onReady = f
	}
}

func WithOnError(f func(err error)) ClientOption {
	return func(cli *Client) {
		cli.onError = f
	}
}

func WithOnReconnecting(f func()) ClientOption {
	return func(cli *Client) {
		cli.onReconnecting = f
	}
}

func WithOnReconnected(f func()) ClientOption {
	return func(cli *Client) {
		cli.onReconnected = f
	}
}

func WithOnDisconnected(f func()) ClientOption {
	return func(cli *Client) {
		cli.onDisconnected = f
	}
}

func (c *Client) SetOnReady(f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onReady = f
}

func (c *Client) SetOnReconnecting(f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onReconnecting = f
}

func (c *Client) SetOnReconnected(f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onReconnected = f
}

func (c *Client) SetOnError(f func(err error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onError = f
}

func (c *Client) SetOnDisconnected(f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onDisconnected = f
}

func (c *Client) Close() {
	c.autoReconnect = false
	c.disconnect(context.Background())
}

func NewClient(appId, appSecret string, opts ...ClientOption) *Client {
	cli := &Client{
		appID:             appId,
		appSecret:         appSecret,
		autoReconnect:     true,
		reconnectNonce:    30,
		reconnectCount:    -1,
		reconnectInterval: 2 * time.Minute,
		pingInterval:      2 * time.Minute,
		cache:             larkcache.New(30 * time.Second),
		domain:            lark.FeishuBaseUrl,
	}

	for _, opt := range opts {
		opt(cli)
	}

	if cli.logger == nil {
		cli.logger = larkcore.NewDefaultLogger(cli.logLevel)
	}

	return cli
}

func (c *Client) Start(ctx context.Context) (err error) {
	err = c.connect(ctx)
	if err != nil {
		c.logger.Error(ctx, c.fmtLog("connect failed, err: %v", err)...)
		if c.onError != nil {
			c.onError(err)
		}
		if _, ok := err.(*ClientError); ok {
			return
		}
		c.disconnect(ctx)
		if c.autoReconnect {
			if err = c.reconnect(ctx); err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		if c.onReady != nil {
			c.onReady()
		}
	}
	go c.pingLoop(ctx)
	select {}
}

func (c *Client) connect(ctx context.Context) (err error) {
	if c.conn != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return
	}

	// 获取建连URL
	connUrl, err := c.getConnURL(ctx)
	if err != nil {
		c.logger.Warn(ctx, c.fmtLog("get conn url failed, err: %v", err)...)
		return
	}

	// 验证URL
	u, err := url.Parse(connUrl)
	if err != nil {
		return
	}
	connID := u.Query().Get(DeviceID)
	serviceID := u.Query().Get(ServiceID)

	conn, resp, err := ws.DefaultDialer.Dial(connUrl, nil)
	if err != nil && resp == nil {
		return
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		// 连接失败
		return parseErr(resp)
	}

	c.conn = conn
	c.connUrl = u
	c.connID = connID
	c.serviceID = serviceID

	c.logger.Info(ctx, c.fmtLog("connected to %s", u)...)

	go c.receiveMessageLoop(ctx)
	return
}

func (c *Client) reconnect(ctx context.Context) (err error) {
	if c.onReconnecting != nil {
		c.onReconnecting()
	}

	// 首次重连随机抖动
	if c.reconnectNonce > 0 {
		rand.Seed(time.Now().UnixNano())
		num := rand.Intn(c.reconnectNonce * 1000)
		time.Sleep(time.Duration(num) * time.Millisecond)
	}

	if c.reconnectCount >= 0 {
		for i := 0; i < c.reconnectCount; i++ {
			success, err := c.tryConnect(ctx, i)
			if success {
				if c.onReconnected != nil {
					c.onReconnected()
				}
				return nil
			}
			if err != nil {
				return err
			}
			time.Sleep(c.reconnectInterval)
		}
		return fmt.Errorf("unable to connect to server after %d retries", c.reconnectCount)
	} else {
		i := 0
		for {
			success, err := c.tryConnect(ctx, i)
			if success {
				if c.onReconnected != nil {
					c.onReconnected()
				}
				return nil
			}
			if err != nil {
				return err
			}
			time.Sleep(c.reconnectInterval)
			i += 1
		}
	}
}

func (c *Client) tryConnect(ctx context.Context, cnt int) (bool, error) {
	c.logger.Info(ctx, c.fmtLog("trying to reconnect: %d", cnt+1)...)
	err := c.connect(ctx)
	if err == nil {
		return true, nil
	} else if _, ok := err.(*ClientError); ok {
		if c.onError != nil {
			c.onError(err)
		}
		return false, err
	} else {
		c.logger.Error(ctx, c.fmtLog("connect failed, err: %v", err)...)
		if c.onError != nil {
			c.onError(err)
		}
		return false, nil
	}
}

func (c *Client) disconnect(ctx context.Context) {
	if c.conn == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return
	}

	_ = c.conn.Close()
	c.logger.Info(ctx, c.fmtLog("disconnected to %s", c.connUrl)...)

	if c.onDisconnected != nil {
		c.onDisconnected()
	}

	defer func() {
		c.conn = nil
		c.connUrl = nil
		c.connID = ""
		c.serviceID = ""
	}()
}

func (c *Client) getConnURL(ctx context.Context) (url string, err error) {
	requestURL := strings.TrimRight(c.domain, "/") + GenEndpointUri
	body := &BootstrapRequest{AppID: c.appID}
	headers := make(http.Header)

	if c.clientAssertionProvider == nil && c.appSecret == "" {
		return "", NewClientError(larkcore.ErrCodeAppSecretAndClientAssertionEmpty, "appSecret and clientAssertionProvider cannot be nil")
	}

	if c.clientAssertionProvider != nil {
		aud, extractErr := extractAudFromWSURL(c.domain)
		if extractErr != nil {
			return "", extractErr
		}
		clientAssertionToken, retrieveErr := c.clientAssertionProvider.RetrieveToken(ctx, aud)
		if retrieveErr != nil {
			return "", retrieveErr
		}
		if clientAssertionToken == nil || clientAssertionToken.Value == "" {
			return "", NewClientError(larkcore.ErrCodeClientAssertionTokenEmpty, "client assertion token is empty")
		}
		body.ClientAssertion = clientAssertionToken.Value
		body.AppSecret = ""
		if clientAssertionToken.TargetInfo != nil {
			requestURL = buildWSProxyURL(clientAssertionToken.TargetInfo.TargetService, clientAssertionToken.TargetInfo.TargetPrefix, GenEndpointUri)
			headers.Set(larkcore.HeaderXTargetService, aud)
		}
	} else {
		body.AppSecret = c.appSecret
	}

	bs, err := json.Marshal(body)
	if err != nil {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewBuffer(bs))
	if err != nil {
		return
	}

	req.Header.Add("locale", "zh")
	req.Header.Add("Content-Type", "application/json")
	for k, values := range c.headers {
		for _, value := range values {
			req.Header.Add(k, value)
		}
	}
	for k, values := range headers {
		req.Header.Del(k)
		for _, value := range values {
			req.Header.Add(k, value)
		}
	}
	req.Header.Set("User-Agent", larkcore.UserAgent(c.source))
	resp, err := bootstrapHTTPClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	if resp.StatusCode != http.StatusOK {
		c.logger.Warn(ctx, "response status code %d", resp.StatusCode)
		serverMsg := "system busy"
		bootstrapErrResp := &bootstrapErrorResp{}
		if json.Unmarshal(respBody, bootstrapErrResp) == nil {
			if bootstrapErrResp.Msg != "" {
				serverMsg = bootstrapErrResp.Msg
			}
		}
		err = NewServerError(resp.StatusCode, serverMsg)
		return
	}

	endpointResp := &EndpointResp{}
	err = json.Unmarshal(respBody, endpointResp)
	if err != nil {
		return
	}

	switch endpointResp.Code {
	case OK:
	case SystemBusy:
		return "", NewServerError(endpointResp.Code, "system busy")
	case InternalError:
		return "", NewServerError(endpointResp.Code, endpointResp.Msg)
	default:
		return "", NewClientError(endpointResp.Code, endpointResp.Msg)
	}

	endpoint := endpointResp.Data
	if endpoint == nil || endpoint.Url == "" {
		err = NewServerError(http.StatusInternalServerError, "endpoint is null")
		return
	}

	url = endpoint.Url
	if endpoint.ClientConfig != nil {
		c.configure(endpoint.ClientConfig)
	}

	return
}

func extractAudFromWSURL(rawURL string) (string, error) {
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if parsedURL.Host != "" {
		return parsedURL.Host, nil
	}
	return "", fmt.Errorf("invalid url: %s", rawURL)
}

func buildWSProxyURL(targetService, targetPrefix, apiPath string) string {
	if !strings.Contains(targetService, "://") {
		targetService = "https://" + targetService
	}
	return targetService + targetPrefix + apiPath
}

func (c *Client) pingLoop(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			c.logger.Warn(ctx, c.fmtLog("ping loop panic, err: %v, stack: %s", err, string(debug.Stack()))...)
		}
		go c.pingLoop(ctx)
	}()

	for {
		if c.conn != nil {
			i, _ := strconv.ParseInt(c.serviceID, 10, 32)
			frame := NewPingFrame(int32(i))
			bs, _ := frame.Marshal()

			err := c.writeMessage(ws.BinaryMessage, bs)
			if err != nil {
				c.logger.Warn(ctx, c.fmtLog("ping failed, err: %v", err)...)
			} else {
				c.logger.Debug(ctx, c.fmtLog("ping success")...)
			}
		}
		time.Sleep(c.pingInterval)
	}
}

func (c *Client) receiveMessageLoop(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			c.logger.Error(ctx, c.fmtLog("receive message loop panic, err: %v, stack: %s", err, string(debug.Stack()))...)
		}
		c.disconnect(ctx)
		if c.autoReconnect {
			if err := c.reconnect(ctx); err != nil {
				c.logger.Error(ctx, err)
			}
		}
	}()

	for {
		if c.conn == nil {
			c.logger.Error(ctx, c.fmtLog("connection is closed, receive message loop exit")...)
			return
		}

		mt, msg, err := c.conn.ReadMessage()
		if err != nil {
			c.logger.Error(ctx, c.fmtLog("receive message failed, err: %v", err)...)
			return
		}

		if mt != ws.BinaryMessage {
			c.logger.Warn(ctx, c.fmtLog("receive unknown message, message_type: %d, message: %s", mt, msg)...)
			continue
		}

		go c.handleMessage(ctx, msg)
	}
}

func (c *Client) handleMessage(ctx context.Context, msg []byte) {
	defer func() {
		if err := recover(); err != nil {
			c.logger.Error(ctx, c.fmtLog("handle message panic, err: %v, stack: %s", err, string(debug.Stack()))...)
		}
	}()

	var frame Frame
	if err := frame.Unmarshal(msg); err != nil {
		c.logger.Error(ctx, c.fmtLog("unmarshal message failed, error: %v", err)...)
		return
	}

	switch FrameType(frame.Method) {
	case FrameTypeControl:
		c.handleControlFrame(ctx, frame)
	case FrameTypeData:
		c.handleDataFrame(ctx, frame)
	default:
	}
}

func (c *Client) handleControlFrame(ctx context.Context, frame Frame) {
	hs := Headers(frame.Headers)
	t := hs.GetString(HeaderType)

	switch MessageType(t) {
	case MessageTypePong:
		c.logger.Debug(ctx, c.fmtLog("receive pong")...)
		if len(frame.Payload) == 0 {
			return
		}
		conf := &ClientConfig{}
		if err := json.Unmarshal(frame.Payload, conf); err != nil {
			c.logger.Warn(ctx, c.fmtLog("unmarshal client config failed, err: %v", err)...)
			return
		}
		c.configure(conf)
	default:
	}
}

func (c *Client) handleDataFrame(ctx context.Context, frame Frame) {
	hs := Headers(frame.Headers)
	sum := hs.GetInt(HeaderSum)
	seq := hs.GetInt(HeaderSeq)
	msgID := hs.GetString(HeaderMessageID)
	traceID := hs.GetString(HeaderTraceID)
	type_ := hs.GetString(HeaderType)

	pl := frame.Payload
	if sum > 1 {
		// 合包
		pl = c.combine(msgID, sum, seq, pl)
		if pl == nil {
			return
		}
	}

	c.logger.Debug(ctx, c.fmtLog("receive message, message_type: %s, message_id: %s, trace_id: %s, payload: %s",
		type_, msgID, traceID, pl))

	var err error
	var rsp interface{}
	start := time.Now().UnixNano() / int64(time.Millisecond) // 兼容 go < 1.17
	switch MessageType(type_) {
	case MessageTypeEvent:
		rsp, err = c.eventHandler.Do(ctx, pl)
	case MessageTypeCard:
		return
	default:
		return
	}
	end := time.Now().UnixNano() / int64(time.Millisecond)
	hs.Add(HeaderBizRt, strconv.FormatInt(end-start, 10))

	resp := NewResponseByCode(http.StatusOK)
	if err != nil {
		c.logger.Error(ctx, c.fmtLog("handle message failed, message_type: %s, message_id: %s, trace_id: %s, err: %v",
			type_, msgID, traceID, err)...)
		resp = NewResponseByCode(http.StatusInternalServerError)
	} else {
		if rsp != nil { // for cardCallback
			resp.Data, err = json.Marshal(rsp)
			if err != nil {
				c.logger.Error(ctx, c.fmtLog("handle message failed, message_type: %s, message_id: %s, trace_id: %s, err: %v",
					type_, msgID, traceID, err)...)
				resp = NewResponseByCode(http.StatusInternalServerError)
			}
		}
	}

	p, _ := json.Marshal(resp)
	frame.Payload = p
	frame.Headers = hs
	bs, _ := frame.Marshal()

	err = c.writeMessage(ws.BinaryMessage, bs)
	if err != nil {
		c.logger.Error(ctx, c.fmtLog("response message failed, type: %s, message_id: %s, trace_id: %s, err: %v", type_, msgID, traceID, err)...)
		return
	}
}

func (c *Client) combine(msgID string, sum, seq int, bs []byte) []byte {
	val := c.cache.Get(msgID)
	if val == nil {
		buf := make([][]byte, sum)
		buf[seq] = bs
		c.cache.Set(msgID, buf, 5*time.Second)
		return nil
	}

	buf := val.([][]byte)
	buf[seq] = bs
	capacity := 0
	for _, v := range buf {
		if len(v) == 0 {
			c.cache.Set(msgID, buf, 5*time.Second)
			return nil
		}
		capacity += len(v)
	}

	pl := make([]byte, 0, capacity)
	for _, v := range buf {
		pl = append(pl, v...)
	}

	return pl
}

func (c *Client) writeMessage(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("connection is closed")
	}
	return c.conn.WriteMessage(messageType, data)
}

func (c *Client) fmtLog(format string, i ...interface{}) []interface{} {
	log := []interface{}{fmt.Sprintf(format, i...)}
	if c.connID != "" {
		log = append(log, fmt.Sprintf("[conn_id=%s]", c.connID))
	}

	return log
}

func (c *Client) configure(conf *ClientConfig) {
	c.reconnectCount = conf.ReconnectCount
	c.reconnectInterval = time.Duration(conf.ReconnectInterval) * time.Second
	c.reconnectNonce = conf.ReconnectNonce
	c.pingInterval = time.Duration(conf.PingInterval) * time.Second
}

func parseErr(resp *http.Response) error {
	code, _ := strconv.Atoi(resp.Header.Get(HeaderHandshakeStatus))
	msg := resp.Header.Get(HeaderHandshakeMsg)
	switch code {
	case AuthFailed:
		// Auth失败
		authCode, _ := strconv.Atoi(resp.Header.Get(HeaderHandshakeAuthErrCode))
		if authCode == ExceedConnLimit {
			return NewClientError(code, msg)
		} else {
			return NewServerError(code, msg)
		}
	case Forbidden:
		// 被封禁
		return NewClientError(code, msg)
	default:
		return NewServerError(code, msg)
	}
}

// EventHandler returns the configured event dispatcher.
func (c *Client) EventHandler() *dispatcher.EventDispatcher {
	return c.eventHandler
}
