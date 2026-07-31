module github.com/larksuite/oapi-sdk-go/v3

go 1.13

//
//replace (
//	github.com/larksuite/oapi-sdk-go/v3/core => ./core
//	github.com/larksuite/oapi-sdk-go/v3/event => ./event
//	github.com/larksuite/oapi-sdk-go/v3/service/acs => ./service/acs
//	github.com/larksuite/oapi-sdk-go/v3/service/admin => ./service/admin
//	github.com/larksuite/oapi-sdk-go/v3/service/application => ./service/application
//	github.com/larksuite/oapi-sdk-go/v3/service/approval => ./service/approval
//	github.com/larksuite/oapi-sdk-go/v3/service/attendance => ./service/attendance
//	github.com/larksuite/oapi-sdk-go/v3/service/baike => ./service/baike
//	github.com/larksuite/oapi-sdk-go/v3/service/bitable => ./service/bitable
//	github.com/larksuite/oapi-sdk-go/v3/service/calendar => ./service/calendar
//	github.com/larksuite/oapi-sdk-go/v3/service/contact => ./service/contact
//	github.com/larksuite/oapi-sdk-go/v3/service/docx => ./service/docx
//	github.com/larksuite/oapi-sdk-go/v3/service/drive => ./service/drive
//	github.com/larksuite/oapi-sdk-go/v3/service/ehr => ./service/ehr
//	github.com/larksuite/oapi-sdk-go/v3/service/event => ./service/event
//	github.com/larksuite/oapi-sdk-go/v3/service/human_authentication => ./service/human_authentication
//	github.com/larksuite/oapi-sdk-go/v3/service/im => ./service/im
//	github.com/larksuite/oapi-sdk-go/v3/service/mail => ./service/mail
//	github.com/larksuite/oapi-sdk-go/v3/service/meeting_room => ./service/meeting_room
//	github.com/larksuite/oapi-sdk-go/v3/service/optical_char_recognition => ./service/optical_char_recognition
//	github.com/larksuite/oapi-sdk-go/v3/service/passport => ./service/passport
//	github.com/larksuite/oapi-sdk-go/v3/service/search => ./service/search
//	github.com/larksuite/oapi-sdk-go/v3/service/sheets => ./service/sheets
//	github.com/larksuite/oapi-sdk-go/v3/service/speech_to_text => ./service/speech_to_text
//	github.com/larksuite/oapi-sdk-go/v3/service/task => ./service/task
//	github.com/larksuite/oapi-sdk-go/v3/service/tenant => ./service/tenant
//	github.com/larksuite/oapi-sdk-go/v3/service/translation => ./service/translation
//	github.com/larksuite/oapi-sdk-go/v3/service/vc => ./service/vc
//	github.com/larksuite/oapi-sdk-go/v3/service/wiki => ./service/wiki
//
//)

require (
	github.com/gogo/protobuf v1.3.2
	github.com/gorilla/websocket v1.5.0
)
