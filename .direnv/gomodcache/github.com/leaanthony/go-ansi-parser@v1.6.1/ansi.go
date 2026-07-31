package ansi

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rivo/uniseg"
)

// TextStyle is a type representing the
// ansi text styles
type TextStyle int

const (
	// Bold Style
	Bold TextStyle = 1 << 0
	// Faint Style
	Faint TextStyle = 1 << 1
	// Italic Style
	Italic TextStyle = 1 << 2
	// Blinking Style
	Blinking TextStyle = 1 << 3
	// Inversed Style
	Inversed TextStyle = 1 << 4
	// Invisible Style
	Invisible TextStyle = 1 << 5
	// Underlined Style
	Underlined TextStyle = 1 << 6
	// Strikethrough Style
	Strikethrough TextStyle = 1 << 7
	// Bright Style
	Bright TextStyle = 1 << 8
)

type ColourMode int

const (
	Default    ColourMode = 0
	TwoFiveSix ColourMode = 1
	TrueColour ColourMode = 2
)

var invalid = fmt.Errorf("invalid ansi string")
var missingTerminator = fmt.Errorf("missing escape terminator 'm'")
var invalidTrueColorSequence = fmt.Errorf("invalid TrueColor sequence")
var invalid256ColSequence = fmt.Errorf("invalid 256 colour sequence")

const (
	// Default colors uses foreground color codes [30-37].
	// See ColourMap and case for background colors.
	defaultForegroundColor = "37"
	defaultBackgroundColor = "30"
)

// StyledText represents a single formatted string
type StyledText struct {
	Label      string
	FgCol      *Col
	BgCol      *Col
	Style      TextStyle
	ColourMode ColourMode
	// Offset is the offset into the input string where the StyledText begins
	Offset int
	// Len is the length in bytes of the substring of the input text that
	// contains the styled text
	Len int
}

func (s *StyledText) styleToParams() []string {
	var params []string
	if s.Bold() {
		params = append(params, "1")
	}
	if s.Faint() {
		params = append(params, "2")
	}
	if s.Italic() {
		params = append(params, "3")
	}
	if s.Underlined() {
		params = append(params, "4")
	}
	if s.Blinking() {
		params = append(params, "5")
	}
	if s.Inversed() {
		params = append(params, "7")
	}
	if s.Invisible() {
		params = append(params, "8")
	}
	if s.Strikethrough() {
		params = append(params, "9")
	}
	if s.FgCol != nil {
		// Do we have an ID?
		switch s.ColourMode {
		case Default:
			offset := 30
			id := s.FgCol.Id
			// Adjust when bold has been applied to the id
			if (s.Bold() || s.Bright()) && id > 7 && id < 16 {
				id -= 8
			}
			if s.Bright() {
				offset = 90
			}
			params = append(params, fmt.Sprintf("%d", id+offset))
		case TwoFiveSix:
			params = append(params, []string{"38", "5", fmt.Sprintf("%d", s.FgCol.Id)}...)
		case TrueColour:
			r := fmt.Sprintf("%d", s.FgCol.Rgb.R)
			g := fmt.Sprintf("%d", s.FgCol.Rgb.G)
			b := fmt.Sprintf("%d", s.FgCol.Rgb.B)
			params = append(params, []string{"38", "2", r, g, b}...)
		}
	}
	if s.BgCol != nil {
		// Do we have an ID?
		switch s.ColourMode {
		case Default:
			id := s.BgCol.Id
			offset := 40
			if s.Bright() {
				offset = 100
			}
			// Adjust when bold has been applied to the id
			if (s.Bold() || s.Bright()) && id > 7 && id < 16 {
				id -= 8
			}
			params = append(params, fmt.Sprintf("%d", id+offset))
		case TwoFiveSix:
			params = append(params, []string{"48", "5", fmt.Sprintf("%d", s.BgCol.Id)}...)
		case TrueColour:
			r := fmt.Sprintf("%d", s.BgCol.Rgb.R)
			g := fmt.Sprintf("%d", s.BgCol.Rgb.G)
			b := fmt.Sprintf("%d", s.BgCol.Rgb.B)
			params = append(params, []string{"48", "2", r, g, b}...)
		}
	}
	return params
}

func (s *StyledText) String() string {
	params := strings.Join(s.styleToParams(), ";")
	return "\033[0;" + params + "m" + s.Label + "\033[0m"
}

// Bold will return true if the text has a Bold style
func (s *StyledText) Bold() bool {
	return s.Style&Bold == Bold
}

// Faint will return true if the text has a Faint style
func (s *StyledText) Faint() bool {
	return s.Style&Faint == Faint
}

// Italic will return true if the text has an Italic style
func (s *StyledText) Italic() bool {
	return s.Style&Italic == Italic
}

// Blinking will return true if the text has a Blinking style
func (s *StyledText) Blinking() bool {
	return s.Style&Blinking == Blinking
}

// Inversed will return true if the text has an Inversed style
func (s *StyledText) Inversed() bool {
	return s.Style&Inversed == Inversed
}

// Invisible will return true if the text has an Invisible style
func (s *StyledText) Invisible() bool {
	return s.Style&Invisible == Invisible
}

// Underlined will return true if the text has an Underlined style
func (s *StyledText) Underlined() bool {
	return s.Style&Underlined == Underlined
}

// Strikethrough will return true if the text has a Strikethrough style
func (s *StyledText) Strikethrough() bool {
	return s.Style&Strikethrough == Strikethrough
}

// Bright will return true if the text has a Bright style
func (s *StyledText) Bright() bool {
	return s.Style&Bright == Bright
}

// ColourMap maps ansi identifiers to a colour
var ColourMap = map[string]map[string]*Col{
	"Regular": {
		"30":  Cols[0],
		"31":  Cols[1],
		"32":  Cols[2],
		"33":  Cols[3],
		"34":  Cols[4],
		"35":  Cols[5],
		"36":  Cols[6],
		"37":  Cols[7],
		"90":  Cols[8],
		"91":  Cols[9],
		"92":  Cols[10],
		"93":  Cols[11],
		"94":  Cols[12],
		"95":  Cols[13],
		"96":  Cols[14],
		"97":  Cols[15],
		"100": Cols[8],
		"101": Cols[9],
		"102": Cols[10],
		"103": Cols[11],
		"104": Cols[12],
		"105": Cols[13],
		"106": Cols[14],
		"107": Cols[15],
	},
	"Bold": {
		"30":  Cols[8],
		"31":  Cols[9],
		"32":  Cols[10],
		"33":  Cols[11],
		"34":  Cols[12],
		"35":  Cols[13],
		"36":  Cols[14],
		"37":  Cols[15],
		"90":  Cols[8],
		"91":  Cols[9],
		"92":  Cols[10],
		"93":  Cols[11],
		"94":  Cols[12],
		"95":  Cols[13],
		"96":  Cols[14],
		"97":  Cols[15],
		"100": Cols[8],
		"101": Cols[9],
		"102": Cols[10],
		"103": Cols[11],
		"104": Cols[12],
		"105": Cols[13],
		"106": Cols[14],
		"107": Cols[15],
	},
	"Faint": {
		"30": Cols[0],
		"31": Cols[1],
		"32": Cols[2],
		"33": Cols[3],
		"34": Cols[4],
		"35": Cols[5],
		"36": Cols[6],
		"37": Cols[7],
	},
}

// Parse will convert an ansi encoded string and return
// a slice of StyledText structs that represent the text.
// If parsing is unsuccessful, an error is returned.
func Parse(input string, options ...ParseOption) ([]*StyledText, error) {
	var result []*StyledText
	index := 0
	offset := 0
	escapeCodeLen := 0
	var currentStyledText = &StyledText{}

	if len(input) == 0 {
		return []*StyledText{currentStyledText}, nil
	}

	for {
		// Read all chars to next escape code
		esc := strings.Index(input, "\033[")

		// If no more esc chars, save what's left and return
		if esc == -1 {
			text := input[index:]
			if len(text) > 0 {
				currentStyledText.Label = text
				currentStyledText.Offset = offset
				currentStyledText.Len = len(text) + escapeCodeLen
				result = append(result, currentStyledText)
			}
			return result, nil
		}
		label := input[:esc]
		if len(label) > 0 {
			currentStyledText.Label = label
			currentStyledText.Offset = offset
			currentStyledText.Len = len(label) + escapeCodeLen
			offset += currentStyledText.Len
			result = append(result, currentStyledText)
			currentStyledText = &StyledText{
				Label: "",
				FgCol: currentStyledText.FgCol,
				BgCol: currentStyledText.BgCol,
				Style: currentStyledText.Style,
			}
			escapeCodeLen = 0
		}
		input = input[esc:]
		// skip
		input = input[2:]

		// Read in params
		endesc := strings.Index(input, "m")
		if endesc == -1 {
			return nil, missingTerminator
		}
		paramText := input[:endesc]
		input = input[endesc+1:]
		escapeCodeLen += 2 + endesc + 1
		params := strings.Split(paramText, ";")
		colourMap := ColourMap["Regular"]
		skip := 0
		for index, param := range params {
			if skip > 0 {
				skip--
				continue
			}
			param = stripLeadingZeros(param)
			switch param {
			case "0", "":
				colourMap = ColourMap["Regular"]
				currentStyledText.Style = 0
				currentStyledText.FgCol = nil
				currentStyledText.BgCol = nil
			case "1":
				// Bold
				colourMap = ColourMap["Bold"]
				currentStyledText.Style |= Bold
			case "2":
				// Dim/Feint
				colourMap = ColourMap["Faint"]
				currentStyledText.Style |= Faint
			case "3":
				// Italic
				currentStyledText.Style |= Italic
			case "4":
				// Underlined
				currentStyledText.Style |= Underlined
			case "5":
				// Blinking
				currentStyledText.Style |= Blinking
			case "7":
				// Inversed
				currentStyledText.Style |= Inversed
			case "8":
				// Invisible
				currentStyledText.Style |= Invisible
			case "9":
				// Strikethrough
				currentStyledText.Style |= Strikethrough
			case "30", "31", "32", "33", "34", "35", "36", "37":
				currentStyledText.FgCol = colourMap[param]
			case "90", "91", "92", "93", "94", "95", "96", "97":
				currentStyledText.FgCol = colourMap[param]
				currentStyledText.Style |= Bright
			case "100", "101", "102", "103", "104", "105", "106", "107":
				currentStyledText.BgCol = colourMap[param]
				currentStyledText.Style |= Bright
			case "40", "41", "42", "43", "44", "45", "46", "47":
				bgcol := "3" + param[1:] // Equivalent of -10
				currentStyledText.BgCol = colourMap[bgcol]
			case "38", "48":
				if len(params)-index < 3 {
					return nil, invalid
				}
				// 256 colours
				param1 := stripLeadingZeros(params[index+1])
				if param1 == "5" {
					skip = 2
					colIndexText := stripLeadingZeros(params[index+2])
					colIndex, err := strconv.Atoi(colIndexText)
					if err != nil {
						return nil, invalid256ColSequence
					}
					if colIndex < 0 || colIndex > 255 {
						return nil, invalid256ColSequence
					}
					currentStyledText.ColourMode = TwoFiveSix
					if param == "38" {
						currentStyledText.FgCol = Cols[colIndex]
						continue
					}
					currentStyledText.BgCol = Cols[colIndex]
					continue
				}
				// we must have 4 params left
				if len(params)-index < 5 {
					return nil, invalidTrueColorSequence
				}
				if param1 != "2" {
					return nil, invalidTrueColorSequence
				}
				var r, g, b uint8
				ri, err := strconv.Atoi(params[index+2])
				if err != nil {
					return nil, invalidTrueColorSequence
				}
				gi, err := strconv.Atoi(params[index+3])
				if err != nil {
					return nil, invalidTrueColorSequence
				}
				bi, err := strconv.Atoi(params[index+4])
				if err != nil {
					return nil, invalidTrueColorSequence
				}
				if bi > 255 || gi > 255 || ri > 255 {
					return nil, invalidTrueColorSequence
				}
				if bi < 0 || gi < 0 || ri < 0 {
					return nil, invalidTrueColorSequence
				}
				r = uint8(ri)
				g = uint8(gi)
				b = uint8(bi)
				skip = 4
				colvalue := fmt.Sprintf("#%02x%02x%02x", r, g, b)
				currentStyledText.ColourMode = TrueColour
				if param == "38" {
					currentStyledText.FgCol = &Col{Id: 256, Hex: colvalue, Rgb: Rgb{r, g, b}}
					continue
				}
				currentStyledText.BgCol = &Col{Id: 256, Hex: colvalue, Rgb: Rgb{r, g, b}}
			case "39":
				// Lookup for default foreground color.
				foregroundColor := colourMap[defaultForegroundColor]
				for _, option := range options {
					if option.ansiForegroundColor != "" {
						foregroundColor = colourMap[option.ansiForegroundColor]
						break
					}
				}

				// Set selected foreground color.
				currentStyledText.FgCol = foregroundColor
			case "49":
				// Lookup for default background color.
				backgroundColor := colourMap[defaultBackgroundColor]
				for _, option := range options {
					if option.ansiBackgroundColor != "" {
						backgroundColor = colourMap[option.ansiBackgroundColor]
						break
					}
				}

				// Set selected background color.
				currentStyledText.BgCol = backgroundColor
			default:
				// Unexpected codes may be ignored.
				unexpectedCodeIgnored := false
				for _, option := range options {
					if option.ignoreUnexpectedCode {
						unexpectedCodeIgnored = true
						break
					}
				}
				if !unexpectedCodeIgnored {
					return nil, invalid
				}
			}
		}
	}
}

func stripLeadingZeros(s string) string {
	if len(s) < 2 {
		return s
	}
	return strings.TrimLeft(s, "0")
}

// HasEscapeCodes tests that input has escape codes.
func HasEscapeCodes(input string) bool {
	return strings.IndexAny(input, "\033[") != -1
}

// String builds an ANSI string for specified StyledText slice.
func String(input []*StyledText) string {
	var result strings.Builder
	for _, text := range input {
		params := text.styleToParams()
		if len(params) == 0 {
			result.WriteString(text.Label)
			continue
		}
		result.WriteString(text.String())
	}
	return result.String()
}

// Truncate truncates text to length but preserves control symbols in ANSI string.
func Truncate(input string, maxChars int, options ...ParseOption) (string, error) {
	parsed, err := Parse(input, options...)
	if err != nil {
		return "", err
	}
	charsLeft := maxChars
	var result []*StyledText
	for _, element := range parsed {
		userPerceivedChars := uniseg.GraphemeClusterCount(element.Label)
		if userPerceivedChars >= charsLeft {
			var newLabel []rune
			graphemes := uniseg.NewGraphemes(element.Label)
			for graphemes.Next() {
				newLabel = append(newLabel, graphemes.Runes()...)
				charsLeft--
				if charsLeft == 0 {
					element.Label = string(newLabel)
					result = append(result, element)
					return String(result), nil
				}
			}
		}
		result = append(result, element)
		charsLeft -= userPerceivedChars
	}
	return String(result), nil
}

// Cleanse removes ANSI control symbols from the string.
func Cleanse(input string, options ...ParseOption) (string, error) {
	if input == "" {
		return "", nil
	}
	parsed, err := Parse(input, options...)
	if err != nil {
		return "", err
	}
	var result strings.Builder
	for _, element := range parsed {
		result.WriteString(element.Label)
	}
	return result.String(), nil
}

// Length calculates count of user-perceived characters in ANSI string.
func Length(input string, options ...ParseOption) (int, error) {
	if input == "" {
		return 0, nil
	}
	parsed, err := Parse(input, options...)
	if err != nil {
		return -1, err
	}
	var result int
	for _, element := range parsed {
		userPerceivedChars := uniseg.GraphemeClusterCount(element.Label)
		result += userPerceivedChars
	}
	return result, nil
}
