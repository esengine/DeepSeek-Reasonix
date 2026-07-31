package normalize

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

type postContent struct {
	ZhCn postLanguage `json:"zh_cn"`
}

type postLanguage struct {
	Title   string          `json:"title,omitempty"`
	Content [][]postElement `json:"content"`
}

type postElement struct {
	Tag      string `json:"tag"`
	Text     string `json:"text,omitempty"`
	Href     string `json:"href,omitempty"`
	ImageKey string `json:"image_key,omitempty"`
	UserID   string `json:"user_id,omitempty"`
	UserName string `json:"user_name,omitempty"`
}

// SimpleMarkdownToPost converts a basic markdown string to Lark Post JSON string.
// It supports paragraphs and links in the format [text](url).
func SimpleMarkdownToPost(title, markdown string, mentions []types.Mention) (string, error) {
	lines := strings.Split(markdown, "\n")
	content := make([][]postElement, 0, len(lines))

	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	atRegex := regexp.MustCompile(`<at user_id="([^"]+)">(.*?)</at>`)

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")

		var paragraph []postElement

		// Find all matches for both links and at tags
		linkMatches := linkRegex.FindAllStringSubmatchIndex(line, -1)
		atMatches := atRegex.FindAllStringSubmatchIndex(line, -1)

		// Combine and sort matches by starting index
		type matchInfo struct {
			start, end int
			mType      string // "link" or "at"
			indices    []int
		}
		var allMatches []matchInfo
		for _, m := range linkMatches {
			allMatches = append(allMatches, matchInfo{m[0], m[1], "link", m})
		}
		for _, m := range atMatches {
			allMatches = append(allMatches, matchInfo{m[0], m[1], "at", m})
		}

		// Sort matches by start index
		for i := 0; i < len(allMatches); i++ {
			for j := i + 1; j < len(allMatches); j++ {
				if allMatches[i].start > allMatches[j].start {
					allMatches[i], allMatches[j] = allMatches[j], allMatches[i]
				}
			}
		}

		lastIndex := 0
		for _, match := range allMatches {
			start, end := match.start, match.end

			// Prevent overlapping matches
			if start < lastIndex {
				continue
			}

			if start > lastIndex {
				paragraph = append(paragraph, postElement{
					Tag:  "text",
					Text: line[lastIndex:start],
				})
			}

			if match.mType == "link" {
				textStart, textEnd := match.indices[2], match.indices[3]
				hrefStart, hrefEnd := match.indices[4], match.indices[5]
				paragraph = append(paragraph, postElement{
					Tag:  "a",
					Text: line[textStart:textEnd],
					Href: line[hrefStart:hrefEnd],
				})
			} else if match.mType == "at" {
				userIDStart, userIDEnd := match.indices[2], match.indices[3]
				userNameStart, userNameEnd := match.indices[4], match.indices[5]
				paragraph = append(paragraph, postElement{
					Tag:      "at",
					UserID:   line[userIDStart:userIDEnd],
					UserName: line[userNameStart:userNameEnd],
				})
			}

			lastIndex = end
		}

		if lastIndex < len(line) {
			paragraph = append(paragraph, postElement{
				Tag:  "text",
				Text: line[lastIndex:],
			})
		}

		// Keep empty lines as empty text elements to maintain paragraph spacing
		if len(paragraph) == 0 {
			paragraph = append(paragraph, postElement{
				Tag:  "text",
				Text: " ", // Use space instead of empty string to bypass omitempty and Feishu empty check
			})
		}

		content = append(content, paragraph)
	}

	// Prepend mentions
	if len(mentions) > 0 {
		atElements := ComposePostMentionElements(mentions)
		if len(atElements) > 0 {
			var first []postElement
			for _, el := range atElements {
				first = append(first, el, postElement{Tag: "text", Text: " "})
			}
			content = append([][]postElement{first}, content...)
		}
	}

	post := postContent{
		ZhCn: postLanguage{
			Title:   title,
			Content: content,
		},
	}

	bytes, err := json.Marshal(post)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
