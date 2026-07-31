package normalize

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

// ParseContent parses the message content based on the message type.
func ParseContent(msgType string, content string) (string, []types.Resource) {
	// For merge_forward, the content from API might be just a raw string like "Merged and Forwarded Message", not a JSON.
	if msgType == "merge_forward" {
		return content, nil
	}

	var contentMap map[string]interface{}
	if err := json.Unmarshal([]byte(content), &contentMap); err != nil {
		return "[unsupported message]", nil
	}

	switch msgType {
	case "text":
		if text, ok := contentMap["text"].(string); ok {
			return text, nil
		}
		return "", nil

	case "image":
		if imageKey, ok := contentMap["image_key"].(string); ok {
			return fmt.Sprintf("![image](%s)", imageKey), []types.Resource{{Type: "image", FileKey: imageKey}}
		}
		return "[image]", nil

	case "file":
		fileKey, _ := contentMap["file_key"].(string)
		fileName, _ := contentMap["file_name"].(string)
		if fileKey == "" {
			return "[file]", nil
		}
		attr := ""
		if fileName != "" {
			attr = fmt.Sprintf(` name="%s"`, escapeAttr(fileName))
		}
		return fmt.Sprintf(`<file key="%s"%s/>`, fileKey, attr), []types.Resource{{Type: "file", FileKey: fileKey, FileName: fileName}}

	case "folder":
		fileKey, _ := contentMap["file_key"].(string)
		fileName, _ := contentMap["file_name"].(string)
		if fileKey == "" {
			return "[folder]", nil
		}
		attr := ""
		if fileName != "" {
			attr = fmt.Sprintf(` name="%s"`, escapeAttr(fileName))
		}
		return fmt.Sprintf(`<folder key="%s"%s/>`, fileKey, attr), nil

	case "audio":
		fileKey, _ := contentMap["file_key"].(string)
		if fileKey == "" {
			return "[audio]", nil
		}
		res := types.Resource{Type: "audio", FileKey: fileKey}
		attr := ""
		if durFloat, ok := contentMap["duration"].(float64); ok {
			durMs := int(durFloat)
			res.DurationMs = &durMs
			if durStr := formatDuration(durMs); durStr != "" {
				attr = fmt.Sprintf(` duration="%s"`, durStr)
			}
		}
		return fmt.Sprintf(`<audio key="%s"%s/>`, fileKey, attr), []types.Resource{res}

	case "media", "video":
		fileKey, _ := contentMap["file_key"].(string)
		fileName, _ := contentMap["file_name"].(string)
		imageKey, _ := contentMap["image_key"].(string) // cover
		if fileKey == "" {
			return "[video]", nil
		}
		res := types.Resource{Type: "video", FileKey: fileKey, FileName: fileName, CoverImageKey: imageKey}
		attr := ""
		if fileName != "" {
			attr += fmt.Sprintf(` name="%s"`, escapeAttr(fileName))
		}
		if durFloat, ok := contentMap["duration"].(float64); ok {
			durMs := int(durFloat)
			res.DurationMs = &durMs
			if durStr := formatDuration(durMs); durStr != "" {
				attr += fmt.Sprintf(` duration="%s"`, durStr)
			}
		}
		return fmt.Sprintf(`<video key="%s"%s/>`, fileKey, attr), []types.Resource{res}

	case "sticker":
		fileKey, _ := contentMap["file_key"].(string)
		if fileKey == "" {
			return "[sticker]", nil
		}
		return fmt.Sprintf(`<sticker key="%s"/>`, fileKey), []types.Resource{{Type: "sticker", FileKey: fileKey}}

	case "hongbao":
		text, _ := contentMap["text"].(string)
		attr := ""
		if text != "" {
			attr = fmt.Sprintf(` text="%s"`, escapeAttr(text))
		}
		return fmt.Sprintf(`<hongbao%s/>`, attr), nil

	case "location":
		name, _ := contentMap["name"].(string)
		lat, _ := contentMap["latitude"].(string)
		lng, _ := contentMap["longitude"].(string)
		attr := ""
		if name != "" {
			attr += fmt.Sprintf(` name="%s"`, escapeAttr(name))
		}
		if lat != "" && lng != "" {
			attr += fmt.Sprintf(` coords="lat:%s,lng:%s"`, lat, lng)
		}
		return fmt.Sprintf(`<location%s/>`, attr), nil

	case "share_chat":
		chatID, _ := contentMap["chat_id"].(string)
		return fmt.Sprintf(`<group_card id="%s"/>`, chatID), nil

	case "share_user":
		userID, _ := contentMap["user_id"].(string)
		return fmt.Sprintf(`<contact_card id="%s"/>`, userID), nil

	case "system":
		template, ok := contentMap["template"].(string)
		if !ok || template == "" {
			return "[system message]", nil
		}
		// In a real implementation, we should replace {key} with values from contentMap.
		// A simple regex or string replacement approach can be used.
		out := template
		for k, v := range contentMap {
			if k == "template" {
				continue
			}
			placeholder := fmt.Sprintf("{%s}", k)
			if !strings.Contains(out, placeholder) {
				continue
			}
			var strVal string
			switch val := v.(type) {
			case string:
				strVal = val
			case []interface{}:
				var strVals []string
				for _, item := range val {
					strVals = append(strVals, fmt.Sprintf("%v", item))
				}
				strVal = strings.Join(strVals, ", ")
			default:
				strVal = fmt.Sprintf("%v", val)
			}
			out = strings.ReplaceAll(out, placeholder, strVal)
		}
		out = strings.TrimSpace(out)
		if out == "" {
			out = "[system message]"
		}
		return out, nil

	case "vote":
		topic, _ := contentMap["topic"].(string)
		optionsInterface, _ := contentMap["options"].([]interface{})
		var options []string
		for _, opt := range optionsInterface {
			if s, ok := opt.(string); ok {
				options = append(options, s)
			}
		}
		if topic == "" && len(options) == 0 {
			return "<vote>\n[vote]\n</vote>", nil
		}
		lines := []string{}
		if topic != "" {
			lines = append(lines, topic)
		}
		for _, opt := range options {
			lines = append(lines, "• "+opt)
		}
		return fmt.Sprintf("<vote>\n%s\n</vote>", strings.Join(lines, "\n")), nil

	case "video_chat":
		topic, _ := contentMap["topic"].(string)
		startTimeStr, _ := contentMap["start_time"].(string)
		if startTimeStr == "" {
			if startFloat, ok := contentMap["start_time"].(float64); ok {
				startTimeStr = fmt.Sprintf("%d", int64(startFloat))
			}
		}
		lines := []string{}
		if topic != "" {
			lines = append(lines, "📹 "+topic)
		}
		if dt := millisToDatetime(startTimeStr); dt != "" {
			lines = append(lines, "🕙 "+dt)
		}
		inner := "[video chat]"
		if len(lines) > 0 {
			inner = strings.Join(lines, "\n")
		}
		return fmt.Sprintf("<meeting>\n%s\n</meeting>", inner), nil

	case "calendar", "general_calendar", "share_calendar_event":
		summary, _ := contentMap["summary"].(string)
		startTimeStr, _ := contentMap["start_time"].(string)
		if startTimeStr == "" {
			if startFloat, ok := contentMap["start_time"].(float64); ok {
				startTimeStr = fmt.Sprintf("%d", int64(startFloat))
			}
		}
		endTimeStr, _ := contentMap["end_time"].(string)
		if endTimeStr == "" {
			if endFloat, ok := contentMap["end_time"].(float64); ok {
				endTimeStr = fmt.Sprintf("%d", int64(endFloat))
			}
		}

		lines := []string{}
		if summary != "" {
			lines = append(lines, "📅 "+summary)
		}
		start := millisToDatetime(startTimeStr)
		end := millisToDatetime(endTimeStr)
		if start != "" && end != "" {
			lines = append(lines, fmt.Sprintf("🕙 %s ~ %s", start, end))
		} else if start != "" {
			lines = append(lines, fmt.Sprintf("🕙 %s", start))
		}
		inner := "[calendar event]"
		if len(lines) > 0 {
			inner = strings.Join(lines, "\n")
		}

		tag := "calendar"
		if msgType == "calendar" {
			tag = "calendar_invite"
		} else if msgType == "share_calendar_event" {
			tag = "calendar_share"
		}
		return fmt.Sprintf("<%s>\n%s\n</%s>", tag, inner, tag), nil

	case "todo":
		summaryMap, ok := contentMap["summary"].(map[string]interface{})
		if !ok {
			return "<todo>\n[todo]\n</todo>", nil
		}
		lines := []string{}
		if title, _ := summaryMap["title"].(string); title != "" {
			lines = append(lines, title)
		}

		// Parse post content simply
		if contentList, ok := summaryMap["content"].([]interface{}); ok {
			bodyText := extractPostPlainText(contentList)
			if bodyText != "" {
				lines = append(lines, bodyText)
			}
		}

		dueTimeStr, _ := contentMap["due_time"].(string)
		if dueTimeStr == "" {
			if dueFloat, ok := contentMap["due_time"].(float64); ok {
				dueTimeStr = fmt.Sprintf("%d", int64(dueFloat))
			}
		}
		if due := millisToDatetime(dueTimeStr); due != "" {
			lines = append(lines, "Due: "+due)
		}

		if len(lines) == 0 {
			return "<todo>\n[todo]\n</todo>", nil
		}
		return fmt.Sprintf("<todo>\n%s\n</todo>", strings.Join(lines, "\n")), nil

	case "post":
		var contentStr string
		var resources []types.Resource

		// For post, the contentMap usually looks like:
		// { "zh_cn": { "title": "...", "content": [ [ {...} ] ] } }
		// Find the first value that is a map
		var bodyMap map[string]interface{}
		for _, v := range contentMap {
			if m, ok := v.(map[string]interface{}); ok {
				bodyMap = m
				break
			}
		}

		if bodyMap == nil {
			return "[rich text message]", nil
		}

		lines := []string{}
		if title, _ := bodyMap["title"].(string); title != "" {
			lines = append(lines, fmt.Sprintf("**%s**", title))
			lines = append(lines, "")
		}

		if contentList, ok := bodyMap["content"].([]interface{}); ok {
			for _, paragraphInterface := range contentList {
				paragraph, ok := paragraphInterface.([]interface{})
				if !ok {
					continue
				}
				var lineParts []string
				for _, elInterface := range paragraph {
					el, ok := elInterface.(map[string]interface{})
					if !ok {
						continue
					}
					tag, _ := el["tag"].(string)
					text, _ := el["text"].(string)

					switch tag {
					case "text":
						// Simplified style handling
						lineParts = append(lineParts, text)
					case "a":
						href, _ := el["href"].(string)
						label := text
						if label == "" {
							label = href
						}
						if href != "" {
							lineParts = append(lineParts, fmt.Sprintf("[%s](%s)", label, href))
						} else {
							lineParts = append(lineParts, label)
						}
					case "at":
						userID, _ := el["user_id"].(string)
						userName, _ := el["user_name"].(string)
						if userID == "all" || userID == "all_members" {
							lineParts = append(lineParts, "@all")
						} else if userName != "" {
							lineParts = append(lineParts, "@"+userName)
						} else {
							lineParts = append(lineParts, "@"+userID)
						}
					case "img":
						imageKey, _ := el["image_key"].(string)
						if imageKey != "" {
							resources = append(resources, types.Resource{Type: "image", FileKey: imageKey})
							lineParts = append(lineParts, fmt.Sprintf("![image](%s)", imageKey))
						}
					case "media":
						fileKey, _ := el["file_key"].(string)
						if fileKey != "" {
							resources = append(resources, types.Resource{Type: "file", FileKey: fileKey})
							lineParts = append(lineParts, fmt.Sprintf(`<file key="%s"/>`, fileKey))
						}
					case "code_block":
						lang, _ := el["language"].(string)
						lineParts = append(lineParts, fmt.Sprintf("\n```%s\n%s\n```\n", lang, text))
					case "hr":
						lineParts = append(lineParts, "\n---\n")
					default:
						lineParts = append(lineParts, text)
					}
				}
				lines = append(lines, strings.Join(lineParts, ""))
			}
		}

		contentStr = strings.Join(lines, "\n")
		contentStr = strings.TrimSpace(contentStr)
		if contentStr == "" {
			contentStr = "[rich text message]"
		}
		return contentStr, resources

	case "interactive":
		// Typically, the content string is the card JSON.
		// For interactive, we can just return the raw string or format it.
		// Assuming we just return "[interactive card]" if not handled specifically.
		// Wait, the prompt says "interactive/card".
		// We could just return the content as-is or "[interactive card]"
		return "[interactive card]", nil

	case "merge_forward":
		return "Merged and Forwarded Message", nil

	default:
		return "[unsupported message]", nil
	}
}

// Helper functions

func escapeAttr(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

func formatDuration(ms int) string {
	if ms <= 0 {
		return ""
	}
	sec := ms / 1000
	if sec < 60 {
		return fmt.Sprintf("0:%02d", sec)
	}
	min := sec / 60
	sec = sec % 60
	if min < 60 {
		return fmt.Sprintf("%d:%02d", min, sec)
	}
	hr := min / 60
	min = min % 60
	return fmt.Sprintf("%d:%02d:%02d", hr, min, sec)
}

func millisToDatetime(msStr string) string {
	var ms int64
	if msStr == "" {
		return ""
	}
	_, err := fmt.Sscanf(msStr, "%d", &ms)
	if err != nil || ms <= 0 {
		return ""
	}
	// Parse as time.RFC3339 style but return YYYY-MM-DD HH:MM:SS
	// In Go, time.Unix(sec, nsec)
	t := time.Unix(ms/1000, (ms%1000)*1000000)
	// We'll format to UTC+8 or just local time. For simplicity, we use local or specific format.
	// In JS it was Beijing time. Let's use UTC+8.
	loc := time.FixedZone("UTC+8", 8*3600)
	t = t.In(loc)
	return t.Format("2006-01-02 15:04:05")
}

func extractPostPlainText(blocks []interface{}) string {
	var lines []string
	for _, paragraphInterface := range blocks {
		paragraph, ok := paragraphInterface.([]interface{})
		if !ok {
			continue
		}
		var parts []string
		for _, elInterface := range paragraph {
			el, ok := elInterface.(map[string]interface{})
			if !ok {
				continue
			}
			tag, _ := el["tag"].(string)
			text, _ := el["text"].(string)
			if tag == "text" && text != "" {
				parts = append(parts, text)
			} else if tag == "a" && text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			lines = append(lines, strings.Join(parts, ""))
		}
	}
	return strings.Join(lines, "\n")
}
