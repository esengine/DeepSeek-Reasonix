package agent

import "testing"

// OPT-246
func TestTAQE_SetQuotaAndEnforce(t *testing.T) {
	e := NewTokenAwareQuotaEnforcer()
	e.SetQuota("a", 1000)
	if !e.Enforce("a", 500) { t.Errorf("fail") }
	if e.GetUsage("a") != 500 { t.Errorf("usage") }
}

func TestTAQE_ExceedQuota(t *testing.T) {
	e := NewTokenAwareQuotaEnforcer()
	e.SetQuota("a", 1000)
	e.Enforce("a", 800)
	if e.Enforce("a", 300) { t.Errorf("should fail") }
	if e.GetViolationCount() != 1 { t.Errorf("violations") }
}

func TestTAQE_NoQuota(t *testing.T) {
	e := NewTokenAwareQuotaEnforcer()
	if !e.Enforce("x", 99) { t.Errorf("allow") }
}

func TestTAQE_Stats(t *testing.T) {
	e := NewTokenAwareQuotaEnforcer()
	e.SetQuota("a", 500)
	e.SetQuota("b", 1000)
	e.Enforce("a", 100)
	s := e.GetStats()
	if s["tenantCount"].(int) != 2 { t.Errorf("tc") }
}

func TestTAQE_Reset(t *testing.T) {
	e := NewTokenAwareQuotaEnforcer()
	e.SetQuota("a", 500)
	e.Enforce("a", 100)
	e.Reset()
	s := e.GetStats()
	if s["tenantCount"].(int) != 0 { t.Errorf("reset") }
}

// OPT-247
func TestCIW_StartAndAdd(t *testing.T) {
	w := NewCacheInvalidationWave()
	w.StartWave(1000)
	if !w.AddToWave("k1") { t.Errorf("fail") }
}

func TestCIW_PropagateWave(t *testing.T) {
	w := NewCacheInvalidationWave()
	w.StartWave(1000)
	w.AddToWave("a")
	w.AddToWave("b")
	k := w.PropagateWave()
	if len(k) != 2 { t.Errorf("len=%d", len(k)) }
}

func TestCIW_PropagateEmpty(t *testing.T) {
	w := NewCacheInvalidationWave()
	if w.PropagateWave() != nil { t.Errorf("not nil") }
}

func TestCIW_AddNoWave(t *testing.T) {
	w := NewCacheInvalidationWave()
	if w.AddToWave("k") { t.Errorf("should fail") }
}

func TestCIW_Stats(t *testing.T) {
	w := NewCacheInvalidationWave()
	w.StartWave(1000)
	w.AddToWave("a")
	w.AddToWave("b")
	w.PropagateWave()
	s := w.GetStats()
	if s["totalPropagated"].(int) != 2 { t.Errorf("tp") }
}

func TestCIW_Reset(t *testing.T) {
	w := NewCacheInvalidationWave()
	w.StartWave(1000)
	w.AddToWave("a")
	w.Reset()
	s := w.GetStats()
	if s["totalPropagated"].(int) != 0 { t.Errorf("reset") }
}

// OPT-248
func TestCWTR_HighTempShrinks(t *testing.T) {
	r := NewContextWindowThermalRegulator(1024, 8192, 8192)
	r.SetTemperature(1.0)
	if r.Regulate() != 1024 { t.Errorf("high temp") }
}

func TestCWTR_LowTempExpands(t *testing.T) {
	r := NewContextWindowThermalRegulator(1024, 8192, 1024)
	r.SetTemperature(0.0)
	if r.Regulate() != 8192 { t.Errorf("low temp") }
}

func TestCWTR_TempClamped(t *testing.T) {
	r := NewContextWindowThermalRegulator(1024, 8192, 4096)
	r.SetTemperature(-0.5)
	if r.GetTemperature() != 0.0 { t.Errorf("clamp low") }
	r.SetTemperature(2.0)
	if r.GetTemperature() != 1.0 { t.Errorf("clamp high") }
}

func TestCWTR_GetWindowSize(t *testing.T) {
	r := NewContextWindowThermalRegulator(1024, 8192, 4096)
	if r.GetWindowSize() != 4096 { t.Errorf("size") }
}

func TestCWTR_Stats(t *testing.T) {
	r := NewContextWindowThermalRegulator(1024, 8192, 4096)
	r.SetTemperature(1.0)
	r.Regulate()
	s := r.GetStats()
	if s["adjustments"].(int) != 1 { t.Errorf("adj") }
}

func TestCWTR_Reset(t *testing.T) {
	r := NewContextWindowThermalRegulator(1024, 8192, 4096)
	r.SetTemperature(1.0)
	r.Regulate()
	r.Reset()
	s := r.GetStats()
	if s["adjustments"].(int) != 0 { t.Errorf("reset") }
}

// OPT-249
func TestTACM_RegisterAndCheck(t *testing.T) {
	m := NewTokenAwareCircuitMonitor()
	m.Register("c1")
	if m.Check("c1") != "closed" { t.Errorf("closed") }
}

func TestTACM_CheckUnknown(t *testing.T) {
	m := NewTokenAwareCircuitMonitor()
	if m.Check("x") != "unknown" { t.Errorf("unknown") }
}

func TestTACM_SetState(t *testing.T) {
	m := NewTokenAwareCircuitMonitor()
	m.Register("c1")
	m.SetState("c1", "open")
	if m.GetState("c1") != "open" { t.Errorf("open") }
}

func TestTACM_OpenAlerts(t *testing.T) {
	m := NewTokenAwareCircuitMonitor()
	m.Register("c1")
	m.SetState("c1", "open")
	m.Check("c1")
	m.Check("c1")
	s := m.GetStats()
	if s["alerts"].(int) != 2 { t.Errorf("alerts") }
}

func TestTACM_Stats(t *testing.T) {
	m := NewTokenAwareCircuitMonitor()
	m.Register("c1")
	m.Register("c2")
	m.SetState("c2", "open")
	s := m.GetStats()
	if s["circuitCount"].(int) != 2 { t.Errorf("cc") }
	if s["openCircuits"].(int) != 1 { t.Errorf("oc") }
}

func TestTACM_Reset(t *testing.T) {
	m := NewTokenAwareCircuitMonitor()
	m.Register("c1")
	m.Check("c1")
	m.Reset()
	s := m.GetStats()
	if s["circuitCount"].(int) != 0 { t.Errorf("reset") }
}

// OPT-250
func TestPCAC_RecordPerformance(t *testing.T) {
	c := NewPromptCacheAdaptiveController(10)
	c.RecordPerformance(0.9)
	s := c.GetStats()
	if s["lastHitRate"].(float64) != 0.9 { t.Errorf("hr") }
}

func TestPCAC_AdaptHigh(t *testing.T) {
	c := NewPromptCacheAdaptiveController(10)
	c.RecordPerformance(0.9)
	c.RecordPerformance(0.85)
	if c.Adapt() != "aggressive" { t.Errorf("aggressive") }
}

func TestPCAC_AdaptLow(t *testing.T) {
	c := NewPromptCacheAdaptiveController(10)
	c.RecordPerformance(0.05)
	c.RecordPerformance(0.1)
	if c.Adapt() != "minimal" { t.Errorf("minimal") }
}

func TestPCAC_GetStrategy(t *testing.T) {
	c := NewPromptCacheAdaptiveController(10)
	if c.GetStrategy() != "default" { t.Errorf("default") }
}

func TestPCAC_IsAdaptive(t *testing.T) {
	c := NewPromptCacheAdaptiveController(10)
	if !c.IsAdaptive() { t.Errorf("adaptive") }
}

func TestPCAC_StatsAndReset(t *testing.T) {
	c := NewPromptCacheAdaptiveController(10)
	c.RecordPerformance(0.9)
	c.RecordPerformance(0.85)
	c.Adapt()
	s := c.GetStats()
	if s["adjustments"].(int) != 1 { t.Errorf("adj") }
	c.Reset()
	s = c.GetStats()
	if s["strategy"].(string) != "default" { t.Errorf("reset") }
}
