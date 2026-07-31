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

package larkapproval

import larkevent "github.com/larksuite/oapi-sdk-go/v3/event"

type P1LeaveApprovalV4 struct {
	*larkevent.EventReq
	*larkevent.EventBase
	Event *P1LeaveApprovalV4Data `json:"event"`
}

func (m *P1LeaveApprovalV4) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1LeaveApprovalV4Data struct {
	AppID              string `json:"app_id,omitempty"`               // APP ID
	TenantKey          string `json:"tenant_key,omitempty"`           // 企业标识
	Type               string `json:"type,omitempty"`                 // 事件回调此处固定为event_callback
	InstanceCode       string `json:"instance_code,omitempty"`        // 审批实例Code
	UserID             string `json:"user_id,omitempty"`              // 用户id
	OpenID             string `json:"open_id,omitempty"`              // 用户open_id
	OriginInstanceCode string `json:"origin_instance_code,omitempty"` // 销假单关联的原始单据
	StartTime          int64  `json:"start_time,omitempty"`           // 销假单关联的原始单据
	EndTime            int64  `json:"end_time,omitempty"`             // 审批结束时间

	LeaveFeedingArriveLate int64 `json:"leave_feeding_arrive_late,omitempty"` //上班晚到（哺乳假相关）
	LeaveFeedingLeaveEarly int64 `json:"leave_feeding_leave_early,omitempty"` //下班早走（哺乳假相关）
	LeaveFeedingRestDaily  int64 `json:"leave_feeding_rest_daily,omitempty"`  //每日休息（哺乳假相关）

	LeaveName      string                           `json:"leave_name,omitempty"`       // 假期名称
	LeaveUnit      string                           `json:"leave_unit,omitempty"`       // 请假最小时长
	LeaveStartTime string                           `json:"leave_start_time,omitempty"` // 请假开始时间
	LeaveEndTime   string                           `json:"leave_end_time,omitempty"`   // 请假结束时间
	LeaveDetail    []string                         `json:"leave_detail,omitempty"`     // 具体的请假明细时间
	LeaveRange     []string                         `json:"leave_range,omitempty"`      // 具体的请假时间范围
	LeaveInterval  int64                            `json:"leave_interval,omitempty"`   // 请假时长，单位（秒）
	LeaveReason    string                           `json:"leave_reason,omitempty"`     // 请假事由
	I18nResources  []*P1LeaveApprovalI18nResourceV4 `json:"i18n_resources,omitempty"`   // 国际化文案
}

type P1LeaveApprovalI18nResourceV4 struct {
	Locale    string            `json:"locale,omitempty"`     // 如: en_us
	IsDefault bool              `json:"is_default,omitempty"` // 如: true
	Texts     map[string]string `json:"texts,omitempty"`
}

type P1WorkApprovalV4 struct {
	*larkevent.EventReq
	*larkevent.EventBase
	Event *P1WorkApprovalV4Data `json:"event"`
}

func (m *P1WorkApprovalV4) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1WorkApprovalV4Data struct {
	AppID         string `json:"app_id,omitempty"`          // APP ID
	TenantKey     string `json:"tenant_key,omitempty"`      // 企业标识
	Type          string `json:"type,omitempty"`            //事件回调此处固定为event_callback
	InstanceCode  string `json:"instance_code,omitempty"`   // 审批实例Code
	EmployeeID    string `json:"employee_id,omitempty"`     // 用户id
	OpenID        string `json:"open_id,omitempty"`         // 用户open_id
	StartTime     int64  `json:"start_time,omitempty"`      // 审批发起时间
	EndTime       int64  `json:"end_time,omitempty"`        // 审批结束时间
	WorkType      string `json:"work_type,omitempty"`       // 加班类型
	WorkStartTime string `json:"work_start_time,omitempty"` // 加班开始时间
	WorkEndTime   string `json:"work_end_time,omitempty"`   // 加班结束时间
	WorkInterval  int64  `json:"work_interval,omitempty"`   // 加班时长，单位（秒）
	WorkReason    string `json:"work_reason,omitempty"`     // 加班事由
}

type P1ShiftApprovalV4 struct {
	*larkevent.EventReq
	*larkevent.EventBase
	Event *P1ShiftApprovalV4Data `json:"event"`
}

func (m *P1ShiftApprovalV4) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1ShiftApprovalV4Data struct {
	AppID        string `json:"app_id,omitempty"`        // APP ID
	TenantKey    string `json:"tenant_key,omitempty"`    // 企业标识
	Type         string `json:"type,omitempty"`          //事件回调此处固定为event_callback
	InstanceCode string `json:"instance_code,omitempty"` // 审批实例Code
	EmployeeID   string `json:"employee_id,omitempty"`   // 用户id
	OpenID       string `json:"open_id,omitempty"`       // 用户open_id
	StartTime    int64  `json:"start_time,omitempty"`    // 审批发起时间
	EndTime      int64  `json:"end_time,omitempty"`      // 审批结束时间
	ShiftTime    string `json:"shift_time,omitempty"`    // 换班时间
	ReturnTime   string `json:"return_time,omitempty"`   // 还班时间
	ShiftReason  string `json:"shift_reason,omitempty"`  // 换班事由
}

type P1RemedyApprovalV4 struct {
	*larkevent.EventReq
	*larkevent.EventBase
	Event *P1RemedyApprovalV4Data `json:"event"`
}

func (m *P1RemedyApprovalV4) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1RemedyApprovalV4Data struct {
	AppID        string `json:"app_id,omitempty"`        // APP ID
	TenantKey    string `json:"tenant_key,omitempty"`    // 企业标识
	Type         string `json:"type,omitempty"`          //事件回调此处固定为event_callback
	InstanceCode string `json:"instance_code,omitempty"` // 审批实例Code
	EmployeeID   string `json:"employee_id,omitempty"`   // 用户id
	OpenID       string `json:"open_id,omitempty"`       // 用户open_id
	StartTime    int64  `json:"start_time,omitempty"`    // 审批发起时间
	EndTime      int64  `json:"end_time,omitempty"`      // 审批结束时间
	RemedyTime   string `json:"remedy_time,omitempty"`   // 补卡时间
	RemedyReason string `json:"remedy_reason,omitempty"` // 补卡原因
}

type P1TripApprovalV4 struct {
	*larkevent.EventReq
	*larkevent.EventBase
	Event *P1TripApprovalV4Data `json:"event"`
}

func (m *P1TripApprovalV4) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1TripApprovalV4Data struct {
	AppID        string                      `json:"app_id,omitempty"`        // APP ID
	TenantKey    string                      `json:"tenant_key,omitempty"`    // 企业标识
	Type         string                      `json:"type,omitempty"`          //事件回调此处固定为event_callback
	InstanceCode string                      `json:"instance_code,omitempty"` // 审批实例Code
	EmployeeID   string                      `json:"employee_id,omitempty"`   // 用户id
	OpenID       string                      `json:"open_id,omitempty"`       // 用户open_id
	StartTime    int64                       `json:"start_time,omitempty"`    // 审批发起时间
	EndTime      int64                       `json:"end_time,omitempty"`      // 审批结束时间
	Schedules    []*P1TripApprovalScheduleV4 `json:"schedules,omitempty"`     // Schedule 结构数组
	TripInterval int64                       `json:"trip_interval,omitempty"` // 行程总时长（秒）
	TripReason   string                      `json:"trip_reason,omitempty"`   // 出差事由
	TripPeers    []string                    `json:"trip_peers,omitempty"`    // 同行人
}

type P1TripApprovalScheduleV4 struct {
	TripStartTime  string `json:"trip_start_time,omitempty"` // 行程开始时间
	TripEndTime    string `json:"trip_end_time,omitempty"`   // 行程结束时间
	TripInterval   int64  `json:"trip_interval,omitempty"`   // 行程时长（秒）
	Departure      string `json:"departure,omitempty"`       // 出发地
	Destination    string `json:"destination,omitempty"`     // 目的地
	Transportation string `json:"transportation,omitempty"`  // 交通工具
	TripType       string `json:"trip_type,omitempty"`       // 单程/往返
	Remark         string `json:"remark,omitempty"`          // 备注
}

type P1TripApprovalTripPeerV4 struct {
	string `json:",omitempty"`
}

type P1OutApprovalV4 struct {
	*larkevent.EventReq
	*larkevent.EventBase
	Event *P1OutApprovalV4Data `json:"event"`
}

func (m *P1OutApprovalV4) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1OutApprovalV4Data struct {
	AppID         string                         `json:"app_id,omitempty"`         // APP ID
	I18nResources []*P1OutApprovalI18nResourceV4 `json:"i18n_resources,omitempty"` // 国际化文案
	InstanceCode  string                         `json:"instance_code,omitempty"`  // 此审批的唯一标识
	OutImage      string                         `json:"out_image,omitempty"`
	OutInterval   int64                          `json:"out_interval,omitempty"`   // 外出时长，单位秒
	OutName       string                         `json:"out_name,omitempty"`       // 通过i18n_resources里的信息换取相应语言的文案
	OutReason     string                         `json:"out_reason,omitempty"`     // 外出事由
	OutStartTime  string                         `json:"out_start_time,omitempty"` // 外出开始时间
	OutEndTime    string                         `json:"out_end_time,omitempty"`   // 外出结束时间
	OutUnit       string                         `json:"out_unit,omitempty"`       // 外出时长的单位，HOUR 小时，DAY 天，HALF_DAY 半天
	StartTime     int64                          `json:"start_time,omitempty"`     // 审批开始时间
	EndTime       int64                          `json:"end_time,omitempty"`       // 审批结束时间
	TenantKey     string                         `json:"tenant_key,omitempty"`     // 企业标识
	Type          string                         `json:"type,omitempty"`           // 此事件此处始终为event_callback
	OpenID        string                         `json:"open_id,omitempty"`        // 申请发起人open_id
	UserID        string                         `json:"user_id,omitempty"`        // 申请发起人
}

type P1OutApprovalI18nResourceV4 struct {
	IsDefault bool              `json:"is_default,omitempty"`
	Locale    string            `json:"locale,omitempty"`
	Texts     map[string]string `json:"texts,omitempty"` // key对应的文案
}
