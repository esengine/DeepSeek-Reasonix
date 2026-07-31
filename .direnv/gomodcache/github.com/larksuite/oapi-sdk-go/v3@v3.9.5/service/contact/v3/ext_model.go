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

package larkcontact

import larkevent "github.com/larksuite/oapi-sdk-go/v3/event"

type P1UserChangedV3Data struct {
	Type       string `json:"type"`               // 事件类型
	AppID      string `json:"app_id"`             // 应用ID
	TenantKey  string `json:"tenant_key"`         // 企业标识
	OpenID     string `json:"open_id,omitempty"`  // 员工对此应用的唯一标识，同一员工对不同应用的open_id不同
	EmployeeId string `json:"employee_id"`        // 即“用户ID”，仅企业自建应用会返回
	UnionId    string `json:"union_id,omitempty"` // 员工对此ISV的唯一标识，同一员工对同一个ISV名下所有应用的union_id相同
}

type P1UserChangedV3 struct {
	*larkevent.EventBase
	*larkevent.EventReq
	Event *P1UserChangedV3Data `json:"event"`
}

func (m *P1UserChangedV3) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1UserStatusV3 struct {
	IsActive   bool `json:"is_active"`   // 账号是否已激活
	IsFrozen   bool `json:"is_frozen"`   // 账号是否冻结
	IsResigned bool `json:"is_resigned"` // 是否离职
}
type P1UserStatusChangedV3Data struct {
	Type          string          `json:"type"`               // 事件类型
	AppID         string          `json:"app_id"`             // 应用ID
	TenantKey     string          `json:"tenant_key"`         // 企业标识
	OpenID        string          `json:"open_id,omitempty"`  // 员工对此应用的唯一标识，同一员工对不同应用的open_id不同
	EmployeeId    string          `json:"employee_id"`        // 即“用户ID”，仅企业自建应用会返回
	UnionId       string          `json:"union_id,omitempty"` // 员工对此ISV的唯一标识，同一员工对同一个ISV名下所有应用的union_id相同
	BeforeStatus  *P1UserStatusV3 `json:"before_status"`      // 变化前的状态
	CurrentStatus *P1UserStatusV3 `json:"current_status"`     // 变化后的状态
	ChangeTime    string          `json:"change_time"`        // 状态更新的时间
}

type P1UserStatusChangedV3 struct {
	*larkevent.EventBase
	*larkevent.EventReq
	Event *P1UserStatusChangedV3Data `json:"event"`
}

func (m *P1UserStatusChangedV3) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1DepartmentChangedV3Data struct {
	Type             string `json:"type"`               // 事件类型，包括 dept_add, dept_update, dept_delete
	AppID            string `json:"app_id"`             // 应用ID
	TenantKey        string `json:"tenant_key"`         // 企业标识
	OpenID           string `json:"open_id,omitempty"`  // 员工对此应用的唯一标识，同一员工对不同应用的open_id不同
	OpenDepartmentId string `json:"open_department_id"` // 部门的Id，已废弃
}

type P1DepartmentChangedV3 struct {
	*larkevent.EventBase
	*larkevent.EventReq
	Event *P1DepartmentChangedV3Data `json:"event"`
}

func (m *P1DepartmentChangedV3) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1ContactScopeChangedV3Data struct {
	Type      string `json:"type"`       // 事件类型
	AppID     string `json:"app_id"`     // 应用ID
	TenantKey string `json:"tenant_key"` //企业标识
}

type P1ContactScopeChangedV3 struct {
	*larkevent.EventBase
	*larkevent.EventReq
	Event *P1ContactScopeChangedV3Data `json:"event"`
}

func (m *P1ContactScopeChangedV3) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}
