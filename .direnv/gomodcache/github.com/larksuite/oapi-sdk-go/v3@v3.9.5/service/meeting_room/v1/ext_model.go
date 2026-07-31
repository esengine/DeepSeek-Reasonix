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

package larkmeeting_room

import "github.com/larksuite/oapi-sdk-go/v3/event"

type P1ThirdPartyMeetingRoomChangedV1 struct {
	*larkevent.EventReq
	*larkevent.EventBase
	Event *P1ThirdPartyMeetingRoomChangedV1Data `json:"event"`
}

func (m *P1ThirdPartyMeetingRoomChangedV1) RawReq(req *larkevent.EventReq) {
	m.EventReq = req
}

type P1EventTimeV1 struct {
	TimeStamp string `json:"time_stamp,omitempty"` // 时间
}

type P1MeetingRoomV1 struct {
	OpenId string `json:"open_id,omitempty"` // 员工对此应用的唯一标识，同一员工对不同应用的open_id不同
}

type P1OrganizerV1 struct {
	OpenId string `json:"open_id,omitempty"` // 员工对此应用的唯一标识，同一员工对不同应用的open_id不同
	UserId string `json:"user_id,omitempty"` // 用户在ISV下的唯一标识，申请了"获取用户user ID"权限后才会返回
}

type P1ThirdPartyMeetingRoomChangedV1Data struct {
	AppID        string             `json:"app_id,omitempty"`        // App ID
	TenantKey    string             `json:"tenant_key,omitempty"`    // 企业标识
	Type         string             `json:"type,omitempty"`          // 此事件此处始终为event_callback
	EventTime    string             `json:"event_time,omitempty"`    //事件发生时间
	Uid          string             `json:"uid,omitempty"`           // 日程的唯一标识
	OriginalTime int                `json:"original_time,omitempty"` // 重复日程的例外的唯一标识，如果不是重复的日程，此处为0
	Start        *P1EventTimeV1     `json:"start,omitempty"`         //日历的日程开始时间
	End          *P1EventTimeV1     `json:"end,omitempty"`           //日历的日程结束时间
	MeetingRoom  []*P1MeetingRoomV1 `json:"meeting_rooms,omitempty"` //日程关联的会议室
	Organizer    *P1OrganizerV1     `json:"organizer,omitempty"`     //日程的组织者
}
