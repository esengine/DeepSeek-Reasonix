package regexp2

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Process the file "testoutput1" from PCRE2 v10.21 (public domain)
var totalCount, failCount = 0, 0

func TestPcre_Basics(t *testing.T) {
	defer func() {
		if failCount > 0 {
			t.Logf("%v of %v patterns failed", failCount, totalCount)
		}
	}()
	// open our test patterns file and run through it
	// validating results as we go
	file, err := os.Open("testoutput1")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// the high level structure of the file:
	//		#comments - ignore only outside of the pattern
	//		pattern (could be multi-line, could be surrounded by "" or //) after the / there are the options some we understand, some we don't
	//		    test case
	//		 0: success case
	//		\= Expect no match (ignored)
	//		    another test case
	//		No Match
	//
	//		another pattern ...etc

	scanner := bufio.NewScanner(file)
	// main pattern loop
	for scanner.Scan() {
		// reading the file a line at a time
		line := scanner.Text()

		if trim := strings.TrimSpace(line); trim == "" || strings.HasPrefix(trim, "#") {
			// skip blanks and comments
			continue
		}

		patternStart := line[0]
		if patternStart != '/' && patternStart != '"' {
			// an error!  expected a pattern but we didn't understand what was in the file
			t.Fatalf("Unknown file format, expected line to start with '/' or '\"', line in: %v", line)
		}

		// start building our pattern, handling multi-line patterns
		pattern := line
		totalCount++

		// keep appending the lines to our pattern string until we
		// find our closing tag, don't allow the first char to match on the
		// line start, but subsequent lines could end on the first char
		allowFirst := false
		for !containsEnder(line, patternStart, allowFirst) {
			if !scanner.Scan() {
				// an error!  expected more pattern, but got eof
				t.Fatalf("Unknown file format, expected more pattern text, but got EOF, pattern so far: %v", pattern)
			}
			line = scanner.Text()
			pattern += fmt.Sprintf("\n%s", line)
			allowFirst = true
		}

		// we have our raw pattern! -- we need to convert this to a compiled regex
		re := compileRawPattern(t, pattern)

		var (
			capsIdx map[int]int
			m       *Match
			toMatch string
		)
		// now we need to parse the test cases if there are any
		// they start with 4 spaces -- if we don't get a 4-space start then
		// we're back out to our next pattern
		for scanner.Scan() {
			line = scanner.Text()

			// blank line is our separator for a new pattern
			if strings.TrimSpace(line) == "" {
				break
			}

			// could be either "    " or "\= Expect"
			if strings.HasPrefix(line, "\\= Expect") {
				continue
			} else if strings.HasPrefix(line, "    ") {
				// trim off leading spaces for our text to match
				toMatch = line[4:]
				// trim off trailing spaces too
				toMatch = strings.TrimRight(toMatch, " ")

				m = matchString(t, re, toMatch)

				capsIdx = make(map[int]int)
				continue
				//t.Fatalf("Expected match text to start with 4 spaces, instead got: '%v'", line)
			} else if strings.HasPrefix(line, "No match") {
				validateNoMatch(t, re, m)
				// no match means we're done
				continue
			} else if subs := matchGroup.FindStringSubmatch(line); len(subs) == 3 {
				gIdx, _ := strconv.Atoi(subs[1])
				if _, ok := capsIdx[gIdx]; !ok {
					capsIdx[gIdx] = 0
				}
				validateMatch(t, re, m, toMatch, subs[2], gIdx, capsIdx[gIdx])
				capsIdx[gIdx]++
				continue
			} else {
				// no match -- problem
				t.Fatalf("Unknown file format, expected match or match group but got '%v'", line)
			}
		}

	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
}

var matchGroup = regexp.MustCompile(`^\s*(\d+): (.*)`)

func problem(t *testing.T, input string, args ...interface{}) {
	failCount++
	t.Errorf(input, args...)
}

func validateNoMatch(t *testing.T, re *Regexp, m *Match) {
	if re == nil || m == nil {
		return
	}

	problem(t, "Expected no match for pattern '%v', but got '%v'", re.pattern, m.String())
}

func validateMatch(t *testing.T, re *Regexp, m *Match, toMatch, value string, idx, capIdx int) {
	if re == nil {
		// already error'd earlier up stream
		return
	}

	if m == nil {
		// we didn't match, but should have
		problem(t, "Expected match for pattern '%v' with input '%v', but got no match", re.pattern, toMatch)
		return
	}

	g := m.Groups()
	if len(g) <= idx {
		problem(t, "Expected group %v does not exist in pattern '%v' with input '%v'", idx, re.pattern, toMatch)
		return
	}

	if value == "<unset>" {
		// this means we shouldn't have a cap for this group
		if len(g[idx].Captures) > 0 {
			problem(t, "Expected no cap %v in group %v in pattern '%v' with input '%v'", g[idx].Captures[capIdx].String(), idx, re.pattern, toMatch)
		}

		return
	}

	if len(g[idx].Captures) <= capIdx {
		problem(t, "Expected cap %v does not exist in group %v in pattern '%v' with input '%v'", capIdx, idx, re.pattern, toMatch)
		return
	}

	escp := unEscapeGroup(g[idx].String())
	//escp := unEscapeGroup(g[idx].Captures[capIdx].String())
	if escp != value {
		problem(t, "Expected '%v' but got '%v' for cap %v, group %v for pattern '%v' with input '%v'", value, escp, capIdx, idx, re.pattern, toMatch)
		return
	}
}

func compileRawPattern(t *testing.T, pattern string) *Regexp {
	// check our end for RegexOptions -trim them off
	index := strings.LastIndexAny(pattern, "/\"")
	//
	// Append "= Debug" to compare details between corefx and regexp2 on the PCRE test suite
	//
	var opts RegexOptions

	if index+1 < len(pattern) {
		textOptions := pattern[index+1:]
		pattern = pattern[:index+1]
		// there are lots of complex options here
		for _, textOpt := range strings.Split(textOptions, ",") {
			switch textOpt {
			case "dupnames":
				// we don't know how to handle this...
			default:
				if strings.Contains(textOpt, "i") {
					opts |= IgnoreCase
				}
				if strings.Contains(textOpt, "s") {
					opts |= Singleline
				}
				if strings.Contains(textOpt, "m") {
					opts |= Multiline
				}
				if strings.Contains(textOpt, "x") {
					opts |= IgnorePatternWhitespace
				}
			}
		}

	}

	// trim off first and last char
	pattern = pattern[1 : len(pattern)-1]

	defer func() {
		if rec := recover(); rec != nil {
			problem(t, "PANIC in compiling \"%v\": %v", pattern, rec)
		}
	}()
	re, err := Compile(pattern, opts)
	if err != nil {
		problem(t, "Error parsing \"%v\": %v", pattern, err)
	}
	return re
}

func matchString(t *testing.T, re *Regexp, toMatch string) *Match {
	if re == nil {
		return nil
	}

	re.MatchTimeout = time.Second * 1

	escp := ""
	var err error
	if toMatch != "\\" {
		escp = unEscapeToMatch(toMatch)
	}
	m, err := re.FindStringMatch(escp)
	if err != nil {
		problem(t, "Error matching \"%v\" in pattern \"%v\": %v", toMatch, re.pattern, err)
	}
	return m
}

func containsEnder(line string, ender byte, allowFirst bool) bool {
	index := strings.LastIndexByte(line, ender)
	if index > 0 {
		return true
	} else if index == 0 && allowFirst {
		return true
	}
	return false
}

func unEscapeToMatch(line string) string {
	idx := strings.IndexRune(line, '\\')
	// no slashes means no unescape needed
	if idx == -1 {
		return line
	}

	buf := bytes.NewBufferString(line[:idx])
	// get the runes for the rest of the string -- we're going full parser scan on this

	inEscape := false
	// take any \'s and convert them
	for i := idx; i < len(line); i++ {
		ch := line[i]
		if ch == '\\' {
			if inEscape {
				buf.WriteByte(ch)
			}
			inEscape = !inEscape
			continue
		}
		if inEscape {
			switch ch {
			case 'x':
				buf.WriteByte(scanHex(line, &i))
			case 'a':
				buf.WriteByte(0x07)
			case 'b':
				buf.WriteByte('\b')
			case 'e':
				buf.WriteByte(0x1b)
			case 'f':
				buf.WriteByte('\f')
			case 'n':
				buf.WriteByte('\n')
			case 'r':
				buf.WriteByte('\r')
			case 't':
				buf.WriteByte('\t')
			case 'v':
				buf.WriteByte(0x0b)
			default:
				if ch >= '0' && ch <= '7' {
					buf.WriteByte(scanOctal(line, &i))
				} else {
					buf.WriteByte(ch)
					//panic(fmt.Sprintf("unexpected escape '%v' in %v", string(ch), line))
				}
			}
			inEscape = false
		} else {
			buf.WriteByte(ch)
		}
	}

	return buf.String()
}

func unEscapeGroup(val string) string {
	// use hex for chars 0x00-0x1f, 0x7f-0xff
	buf := &bytes.Buffer{}

	for i := 0; i < len(val); i++ {
		ch := val[i]
		if ch <= 0x1f || ch >= 0x7f {
			//write it as a \x00
			fmt.Fprintf(buf, "\\x%.2x", ch)
		} else {
			// write as-is
			buf.WriteByte(ch)
		}
	}

	return buf.String()
}

func scanHex(line string, idx *int) byte {
	if *idx >= len(line)-2 {
		panic(fmt.Sprintf("not enough hex chars in %v at %v", line, *idx))
	}
	(*idx)++
	d1 := hexDigit(line[*idx])
	(*idx)++
	d2 := hexDigit(line[*idx])
	if d1 < 0 || d2 < 0 {
		panic("bad hex chars")
	}

	return byte(d1*0x10 + d2)
}

// Returns n <= 0xF for a hex digit.
func hexDigit(ch byte) int {

	if d := uint(ch - '0'); d <= 9 {
		return int(d)
	}

	if d := uint(ch - 'a'); d <= 5 {
		return int(d + 0xa)
	}

	if d := uint(ch - 'A'); d <= 5 {
		return int(d + 0xa)
	}

	return -1
}

// Scans up to three octal digits (stops before exceeding 0377).
func scanOctal(line string, idx *int) byte {
	// Consume octal chars only up to 3 digits and value 0377

	// octals can be 3,2, or 1 digit
	c := 3

	if diff := len(line) - *idx; c > diff {
		c = diff
	}

	i := 0
	d := int(line[*idx] - '0')
	for c > 0 && d <= 7 {
		i *= 8
		i += d

		c--
		(*idx)++
		if *idx < len(line) {
			d = int(line[*idx] - '0')
		}
	}
	(*idx)--

	// Octal codes only go up to 255.  Any larger and the behavior that Perl follows
	// is simply to truncate the high bits.
	i &= 0xFF

	return byte(i)
}
