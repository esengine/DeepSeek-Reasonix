/*
 * MIT License
 *
 * Copyright (c) 2022 Lark Technologies Pte. Ltd.
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice, shall be included in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 */

package larkapplication

import larkevent "github.com/larksuite/oapi-sdk-go/v3/event"

type P1OrderPaidV6 struct {
	*larkevent.EventReq
	*larkevent.EventBase
	Event *P1OrderPaidV6Data `json:"event"`
}

func (m *P1OrderPaidV6) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1OrderPaidV6Data struct {
	Type          string `json:"type,omitempty"`            // 事件类型
	AppID         string `json:"app_id,omitempty"`          // APP ID
	OrderID       string `json:"order_id,omitempty"`        // 用户购买付费方案时对订单ID 可作为唯一标识
	PricePlanID   string `json:"price_plan_id,omitempty"`   // 付费方案ID
	PricePlanType string `json:"price_plan_type,omitempty"` // 用户购买方案类型 "trial" -试用；"permanent"-免费；"per_year"-企业年付费；"per_month"-企业月付费；"per_seat_per_year"-按人按年付费；"per_seat_per_month"-按人按月付费；"permanent_count"-按次付费
	BuyCount      int64  `json:"buy_count,omitempty"`       // 套餐购买数量 目前都为1
	Seats         int64  `json:"seats,omitempty"`           // 表示购买了多少人份
	CreateTime    string `json:"create_time,omitempty"`     //
	PayTime       string `json:"pay_time,omitempty"`        //
	BuyType       string `json:"buy_type,omitempty"`        // 购买类型 buy普通购买 upgrade为升级购买 renew为续费购买
	SrcOrderID    string `json:"src_order_id,omitempty"`    // 当前为升级购买时(buy_type 为upgrade)，该字段表示原订单ID，升级后原订单失效，状态变为已升级(业务方需要处理)
	OrderPayPrice int64  `json:"order_pay_price,omitempty"` // 订单支付价格 单位分，
	TenantKey     string `json:"tenant_key,omitempty"`      // 企业标识
}

type P1AppUninstalledV6 struct {
	*larkevent.EventReq
	*larkevent.EventBase
	Event *P1AppUninstalledV6Data `json:"event"`
}

type P1AppUninstalledV6Data struct {
	AppID     string `json:"app_id,omitempty"`     // APP ID
	TenantKey string `json:"tenant_key,omitempty"` // 企业标识
	Type      string `json:"type,omitempty"`       // 事件类型
}

func (m *P1AppUninstalledV6) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1AppOpenV6 struct {
	*larkevent.EventReq
	*larkevent.EventBase
	Event *P1AppOpenV6Data `json:"event"`
}

func (m *P1AppOpenV6) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1AppOpenApplicantV6 struct {
	OpenID string `json:"open_id,omitempty"` // 用户对此应用的唯一标识，同一用户对不同应用的open_id不同
}

type P1AppOpenInstallerV6 struct {
	OpenID string `json:"open_id,omitempty"` // 用户对此应用的唯一标识，同一用户对不同应用的open_id不同
}

type P1AppOpenInstallerEmployeeV6 struct {
	OpenID string `json:"open_id,omitempty"` // 用户对此应用的唯一标识，同一用户对不同应用的open_id不同
}

type P1AppOpenV6Data struct {
	AppID             string                        `json:"app_id,omitempty"`             // App ID
	TenantKey         string                        `json:"tenant_key,omitempty"`         // 企业标识
	Type              string                        `json:"type,omitempty"`               // 事件类型
	Applicants        []*P1AppOpenApplicantV6       `json:"applicants,omitempty"`         // 应用的申请者，可能有多个
	Installer         *P1AppOpenInstallerV6         `json:"installer,omitempty"`          // 当应用被管理员安装时，返回此字段。如果是自动安装或由普通成员获取时，没有此字段
	InstallerEmployee *P1AppOpenInstallerEmployeeV6 `json:"installer_employee,omitempty"` // 当应用被普通成员安装时，返回此字段
}

type P1AppStatusChangedV6 struct {
	*larkevent.EventReq
	*larkevent.EventBase
	Event *P1AppStatusChangedV6Data `json:"event"`
}

func (m *P1AppStatusChangedV6) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1AppStatusChangedV6Data struct {
	AppID     string                       `json:"app_id,omitempty"`     // App ID
	TenantKey string                       `json:"tenant_key,omitempty"` // 企业标识
	Type      string                       `json:"type,omitempty"`       // 事件类型
	Status    string                       `json:"status,omitempty"`     //应用状态 start_by_tenant: 租户启用; stop_by_tenant: 租户停用; stop_by_platform: 平台停用
	Operator  *P1AppStatusChangeOperatorV6 `json:"operator,omitempty"`   // 仅status=start_by_tenant时有此字段
}

type P1AppStatusChangeOperatorV6 struct {
	OpenID  string `json:"open_id,omitempty"`  // 用户对此应用的唯一标识，同一用户对不同应用的open_id不同
	UserID  string `json:"user_id,omitempty"`  // 仅自建应用才会返回
	UnionId string `json:"union_id,omitempty"` // 用户在ISV下的唯一标识
}
