package tree_sitter

/*
#cgo CFLAGS: -Iinclude -Isrc -std=c11 -D_POSIX_C_SOURCE=200112L -D_DEFAULT_SOURCE
#include <tree_sitter/api.h>

extern bool queryProgressCallback(TSQueryCursorState *state);
*/
import "C"

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unsafe"

	"github.com/mattn/go-pointer"
)

type Query struct {
	_inner             *C.TSQuery
	captureNames       []string
	captureQuantifiers [][]CaptureQuantifier
	TextPredicates     [][]TextPredicateCapture
	propertySettings   [][]QueryProperty
	propertyPredicates [][]PropertyPredicate
	generalPredicates  [][]QueryPredicate
}

type CaptureQuantifier int

const (
	CaptureQuantifierZero CaptureQuantifier = iota
	CaptureQuantifierZeroOrOne
	CaptureQuantifierZeroOrMore
	CaptureQuantifierOne
	CaptureQuantifierOneOrMore
)

func newCaptureQuantifier(raw C.TSQuantifier) CaptureQuantifier {
	switch raw {
	case C.TSQuantifierZero:
		return CaptureQuantifierZero
	case C.TSQuantifierZeroOrOne:
		return CaptureQuantifierZeroOrOne
	case C.TSQuantifierZeroOrMore:
		return CaptureQuantifierZeroOrMore
	case C.TSQuantifierOne:
		return CaptureQuantifierOne
	case C.TSQuantifierOneOrMore:
		return CaptureQuantifierOneOrMore
	default:
		panic("invalid CaptureQuantifier")
	}
}

// A stateful object for executing a [Query] on a syntax [Tree].
type QueryCursor struct {
	_inner *C.TSQueryCursor
}

// A stateful object that is passed into the progress callback [QueryOptions.ProgressCallback].
// to provide the current state of the query.
type QueryCursorState struct {
	// The byte offset in the document that the query is at.
	CurrentByteOffset uint32
}

// Options for query execution
type QueryCursorOptions struct {
	// A function that will be called periodically during the execution of the query to check
	// if query execution should be cancelled. If the progress callback returns `true`, then
	// query execution will be canceled. You can also use this to instrument query execution
	// and check where the query is at in the document. The progress callback takes a single
	// argument, which is a [QueryCursorState] representing the current state of the query.
	ProgressCallback func(QueryCursorState) bool
}

// A key-value pair associated with a particular pattern in a [Query].
type QueryProperty struct {
	Key       string
	Value     *string
	CaptureId *uint
}

type QueryPredicateArg struct {
	CaptureId *uint
	String    *string
}

// A key-value pair associated with a particular pattern in a [Query].
type QueryPredicate struct {
	Operator string
	Args     []QueryPredicateArg
}

// A match of a [Query] to a particular set of [Node]s.
type QueryMatch struct {
	cursor       *C.TSQueryCursor
	Captures     []QueryCapture
	PatternIndex uint
	id           uint
}

// A sequence of [QueryMatch]es associated with a given [QueryCursor].
type QueryMatches struct {
	_inner  *C.TSQueryCursor
	query   *Query
	text    []byte
	buffer1 []byte
	buffer2 []byte
}

// A sequence of [QueryCapture]s associated with a given [QueryCursor].
type QueryCaptures struct {
	_inner  *C.TSQueryCursor
	query   *Query
	text    []byte
	buffer1 []byte
	buffer2 []byte
}

// A particular [Node] that has been captured with a particular name within a [Query].
// Note that this is a C-compatible struct
type QueryCapture struct {
	Node  Node
	Index uint32
}

type QueryError struct {
	Message string
	Row     uint
	Column  uint
	Offset  uint
	Kind    QueryErrorKind
}

type TextPredicateCapture struct {
	Value         any
	Type          TextPredicateType
	CaptureId     uint
	Positive      bool
	MatchAllNodes bool
}

type TextPredicateType int

const (
	TextPredicateTypeEqCapture TextPredicateType = iota
	TextPredicateTypeEqString
	TextPredicateTypeMatchString
	TextPredicateTypeAnyString
)

type PropertyPredicate struct {
	Property QueryProperty
	Positive bool
}

func (e QueryError) Error() string {
	var msg string
	switch e.Kind {
	case QueryErrorField:
		msg = "Invalid field name "
	case QueryErrorNodeType:
		msg = "Invalid node type "
	case QueryErrorCapture:
		msg = "Invalid capture name "
	case QueryErrorPredicate:
		msg = "Invalid predicate: "
	case QueryErrorStructure:
		msg = "Impossible pattern:\n"
	case QueryErrorSyntax:
		msg = "Invalid syntax:\n"
	case QueryErrorLanguage:
		msg = ""
	}

	if msg == "" {
		return e.Message
	}
	return fmt.Sprintf("Query error at %d:%d. %s%s", e.Row+1, e.Column+1, msg, e.Message)
}

type QueryErrorKind int

const (
	QueryErrorSyntax QueryErrorKind = iota
	QueryErrorNodeType
	QueryErrorField
	QueryErrorCapture
	QueryErrorPredicate
	QueryErrorStructure
	QueryErrorLanguage
)

func NewQuery(language *Language, source string) (*Query, *QueryError) {
	var errorOffset C.uint32_t
	var errorType C.TSQueryError
	bytes := []byte(source)

	var bytesPtr *C.char
	if len(bytes) > 0 {
		bytesPtr = (*C.char)(unsafe.Pointer(&bytes[0]))
	} else {
		bytesPtr = nil
	}

	// Compile the query.
	ptr := C.ts_query_new(
		language.Inner,
		bytesPtr,
		C.uint32_t(len(bytes)),
		&errorOffset,
		&errorType,
	)

	// On failure, build an error based on the error code and offset.
	if ptr == nil {
		if errorType == C.TSQueryErrorLanguage {
			lErr := &LanguageError{
				version: language.AbiVersion(),
			}
			return nil, &QueryError{
				Row:     0,
				Column:  0,
				Offset:  0,
				Message: lErr.Error(),
				Kind:    QueryErrorLanguage,
			}
		}

		offset := uint(errorOffset)
		var lineStart uint
		var row uint
		var lineContainingError string
		for _, line := range strings.Split(source, "\n") {
			lineEnd := lineStart + uint(len(line)) + 1
			if lineEnd > offset {
				lineContainingError = string(line)
				break
			}
			lineStart = lineEnd
			row++
		}
		column := offset - lineStart

		var kind QueryErrorKind
		var message string
		switch errorType {
		// Error types that report names
		case C.TSQueryErrorNodeType, C.TSQueryErrorField, C.TSQueryErrorCapture:
			suffix := string(bytes[offset:])
			inQuotes := offset > 0 && bytes[offset-1] == '"'
			endOffset := len(suffix)
			backslashes := 0

			if inQuotes {
				// Handle quoted strings
				for i, c := range suffix {
					if c == '"' && backslashes%2 == 0 {
						endOffset = i
						break
					} else if c == '\\' {
						backslashes++
					} else {
						backslashes = 0
					}
				}
			} else {
				// Handle unquoted strings
				for i, c := range suffix {
					if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-", c) {
						endOffset = i
						break
					}
				}
			}

			message = suffix[:endOffset]
			switch errorType {
			case C.TSQueryErrorNodeType:
				kind = QueryErrorNodeType
			case C.TSQueryErrorField:
				kind = QueryErrorField
			case C.TSQueryErrorCapture:
				kind = QueryErrorCapture
			}

		// Error types that report positions
		default:
			if lineContainingError == "" {
				message = "Unexpected EOF"
			} else {
				message = lineContainingError + "\n" + strings.Repeat(" ", int(offset-lineStart)) + "^"
			}
			switch errorType {
			case C.TSQueryErrorStructure:
				kind = QueryErrorStructure
			default:
				kind = QueryErrorSyntax
			}
		}
		return nil, &QueryError{
			Row:     row,
			Column:  column,
			Offset:  offset,
			Message: message,
			Kind:    kind,
		}
	}

	res, err := fromRawParts(ptr, source)
	return res, err
}

func fromRawParts(ptr *C.TSQuery, source string) (*Query, *QueryError) {
	stringCount := int(C.ts_query_string_count(ptr))
	captureCount := int(C.ts_query_capture_count(ptr))
	patternCount := int(C.ts_query_pattern_count(ptr))

	captureNames := make([]string, captureCount)
	captureQuantifiersVec := make([][]CaptureQuantifier, patternCount)
	textPredicatesVec := make([][]TextPredicateCapture, patternCount)
	propertyPredicatesVec := make([][]PropertyPredicate, patternCount)
	propertySettingsVec := make([][]QueryProperty, patternCount)
	generalPredicatesVec := make([][]QueryPredicate, patternCount)

	// Build a vector of strings to store the capture names.
	for i := 0; i < captureCount; i++ {
		var length C.uint32_t
		name := C.ts_query_capture_name_for_id(ptr, C.uint32_t(i), &length)
		captureNames[i] = C.GoStringN(name, C.int(length))
	}

	// Build a vector to store capture qunatifiers.
	for i := 0; i < patternCount; i++ {
		captureQuantifiers := make([]CaptureQuantifier, captureCount)
		for j := 0; j < captureCount; j++ {
			quantifier := C.ts_query_capture_quantifier_for_id(ptr, C.uint32_t(i), C.uint32_t(j))
			captureQuantifiers[j] = newCaptureQuantifier(quantifier)
		}
		captureQuantifiersVec[i] = captureQuantifiers
	}

	// Build a vector of strings to represent literal values used in predicates.
	stringValues := make([]string, stringCount)
	for i := 0; i < stringCount; i++ {
		var length C.uint32_t
		value := C.ts_query_string_value_for_id(ptr, C.uint32_t(i), &length)
		stringValues[i] = C.GoStringN(value, C.int(length))
	}

	// Build a vector of strings to represent literal values used in predicates.
	for i := 0; i < patternCount; i++ {
		var length C.uint32_t
		rawPredicates := C.ts_query_predicates_for_pattern(ptr, C.uint32_t(i), &length)
		predicateSteps := unsafe.Slice(rawPredicates, int(length))

		byteOffset := C.ts_query_start_byte_for_pattern(ptr, C.uint32_t(i))
		row := 0
		for i, c := range source {
			if i >= int(byteOffset) {
				break
			}
			if c == '\n' {
				row++
			}
		}
		const (
			TYPE_DONE    = C.TSQueryPredicateStepTypeDone
			TYPE_CAPTURE = C.TSQueryPredicateStepTypeCapture
			TYPE_STRING  = C.TSQueryPredicateStepTypeString
		)

		textPredicates := make([]TextPredicateCapture, 0)
		propertyPredicates := make([]PropertyPredicate, 0)
		propertySettings := make([]QueryProperty, 0)
		generalPredicates := make([]QueryPredicate, 0)

		// iterate over predicateSteps, and consi
		split := func(steps []C.TSQueryPredicateStep, sep C.TSQueryPredicateStepType) [][]C.TSQueryPredicateStep {
			var result [][]C.TSQueryPredicateStep
			var current []C.TSQueryPredicateStep
			for _, t := range steps {
				if t._type == sep {
					result = append(result, current)
					current = nil
				} else {
					current = append(current, t)
				}
			}
			if len(current) > 0 {
				result = append(result, current)
			}
			return result
		}

		for _, p := range split(predicateSteps, TYPE_DONE) {
			if len(p) == 0 {
				continue
			}

			if p[0]._type != TYPE_STRING {
				C.ts_query_delete(ptr)
				return nil, predicateError(uint(row), fmt.Sprintf("Expected predicate to start with a function name. Got @%s.", captureNames[p[0].value_id]))
			}

			// Build a predicate for each of the known predicate function names.
			operatorName := stringValues[p[0].value_id]
			switch operatorName {
			case "eq?", "not-eq?", "any-eq?", "any-not-eq?":
				if len(p) != 3 {
					C.ts_query_delete(ptr)
					return nil, predicateError(uint(row), fmt.Sprintf("Wrong number of arguments to #eq? predicate. Expected 2, got %d.", len(p)-1))
				}
				if p[1]._type != TYPE_CAPTURE {
					C.ts_query_delete(ptr)
					return nil, predicateError(uint(row), fmt.Sprintf("First argument to #eq? predicate must be a capture name. Got literal %s.", stringValues[p[1].value_id]))
				}

				isPositive := operatorName == "eq?" || operatorName == "any-eq?"
				matchAll := operatorName == "eq?" || operatorName == "not-eq?"
				if p[2]._type == TYPE_CAPTURE {
					textPredicates = append(textPredicates, TextPredicateCapture{
						Type:          TextPredicateTypeEqCapture,
						CaptureId:     uint(p[1].value_id),
						Value:         uint(p[2].value_id),
						Positive:      isPositive,
						MatchAllNodes: matchAll,
					})
				} else {
					textPredicates = append(textPredicates, TextPredicateCapture{
						Type:          TextPredicateTypeEqString,
						CaptureId:     uint(p[1].value_id),
						Value:         stringValues[p[2].value_id],
						Positive:      isPositive,
						MatchAllNodes: matchAll,
					})
				}

			case "match?", "not-match?", "any-match?", "any-not-match?":
				if len(p) != 3 {
					C.ts_query_delete(ptr)
					return nil, predicateError(uint(row), fmt.Sprintf("Wrong number of arguments to #match? predicate. Expected 2, got %d.", len(p)-1))
				}
				if p[1]._type != TYPE_CAPTURE {
					C.ts_query_delete(ptr)
					return nil, predicateError(uint(row), fmt.Sprintf("First argument to #match? predicate must be a capture name. Got literal %s.", stringValues[p[1].value_id]))
				}
				if p[2]._type == TYPE_CAPTURE {
					C.ts_query_delete(ptr)
					return nil, predicateError(uint(row), fmt.Sprintf("Second argument to #match? predicate must be a literal. Got capture @%s.", captureNames[p[2].value_id]))
				}

				isPositive := operatorName == "match?" || operatorName == "any-match?"
				matchAll := operatorName == "match?" || operatorName == "not-match?"
				regex, err := regexp.Compile(stringValues[p[2].value_id])
				if err != nil {
					C.ts_query_delete(ptr)
					return nil, predicateError(uint(row), fmt.Sprintf("Invalid regex: '%s'", stringValues[p[2].value_id]))
				}
				textPredicates = append(textPredicates, TextPredicateCapture{
					Type:          TextPredicateTypeMatchString,
					CaptureId:     uint(p[1].value_id),
					Value:         regex,
					Positive:      isPositive,
					MatchAllNodes: matchAll,
				})

			case "set!":
				property, err := parseProperty(uint(row), operatorName, captureNames, stringValues, p[1:])
				if err != nil {
					C.ts_query_delete(ptr)
					return nil, err
				}
				propertySettings = append(propertySettings, property)

			case "is?", "is-not?":
				property, err := parseProperty(uint(row), operatorName, captureNames, stringValues, p[1:])
				if err != nil {
					C.ts_query_delete(ptr)
					return nil, err
				}
				propertyPredicates = append(propertyPredicates, PropertyPredicate{
					Property: property,
					Positive: operatorName == "is?",
				})

			case "any-of?", "not-any-of?":
				if len(p) < 2 {
					C.ts_query_delete(ptr)
					return nil, predicateError(uint(row), fmt.Sprintf("Wrong number of arguments to #any-of? predicate. Expected at least 1, got %d.", len(p)-1))
				}
				if p[1]._type != TYPE_CAPTURE {
					C.ts_query_delete(ptr)
					return nil, predicateError(uint(row), fmt.Sprintf("First argument to #any-of? predicate must be a capture name. Got literal %s.", stringValues[p[1].value_id]))
				}

				isPositive := operatorName == "any-of?"
				values := make([]string, 0)

				for _, arg := range p[2:] {
					if arg._type == TYPE_CAPTURE {
						C.ts_query_delete(ptr)
						return nil, predicateError(uint(row), fmt.Sprintf("Arguments to #any-of? predicate must be literals. Got capture @%s.", captureNames[arg.value_id]))
					}
					values = append(values, stringValues[arg.value_id])
				}
				textPredicates = append(textPredicates, TextPredicateCapture{
					Type:          TextPredicateTypeAnyString,
					CaptureId:     uint(p[1].value_id),
					Value:         values,
					Positive:      isPositive,
					MatchAllNodes: true,
				})

			default:
				args := make([]QueryPredicateArg, 0)
				for _, a := range p[1:] {
					if a._type == TYPE_CAPTURE {
						args = append(args, QueryPredicateArg{CaptureId: new(uint), String: nil})
						*args[len(args)-1].CaptureId = uint(a.value_id)
					} else {
						args = append(args, QueryPredicateArg{CaptureId: nil, String: new(string)})
						*args[len(args)-1].String = stringValues[a.value_id]
					}
				}
				generalPredicates = append(generalPredicates, QueryPredicate{
					Operator: operatorName,
					Args:     args,
				})
			}
		}

		textPredicatesVec[i] = textPredicates
		propertyPredicatesVec[i] = propertyPredicates
		propertySettingsVec[i] = propertySettings
		generalPredicatesVec[i] = generalPredicates
	}

	query := &Query{
		_inner:             ptr,
		captureNames:       captureNames,
		captureQuantifiers: captureQuantifiersVec,
		TextPredicates:     textPredicatesVec,
		propertyPredicates: propertyPredicatesVec,
		propertySettings:   propertySettingsVec,
		generalPredicates:  generalPredicatesVec,
	}
	return query, nil
}

func (q *Query) Close() {
	C.ts_query_delete(q._inner)
}

// Get the byte offset where the given pattern starts in the query's source.
func (q *Query) StartByteForPattern(index uint) uint {
	if index >= uint(len(q.TextPredicates)) {
		panic(fmt.Sprintf("Pattern index is %d but the pattern count is %d", index, len(q.TextPredicates)))
	}
	return uint(C.ts_query_start_byte_for_pattern(q._inner, C.uint32_t(index)))
}

// Get the byte offset where the given pattern ends in the query's source.
func (q *Query) EndByteForPattern(index uint) uint {
	if index >= uint(len(q.TextPredicates)) {
		panic(fmt.Sprintf("Pattern index is %d but the pattern count is %d", index, len(q.TextPredicates)))
	}
	return uint(C.ts_query_end_byte_for_pattern(q._inner, C.uint32_t(index)))
}

// Get the number of patterns in the query.
func (q *Query) PatternCount() uint {
	return uint(C.ts_query_pattern_count(q._inner))
}

// Get the names of the captures used in the query.
func (q *Query) CaptureNames() []string {
	return q.captureNames
}

// Get the quantifiers of the captures used in the query.
func (q *Query) CaptureQuantifiers(index uint) []CaptureQuantifier {
	return q.captureQuantifiers[index]
}

// Get the index for a given capture name.
func (q *Query) CaptureIndexForName(name string) (uint, bool) {
	for i, n := range q.captureNames {
		if n == name {
			return uint(i), true
		}
	}
	return 0, false
}

// Get the properties that are checked for the given pattern index.
//
// This includes predicates with the operators `is?` and `is-not?`.
func (q *Query) PropertyPredicates(index uint) []PropertyPredicate {
	return q.propertyPredicates[index]
}

// Get the properties that are set for the given pattern index.
//
// This includes predicates with the operator `set!`.
func (q *Query) PropertySettings(index uint) []QueryProperty {
	return q.propertySettings[index]
}

// Get the other user-defined predicates associated with the given index.
//
// This includes predicate with operators other than:
// * `match?`
// * `eq?` and `not-eq?`
// * `is?` and `is-not?`
// * `set!`
func (q *Query) GeneralPredicates(index uint) []QueryPredicate {
	return q.generalPredicates[index]
}

// Disable a certain capture within a query.
//
// This prevents the capture from being returned in matches, and also
// avoids any resource usage associated with recording the capture.
func (q *Query) DisableCapture(captureName string) {
	cstr := C.CString(captureName)
	C.ts_query_disable_capture(q._inner, cstr, C.uint32_t(len(captureName)))
	go_free(unsafe.Pointer(cstr))
}

// Disable a certain pattern within a query.
//
// This prevents the pattern from matching, and also avoids any resource
// usage associated with the pattern.
func (q *Query) DisablePattern(index uint) {
	C.ts_query_disable_pattern(q._inner, C.uint32_t(index))
}

// Check if a given pattern within a query has a single root node.
func (q *Query) IsPatternRooted(index uint) bool {
	return bool(C.ts_query_is_pattern_rooted(q._inner, C.uint32_t(index)))
}

// Check if a given pattern within a query has a single root node.
func (q *Query) IsPatternNonLocal(index uint) bool {
	return bool(C.ts_query_is_pattern_non_local(q._inner, C.uint32_t(index)))
}

// Check if a given step in a query is 'definite'.
//
// A query step is 'definite' if its parent pattern will be guaranteed to
// match successfully once it reaches the step.
func (q *Query) IsPatternGuaranteedAtStep(byteOffset uint) bool {
	return bool(C.ts_query_is_pattern_guaranteed_at_step(q._inner, C.uint32_t(byteOffset)))
}

func parseProperty(row uint, functionName string, captureNames []string, stringValues []string, args []C.TSQueryPredicateStep) (QueryProperty, *QueryError) {
	if len(args) == 0 || len(args) > 3 {
		return QueryProperty{}, predicateError(row, fmt.Sprintf("Wrong number of arguments to %s predicate. Expected 1 to 3, got %d.", functionName, len(args)))
	}

	var captureId *uint
	var key *string
	var value *string

	for _, arg := range args {
		if arg._type == C.TSQueryPredicateStepTypeCapture {
			if captureId != nil {
				return QueryProperty{}, predicateError(row, fmt.Sprintf("Invalid arguments to %s predicate. Unexpected second capture name @%s", functionName, captureNames[arg.value_id]))
			}
			captureId = new(uint)
			*captureId = uint(arg.value_id)
		} else if key == nil {
			k := stringValues[arg.value_id]
			key = &k
		} else if value == nil {
			v := stringValues[arg.value_id]
			value = &v
		} else {
			return QueryProperty{}, predicateError(row, fmt.Sprintf("Invalid arguments to %s predicate. Unexpected third argument @%s", functionName, stringValues[arg.value_id]))
		}
	}

	if key == nil {
		return QueryProperty{}, predicateError(row, fmt.Sprintf("Invalid arguments to %s predicate. Missing key argument", functionName))
	}

	return QueryProperty{
		Key:       *key,
		Value:     value,
		CaptureId: captureId,
	}, nil
}

// Create a new cursor for executing a given query.
//
// The cursor stores the state that is needed to iteratively search for
// matches.
func NewQueryCursor() *QueryCursor {
	return &QueryCursor{_inner: C.ts_query_cursor_new()}
}

// Delete the underlying memory for a query cursor.
func (qc *QueryCursor) Close() {
	C.ts_query_cursor_delete(qc._inner)
}

// Return the maximum number of in-progress matches for this cursor.
func (qc *QueryCursor) MatchLimit() uint {
	return uint(C.ts_query_cursor_match_limit(qc._inner))
}

// Set the maximum number of in-progress matches for this cursor.
// The limit must be > 0 and <= 65536.
func (qc *QueryCursor) SetMatchLimit(limit uint) {
	C.ts_query_cursor_set_match_limit(qc._inner, C.uint32_t(limit))
}

// Set the maximum duration in microseconds that query execution should be allowed to
// take before halting.
//
// If query execution takes longer than this, it will halt early, returning None.
func (qc *QueryCursor) SetTimeoutMicros(timeoutMicros uint64) {
	C.ts_query_cursor_set_timeout_micros(qc._inner, C.uint64_t(timeoutMicros))
}

// Get the duration in microseconds that query execution is allowed to take.
//
// This is set via [QueryCursor.SetTimeoutMicros]
func (qc *QueryCursor) TimeoutMicros() uint64 {
	return uint64(C.ts_query_cursor_timeout_micros(qc._inner))
}

// Check if, on its last execution, this cursor exceeded its maximum number
// of in-progress matches.
func (qc *QueryCursor) DidExceedMatchLimit() bool {
	return bool(C.ts_query_cursor_did_exceed_match_limit(qc._inner))
}

// Iterate over all of the matches in the order that they were found.
//
// Each match contains the index of the pattern that matched, and a list of
// captures. Because multiple patterns can match the same set of nodes,
// one match may contain captures that appear *before* some of the
// captures from a previous match.
func (qc *QueryCursor) Matches(query *Query, node *Node, text []byte) QueryMatches {
	C.ts_query_cursor_exec(qc._inner, query._inner, node._inner)
	qm := QueryMatches{
		_inner:  qc._inner,
		query:   query,
		text:    text,
		buffer1: []byte{},
		buffer2: []byte{},
	}
	if qm._inner != qc._inner {
		panic("inner pointers of `QueryCursor` and `QueryMatches` are not equal")
	}
	if qm.query != query {
		panic("query pointers of `QueryCursor` and `QueryMatches` are not equal")
	}

	return qm
}

// This C function is passed to Tree-sitter as the progress callback.
//
//export queryProgressCallback
func queryProgressCallback(state *C.TSQueryCursorState) C.bool {
	payload := pointer.Restore(state.payload).(*QueryCursorOptions)
	return C.bool(payload.ProgressCallback(QueryCursorState{
		CurrentByteOffset: uint32(state.current_byte_offset),
	}))
}

// Iterate over all of the matches in the order that they were found, with options.
//
// Each match contains the index of the pattern that matched, and a list of
// captures. Because multiple patterns can match the same set of nodes,
// one match may contain captures that appear *before* some of the
// captures from a previous match.
func (qc *QueryCursor) MatchesWithOptions(query *Query, node *Node, text []byte, options QueryCursorOptions) QueryMatches {
	cOptions := &C.TSQueryCursorOptions{
		payload:           pointer.Save(&options),
		progress_callback: (*[0]byte)(C.queryProgressCallback),
	}

	C.ts_query_cursor_exec_with_options(qc._inner, query._inner, node._inner, cOptions)

	qm := QueryMatches{
		_inner:  qc._inner,
		query:   query,
		text:    text,
		buffer1: []byte{},
		buffer2: []byte{},
	}
	if qm._inner != qc._inner {
		panic("inner pointers of `QueryCursor` and `QueryMatches` are not equal")
	}
	if qm.query != query {
		panic("query pointers of `QueryCursor` and `QueryMatches` are not equal")
	}

	return qm
}

// Iterate over all of the individual captures in the order that they
// appear.
//
// This is useful if you don't care about which pattern matched, and just
// want a single, ordered sequence of captures.
func (qc *QueryCursor) Captures(query *Query, node *Node, text []byte) QueryCaptures {
	C.ts_query_cursor_exec(qc._inner, query._inner, node._inner)
	return QueryCaptures{
		_inner:  qc._inner,
		query:   query,
		text:    text,
		buffer1: []byte{},
		buffer2: []byte{},
	}
}

// Set the range of bytes in which the query will be executed.
//
// The query cursor will return matches that intersect with the given point range.
// This means that a match may be returned even if some of its captures fall
// outside the specified range, as long as at least part of the match
// overlaps with the range.
//
// For example, if a query pattern matches a node that spans a larger area
// than the specified range, but part of that node intersects with the range,
// the entire match will be returned.
//
// This will have no effect if the start byte is greater than the end byte.
func (qc *QueryCursor) SetByteRange(startByte uint, endByte uint) *QueryCursor {
	C.ts_query_cursor_set_byte_range(qc._inner, C.uint32_t(startByte), C.uint32_t(endByte))
	return qc
}

// Set the range of (row, column) positions in which the query will be executed.
//
// The query cursor will return matches that intersect with the given point range.
// This means that a match may be returned even if some of its captures fall
// outside the specified range, as long as at least part of the match
// overlaps with the range.
//
// For example, if a query pattern matches a node that spans a larger area
// than the specified range, but part of that node intersects with the range,
// the entire match will be returned.
//
// This will have no effect if the start point is greater than the end point.
func (qc *QueryCursor) SetPointRange(startPoint Point, endPoint Point) *QueryCursor {
	C.ts_query_cursor_set_point_range(qc._inner, startPoint.toTSPoint(), endPoint.toTSPoint())
	return qc
}

// Set the maximum start depth for a query cursor.
//
// This prevents cursors from exploring children nodes at a certain depth.
// Note if a pattern includes many children, then they will still be
// checked.
//
// The zero max start depth value can be used as a special behavior and
// it helps to destructure a subtree by staying on a node and using
// captures for interested parts. Note that the zero max start depth
// only limit a search depth for a pattern's root node but other nodes
// that are parts of the pattern may be searched at any depth what
// defined by the pattern structure.
//
// Set to `nil` to remove the maximum start depth.
func (qc *QueryCursor) SetMaxStartDepth(depth *uint) *QueryCursor {
	if depth == nil {
		C.ts_query_cursor_set_max_start_depth(qc._inner, C.uint32_t(math.MaxUint32))
	} else {
		C.ts_query_cursor_set_max_start_depth(qc._inner, C.uint32_t(*depth))
	}
	return qc
}

func (qm *QueryMatch) Id() uint {
	return qm.id
}

func newQueryMatch(m *C.TSQueryMatch, cursor *C.TSQueryCursor) QueryMatch {
	var captures []QueryCapture
	if m.capture_count > 0 {
		cCaptures := unsafe.Slice(m.captures, m.capture_count)
		captures = *(*[]QueryCapture)(unsafe.Pointer(&cCaptures))
	}
	return QueryMatch{
		cursor:       cursor,
		Captures:     captures,
		PatternIndex: uint(m.pattern_index),
		id:           uint(m.id),
	}
}

func (qm *QueryMatch) Remove() {
	C.ts_query_cursor_remove_match(qm.cursor, C.uint32_t(qm.id))
}

func (qm *QueryMatch) NodesForCaptureIndex(captureIndex uint) []Node {
	nodes := make([]Node, 0)
	for _, capture := range qm.Captures {
		if uint(capture.Index) == captureIndex {
			nodes = append(nodes, capture.Node)
		}
	}

	return nodes
}

func (qm *QueryMatch) SatisfiesTextPredicate(query *Query, buffer1, buffer2 []byte, text []byte) bool {
	satisfies := true

	condition := func(predicate TextPredicateCapture) bool {
		switch predicate.Type {
		case TextPredicateTypeEqCapture:
			i := predicate.CaptureId
			j := predicate.Value.(uint)
			nodes1 := qm.NodesForCaptureIndex(i)
			nodes2 := qm.NodesForCaptureIndex(j)
			for len(nodes1) > 0 && len(nodes2) > 0 {
				node1 := nodes1[0]
				node2 := nodes2[0]
				isPositiveMatch := bytes.Equal(text[node1.StartByte():node1.EndByte()], text[node2.StartByte():node2.EndByte()])
				if isPositiveMatch != predicate.Positive && predicate.MatchAllNodes {
					return false
				}
				if isPositiveMatch == predicate.Positive && !predicate.MatchAllNodes {
					return true
				}
				nodes1 = nodes1[1:]
				nodes2 = nodes2[1:]
			}
			return len(nodes1) == 0 && len(nodes2) == 0

		case TextPredicateTypeEqString:
			i := predicate.CaptureId
			s := predicate.Value.(string)
			nodes := qm.NodesForCaptureIndex(i)
			for _, node := range nodes {
				nodeText := text[node.StartByte():node.EndByte()]
				isPositiveMatch := bytes.Equal(nodeText, []byte(s))
				if isPositiveMatch != predicate.Positive && predicate.MatchAllNodes {
					return false
				}
				if isPositiveMatch == predicate.Positive && !predicate.MatchAllNodes {
					return true
				}
			}
			return true

		case TextPredicateTypeMatchString:
			i := predicate.CaptureId
			r := predicate.Value.(*regexp.Regexp)

			nodes := qm.NodesForCaptureIndex(i)
			for _, node := range nodes {
				nodeText := text[node.StartByte():node.EndByte()]
				isPositiveMatch := r.Match(nodeText)
				if isPositiveMatch != predicate.Positive && predicate.MatchAllNodes {
					return false
				}
				if isPositiveMatch == predicate.Positive && !predicate.MatchAllNodes {
					return true
				}
			}
			return true
		case TextPredicateTypeAnyString:
			i := predicate.CaptureId
			v := predicate.Value.([]string)
			nodes := qm.NodesForCaptureIndex(i)
			for _, node := range nodes {
				nodeText := text[node.StartByte():node.EndByte()]
				isPositiveMatch := false
				for _, s := range v {
					if bytes.Equal(nodeText, []byte(s)) {
						isPositiveMatch = true
						break
					}
				}
				if isPositiveMatch != predicate.Positive {
					return false
				}
			}
			return true
		}

		return false
	}

	for _, predicate := range query.TextPredicates[qm.PatternIndex] {
		if !condition(predicate) {
			satisfies = false
			break
		}
	}

	return satisfies
}

func NewQueryProperty(key string, value *string, captureId *uint) QueryProperty {
	return QueryProperty{
		Key:       key,
		Value:     value,
		CaptureId: captureId,
	}
}

// Next will return the next match in the sequence of matches.
//
// Subsequent calls to [QueryMatches.Next] will overwrite the memory at the same location as prior matches, since the memory is reused. You can think of this as a stateful iterator.
// If you need to keep the data of a prior match without it being overwritten, you should copy what you need before calling [QueryMatches.Next] again.
//
// If there are no more matches, it will return nil.
func (qm *QueryMatches) Next() *QueryMatch {
	for {
		m := (*C.TSQueryMatch)(C.malloc(C.sizeof_TSQueryMatch))
		defer C.free(unsafe.Pointer(m))
		if C.ts_query_cursor_next_match(qm._inner, m) {
			result := newQueryMatch(m, qm._inner)
			if result.SatisfiesTextPredicate(
				qm.query,
				qm.buffer1,
				qm.buffer2,
				qm.text,
			) {
				return &result
			}
		} else {
			return nil
		}
	}
}

// Next will return the next match in the sequence of matches, as well as the index of the capture.
//
// Subsequent calls to [QueryCaptures.Next] will overwrite the memory at the same location as prior matches, since the memory is reused. You can think of this as a stateful iterator.
// If you need to keep the data of a prior match without it being overwritten, you should copy what you need before calling [QueryCaptures.Next] again.
//
// If there are no more matches, it will return nil.
func (qc *QueryCaptures) Next() (*QueryMatch, uint) {
	for {
		m := (*C.TSQueryMatch)(C.malloc(C.sizeof_TSQueryMatch))
		var captureIndex C.uint32_t
		if C.ts_query_cursor_next_capture(qc._inner, m, &captureIndex) {
			result := newQueryMatch(m, qc._inner)
			if result.SatisfiesTextPredicate(
				qc.query,
				qc.buffer1,
				qc.buffer2,
				qc.text,
			) {
				return &result, uint(captureIndex)
			}
			result.Remove()
		} else {
			return nil, 0
		}
	}
}

func (qm *QueryMatches) SetByteRange(startByte uint, endByte uint) {
	C.ts_query_cursor_set_byte_range(qm._inner, C.uint32_t(startByte), C.uint32_t(endByte))
}

func (qm *QueryMatches) SetPointRange(startPoint Point, endPoint Point) {
	C.ts_query_cursor_set_point_range(qm._inner, startPoint.toTSPoint(), endPoint.toTSPoint())
}

func (qc *QueryCaptures) SetByteRange(startByte uint, endByte uint) {
	C.ts_query_cursor_set_byte_range(qc._inner, C.uint32_t(startByte), C.uint32_t(endByte))
}

func (qc *QueryCaptures) SetPointRange(startPoint Point, endPoint Point) {
	C.ts_query_cursor_set_point_range(qc._inner, startPoint.toTSPoint(), endPoint.toTSPoint())
}

func predicateError(row uint, message string) *QueryError {
	return &QueryError{
		Kind:    QueryErrorPredicate,
		Row:     row,
		Column:  0,
		Offset:  0,
		Message: message,
	}
}
