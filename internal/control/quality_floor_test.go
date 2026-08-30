package control

import (
	"sync"
	"testing"

	"reasonix/internal/taskcontract"
)

func TestSetQualityFloorNormalizesVocabulary(t *testing.T) {
	c := New(Options{Label: "floor"})
	cases := map[string]string{
		"":           QualityFloorStandard,
		"standard":   QualityFloorStandard,
		"balanced":   QualityFloorStandard,
		"full":       QualityFloorStandard,
		"light":      QualityFloorStandard,
		"economy":    QualityFloorStandard,
		"eco":        QualityFloorStandard,
		"lite":       QualityFloorStandard,
		"minimal":    QualityFloorStandard,
		"delivery":   QualityFloorDelivery,
		"deliver":    QualityFloorDelivery,
		"quality":    QualityFloorDelivery,
		" DELIVERY ": QualityFloorDelivery,
	}
	for raw, want := range cases {
		if err := c.SetQualityFloor(raw); err != nil {
			t.Fatalf("SetQualityFloor(%q): %v", raw, err)
		}
		if got := c.QualityFloor(); got != want {
			t.Fatalf("SetQualityFloor(%q) = %q, want %q", raw, got, want)
		}
	}
	if err := c.SetQualityFloor(QualityFloorDelivery); err != nil {
		t.Fatalf("SetQualityFloor(delivery): %v", err)
	}
	if err := c.SetQualityFloor("turbo"); err == nil {
		t.Fatal("unknown floor value must error")
	}
	if got := c.QualityFloor(); got != QualityFloorDelivery {
		t.Fatalf("rejected value must not change state, got %q", got)
	}
}

func TestQualityFloorConstraintReachesTurnConstraints(t *testing.T) {
	c := New(Options{Label: "floor"})
	if got := c.qualityFloorConstraint(); got != taskcontract.PolicyFloorNone {
		t.Fatalf("default floor constraint = %v, want none", got)
	}
	if err := c.SetQualityFloor(QualityFloorDelivery); err != nil {
		t.Fatalf("SetQualityFloor: %v", err)
	}
	if got := c.qualityFloorConstraint(); got != taskcontract.PolicyFloorDelivery {
		t.Fatalf("floor constraint = %v, want delivery", got)
	}
}

func TestSetQualityFloorConcurrentWithReads(t *testing.T) {
	c := New(Options{Label: "floor"})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			floor := QualityFloorStandard
			if i%2 == 0 {
				floor = QualityFloorDelivery
			}
			_ = c.SetQualityFloor(floor)
		}(i)
		go func() {
			defer wg.Done()
			switch c.QualityFloor() {
			case QualityFloorStandard, QualityFloorDelivery:
			default:
				t.Errorf("QualityFloor returned invalid value %q", c.QualityFloor())
			}
			_ = c.qualityFloorConstraint()
		}()
	}
	wg.Wait()
}
