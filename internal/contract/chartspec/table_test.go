package chartspec

import (
	"reflect"
	"strings"
	"testing"
)

func TestTableFormatsEveryCellAsText(t *testing.T) {
	s, err := Parse([]byte(okSpec))
	if err != nil {
		t.Fatal(err)
	}
	tb := s.Table(10)
	if want := []string{"month", "sales"}; !reflect.DeepEqual(tb.Header, want) {
		t.Fatalf("header = %v", tb.Header)
	}
	want := [][]string{{"jan", "10"}, {"feb", ""}, {"mar", "30.5"}}
	if !reflect.DeepEqual(tb.Rows, want) {
		t.Fatalf("rows = %v, want %v", tb.Rows, want)
	}
	if !reflect.DeepEqual(tb.Numeric, []bool{false, true}) || tb.More != 0 {
		t.Fatalf("numeric = %v more = %d", tb.Numeric, tb.More)
	}
}

func TestTableCapsRowsAndCountsTheRest(t *testing.T) {
	rows := make([]string, 25)
	for i := range rows {
		rows[i] = `["r",1]`
	}
	raw := strings.Replace(okSpec, `[["jan",10],["feb",null],["mar",30.5]]`, "["+strings.Join(rows, ",")+"]", 1)
	s, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	tb := s.Table(10)
	if len(tb.Rows) != 10 || tb.More != 15 {
		t.Fatalf("rows = %d more = %d", len(tb.Rows), tb.More)
	}
	if all := s.Table(0); len(all.Rows) != 25 || all.More != 0 {
		t.Fatalf("an unlimited table must carry every row: %d/%d", len(all.Rows), all.More)
	}
}

func TestTableIsStable(t *testing.T) {
	s, _ := Parse([]byte(okSpec))
	if !reflect.DeepEqual(s.Table(5), s.Table(5)) {
		t.Fatal("the same spec must give the same table")
	}
}

func TestDescribeNamesTheMarksAndTheirColumns(t *testing.T) {
	s, _ := Parse([]byte(okSpec))
	if got, want := s.Describe(), "bar month × sales"; got != want {
		t.Fatalf("describe = %q, want %q", got, want)
	}
	raw := strings.Replace(okSpec, `"marks":[{"type":"bar","x":"month","y":["sales"]}]`, `"marks":[{"type":"line","x":"month","y":["sales"]},{"type":"pie","x":"month","y":["sales"],"donut":true}]`, 1)
	s, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := s.Describe(), "line month × sales; pie month × sales"; got != want {
		t.Fatalf("describe = %q, want %q", got, want)
	}
}
