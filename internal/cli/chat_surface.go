package cli

import (
	"strings"
	"time"
)

type surfaceCaps struct {
	slashCompletion bool
	approvals       bool
	askRequests     bool
	todoPanel       bool
	statusDataRow   bool
	queueIndicator  bool
	managerFooter   bool
}

func mainSurfaceCaps() surfaceCaps {
	return surfaceCaps{
		slashCompletion: true,
		approvals:       true,
		askRequests:     true,
		todoPanel:       true,
		statusDataRow:   true,
		queueIndicator:  true,
		managerFooter:   true,
	}
}

func sideSurfaceCaps() surfaceCaps {
	return surfaceCaps{}
}

type conversationSurface struct {
	caps surfaceCaps

	transcript []string
	dirty      bool

	pending       *strings.Builder
	reasoning     *strings.Builder
	answerIdx     int
	answerFlushed int

	reasoningLineIdx int
	reasoningTextIdx int
	reasoningView    []byte
	reasoningNative  bool
	thinkStart       time.Time
	showReasoning    bool

	turnDiscarded  bool
	bubblePending  bool
	bubbleStartIdx int
	pendingRestore string
	pendingPastes  []string
	runStart       time.Time
	elapsed        int
	turnTokens     int
	retryAttempt   int
	retryMax       int
	running        bool

	toolStreamIdx     int
	toolStreamID      string
	toolTail          []string
	toolPartial       string
	toolLineCount     int
	toolLineCountByID map[string]int
	toolStreamStart   time.Time
	toolStreamFrame   int

	shellOutputs       map[string]string
	shellExpanded      map[string]bool
	shellTranscriptIdx map[string]int
}

func newConversationSurface(caps surfaceCaps, showReasoning bool) conversationSurface {
	return conversationSurface{
		caps:               caps,
		pending:            &strings.Builder{},
		reasoning:          &strings.Builder{},
		answerIdx:          -1,
		reasoningLineIdx:   -1,
		reasoningTextIdx:   -1,
		showReasoning:      showReasoning,
		toolStreamIdx:      -1,
		toolLineCountByID:  map[string]int{},
		shellOutputs:       map[string]string{},
		shellExpanded:      map[string]bool{},
		shellTranscriptIdx: map[string]int{},
	}
}

func (s *conversationSurface) resetTranscript() {
	s.transcript = nil
	s.dirty = true
	if s.pending == nil {
		s.pending = &strings.Builder{}
	} else {
		s.pending.Reset()
	}
	if s.reasoning == nil {
		s.reasoning = &strings.Builder{}
	} else {
		s.reasoning.Reset()
	}
	s.answerIdx = -1
	s.answerFlushed = 0
	s.reasoningLineIdx = -1
	s.reasoningTextIdx = -1
	s.reasoningView = s.reasoningView[:0]
	s.reasoningNative = false
	s.turnDiscarded = false
	s.bubblePending = false
	s.pendingRestore = ""
	s.pendingPastes = nil
	s.turnTokens = 0
	s.retryAttempt = 0
	s.retryMax = 0
	s.running = false
	s.toolStreamIdx = -1
	s.toolStreamID = ""
	s.toolTail = nil
	s.toolPartial = ""
	s.toolLineCount = 0
	s.toolLineCountByID = map[string]int{}
	s.shellOutputs = map[string]string{}
	s.shellExpanded = map[string]bool{}
	s.shellTranscriptIdx = map[string]int{}
}
