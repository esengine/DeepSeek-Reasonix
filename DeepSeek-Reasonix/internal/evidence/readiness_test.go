package evidence

import "testing"

func TestHasSuccessfulTodoWriteNilLedger(t *testing.T) {
	var l *Ledger
	if l.HasSuccessfulTodoWrite() {
		t.Fatal("nil ledger must report no todo_write")
	}
}

func TestHasSuccessfulTodoWriteMatchesOnlySuccessfulTodoWrite(t *testing.T) {
	cases := []struct {
		name    string
		receipt Receipt
		want    bool
	}{
		{"successful todo_write", Receipt{ToolName: "todo_write", Success: true}, true},
		{"failed todo_write does not count", Receipt{ToolName: "todo_write", Success: false}, false},
		{"successful non-todo tool does not count", Receipt{ToolName: "write_file", Success: true, Write: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := NewLedger()
			l.Record(tc.receipt)
			if got := l.HasSuccessfulTodoWrite(); got != tc.want {
				t.Fatalf("HasSuccessfulTodoWrite() = %v, want %v", got, tc.want)
			}
		})
	}
}
