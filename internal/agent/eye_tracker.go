package agent

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ── 眼控/注视追踪系统 ──
//
// 技术定位：
//   - Tobii Eye Tracker 5: 144Hz, <0.6°精度, C/C++ SDK
//   - GazeTracking: MIT开源, webcam方案, 2-4°精度, C++可移植
//   - Windows Eye Control API: 系统内置辅助功能
//   - 降级方案: 鼠标悬停模拟注视（无硬件时）
//
// 核心设计：
//   1. 注视 >1.5s 触发意图聚焦 → 自动切换对话到源Tab
//   2. 注视热点记录 → 识别代码审核中被忽略的区域
//   3. 注视路径回放 → 审核证据链
//   4. 无硬件时降级为鼠标悬停（保持功能可用）

// ═══════════════════════════════════════════════════════════════
//  眼控数据结构
// ═══════════════════════════════════════════════════════════════

// GazePoint 注视点
type GazePoint struct {
	X         float64   // 屏幕X坐标（像素）
	Y         float64   // 屏幕Y坐标（像素）
	Timestamp time.Time // 时间戳
	// 注视置信度（0-1）
	Confidence float64
	// 瞳孔直径（mm，用于注意力分析）
	PupilDiameter float64
}

// GazeFixation 注视固定（持续注视同一区域）
type GazeFixation struct {
	Center    GazePoint     // 注视中心点
	StartTime time.Time     // 开始时间
	EndTime   time.Time     // 结束时间
	Duration  time.Duration // 持续时间
	// 注视范围内的区域信息
	TabID    string // 命中的Tab ID
	RegionID string // 命中的投影区域ID
	CodeLine int    // 命中的代码行号
	// 注意力指标
	Stability float64 // 注视稳定性（0-1，越低说明眼球越不稳定）
}

// GazeHotspot 注视热点（一段时间内的注视聚集区域）
type GazeHotspot struct {
	CenterX       float64       // 中心X
	CenterY       float64       // 中心Y
	Radius        float64       // 热点半径
	VisitCount    int           // 访问次数
	TotalDuration time.Duration // 总注视时间
	// 关联信息
	TabID     string // 关联Tab
	CodeRange string // 关联代码范围
	// 审核标记
	Reviewed bool   // 是否被审核过
	Ignored  bool   // 是否被忽略（注视时间不足）
	Severity string // 审核严重性
}

// GazePath 注视路径（一次审核会话的完整注视轨迹）
type GazePath struct {
	ID        string
	StartTime time.Time
	EndTime   time.Time
	Fixations []GazeFixation
	Hotspots  []GazeHotspot
	// 统计
	TotalFixations  int
	TotalDuration   time.Duration
	AvgFixationTime time.Duration
	// 审核覆盖率
	LinesReviewed   int     // 审核过的代码行数
	TotalLines      int     // 总代码行数
	CoveragePercent float64 // 审核覆盖率
}

// ═══════════════════════════════════════════════════════════════
//  GazeMapper — 屏幕坐标到Tab/代码区域映射
// ═══════════════════════════════════════════════════════════════

// ScreenRegion 屏幕区域映射
type ScreenRegion struct {
	Bounds   RegionBounds // 区域边界
	TabID    string       // 关联Tab
	RegionID string       // 关联投影区域
	CodeLine int          // 关联代码行号（-1=非代码区域）
	Type     ScreenRegionType
}

// ScreenRegionType 屏幕区域类型
type ScreenRegionType int

const (
	ScreenRegionMain       ScreenRegionType = iota // 主内容区域
	ScreenRegionProjection                         // 虚空投影区域
	ScreenRegionTabBar                             // 标签栏
	ScreenRegionStatusBar                          // 状态栏
	ScreenRegionCodeBlock                          // 代码块
	ScreenRegionDiff                               // 差异视图
	ScreenRegionEmpty                              // 空白区域
)

// GazeMapper 注视映射器
// 将屏幕像素坐标映射到Tab/投影区域/代码行号
type GazeMapper struct {
	mu      sync.RWMutex
	regions []ScreenRegion
	// 字符尺寸（用于像素↔字符坐标转换）
	charWidth  int // 每字符宽度（像素）
	charHeight int // 每字符高度（像素）
}

// NewGazeMapper 创建注视映射器
func NewGazeMapper(charW, charH int) *GazeMapper {
	if charW <= 0 {
		charW = 8
	}
	if charH <= 0 {
		charH = 16
	}
	return &GazeMapper{
		charWidth:  charW,
		charHeight: charH,
	}
}

// UpdateRegions 更新屏幕区域映射（每次渲染后调用）
func (gm *GazeMapper) UpdateRegions(regions []*ProjectionRegion, termW, termH int) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	gm.regions = gm.regions[:0] // 清空

	// 添加主内容区域
	gm.regions = append(gm.regions, ScreenRegion{
		Bounds: RegionBounds{X: 0, Y: 1, Width: termW, Height: termH - len(regions)*6 - 2},
		Type:   ScreenRegionMain,
	})

	// 添加投影区域
	for _, r := range regions {
		gm.regions = append(gm.regions, ScreenRegion{
			Bounds:   r.Bounds,
			TabID:    r.SourceTabID,
			RegionID: r.ID,
			Type:     ScreenRegionProjection,
		})
	}

	// 添加标签栏
	gm.regions = append(gm.regions, ScreenRegion{
		Bounds: RegionBounds{X: 0, Y: 0, Width: termW, Height: 1},
		Type:   ScreenRegionTabBar,
	})

	// 添加状态栏
	gm.regions = append(gm.regions, ScreenRegion{
		Bounds: RegionBounds{X: 0, Y: termH - 1, Width: termW, Height: 1},
		Type:   ScreenRegionStatusBar,
	})
}

// MapPoint 将屏幕像素坐标映射到区域
func (gm *GazeMapper) MapPoint(pixelX, pixelY float64) *ScreenRegion {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	// 像素 → 字符坐标
	charX := int(pixelX / float64(gm.charWidth))
	charY := int(pixelY / float64(gm.charHeight))

	for i := range gm.regions {
		if gm.regions[i].Bounds.Contains(charX, charY) {
			return &gm.regions[i]
		}
	}

	return &ScreenRegion{Type: ScreenRegionEmpty}
}

// MapToCodeLine 映射到代码行号
func (gm *GazeMapper) MapToCodeLine(pixelY float64, scrollOffset int) int {
	charY := int(pixelY / float64(gm.charHeight))
	// 减去标签栏(1行) + scrollOffset
	codeLine := charY - 1 + scrollOffset
	if codeLine < 0 {
		codeLine = 0
	}
	return codeLine
}

// SetCharSize 设置字符尺寸
func (gm *GazeMapper) SetCharSize(w, h int) {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	gm.charWidth = w
	gm.charHeight = h
}

// ═══════════════════════════════════════════════════════════════
//  EyeTracker — 眼控追踪器（多后端支持）
// ═══════════════════════════════════════════════════════════════

// TrackerBackend 追踪后端类型
type TrackerBackend int

const (
	BackendNone    TrackerBackend = iota // 无眼控硬件
	BackendTobii                         // Tobii Eye Tracker
	BackendWebcam                        // Webcam方案(GazeTracking)
	BackendWindows                       // Windows Eye Control API
	BackendMouse                         // 鼠标悬停降级方案
)

// String 返回后端名称
func (b TrackerBackend) String() string {
	switch b {
	case BackendTobii:
		return "Tobii"
	case BackendWebcam:
		return "Webcam"
	case BackendWindows:
		return "WindowsEyeControl"
	case BackendMouse:
		return "MouseFallback"
	default:
		return "None"
	}
}

// TrackerConfig 追踪器配置
type TrackerConfig struct {
	Backend           TrackerBackend
	SampleRate        int           // 采样率(Hz)
	FixationThreshold time.Duration // 注视阈值（超过此时间认为是注视）
	GazeTimeout       time.Duration // 注视超时（触发切换）
	ConfidenceMin     float64       // 最低置信度
	// Tobii特定
	TobiiLicenseKey string
	// Webcam特定
	WebcamDeviceID  int
	WebcamModelPath string
}

// DefaultTrackerConfig 默认配置
func DefaultTrackerConfig() TrackerConfig {
	return TrackerConfig{
		Backend:           BackendMouse, // 默认降级为鼠标
		SampleRate:        60,
		FixationThreshold: 500 * time.Millisecond,
		GazeTimeout:       1500 * time.Millisecond,
		ConfidenceMin:     0.5,
	}
}

// EyeTracker 眼控追踪器
type EyeTracker struct {
	mu     sync.Mutex
	config TrackerConfig
	mapper *GazeMapper
	// 当前注视状态
	currentFixation *GazeFixation
	fixationStart   time.Time
	fixationPoints  []GazePoint
	// 注视热点（最近5分钟）
	hotspots []GazeHotspot
	// 当前路径
	currentPath *GazePath
	// 数据通道
	gazeCh chan GazePoint
	fixCh  chan GazeFixation
	stopCh chan struct{}
	// 统计
	totalSamples   atomic.Int64
	totalFixations atomic.Int64
	// 运行状态
	running atomic.Bool
}

// NewEyeTracker 创建眼控追踪器
func NewEyeTracker(config TrackerConfig, mapper *GazeMapper) *EyeTracker {
	return &EyeTracker{
		config: config,
		mapper: mapper,
		gazeCh: make(chan GazePoint, 256),
		fixCh:  make(chan GazeFixation, 64),
		stopCh: make(chan struct{}),
	}
}

// Start 启动追踪
func (et *EyeTracker) Start() error {
	if !et.running.CompareAndSwap(false, true) {
		return fmt.Errorf("tracker already running")
	}

	// 根据后端类型初始化
	switch et.config.Backend {
	case BackendTobii:
		// Tobii SDK 初始化（C绑定）
		// 实际实现需要 cgo 调用 Tobii SDK
		// et.initTobii()
	case BackendWebcam:
		// GazeTracking 初始化
		// 实际实现需要调用 OpenCV + GazeTracking 模型
		// et.initWebcam()
	case BackendWindows:
		// Windows Eye Control API
		// 实际实现通过 Windows ETW 或 UI Automation
	case BackendMouse:
		// 鼠标降级方案 — 无需初始化
	default:
		et.running.Store(false)
		return fmt.Errorf("unsupported backend: %s", et.config.Backend)
	}

	// 启动注视检测循环
	go et.fixationLoop()

	// 开始新的注视路径
	et.currentPath = &GazePath{
		ID:        fmt.Sprintf("path_%d", time.Now().UnixMilli()),
		StartTime: time.Now(),
	}

	return nil
}

// Stop 停止追踪
func (et *EyeTracker) Stop() {
	if !et.running.CompareAndSwap(true, false) {
		return
	}
	close(et.stopCh)

	// 结束当前路径
	if et.currentPath != nil {
		et.currentPath.EndTime = time.Now()
		et.finalizePath()
	}
}

// FeedPoint 喂入注视点数据（由后端驱动调用）
func (et *EyeTracker) FeedPoint(point GazePoint) {
	if !et.running.Load() {
		return
	}
	if point.Confidence < et.config.ConfidenceMin {
		return
	}

	et.totalSamples.Add(1)

	select {
	case et.gazeCh <- point:
	default:
		// channel满，丢弃最旧的
		select {
		case <-et.gazeCh:
		default:
		}
		et.gazeCh <- point
	}
}

// FeedMousePosition 鼠标降级方案：喂入鼠标位置
func (et *EyeTracker) FeedMousePosition(x, y float64) {
	et.FeedPoint(GazePoint{
		X:         x,
		Y:         y,
		Timestamp: time.Now(),
		// 鼠标位置置信度为1（精确）
		Confidence: 1.0,
	})
}

// fixationLoop 注视检测循环
// 将连续的注视点聚合为注视固定（fixation）
func (et *EyeTracker) fixationLoop() {
	var lastPoint GazePoint
	var fixationPoints []GazePoint
	fixationStart := time.Time{}

	for {
		select {
		case point := <-et.gazeCh:
			if fixationStart.IsZero() {
				// 开始新的注视
				fixationStart = point.Timestamp
				fixationPoints = []GazePoint{point}
			} else {
				// 检查是否仍在同一区域（容差50像素）
				dx := point.X - lastPoint.X
				dy := point.Y - lastPoint.Y
				dist := dx*dx + dy*dy

				if dist < 2500 { // 50^2 = 2500
					// 仍在同一区域，累积注视点
					fixationPoints = append(fixationPoints, point)
				} else {
					// 离开区域，检查是否构成注视
					duration := lastPoint.Timestamp.Sub(fixationStart)
					if duration >= et.config.FixationThreshold {
						fixation := et.createFixation(fixationPoints, fixationStart, lastPoint.Timestamp)
						et.processFixation(fixation)
					}
					// 开始新注视
					fixationStart = point.Timestamp
					fixationPoints = []GazePoint{point}
				}
			}
			lastPoint = point

		case <-et.stopCh:
			// 处理最后的注视
			if len(fixationPoints) > 0 {
				duration := lastPoint.Timestamp.Sub(fixationStart)
				if duration >= et.config.FixationThreshold {
					fixation := et.createFixation(fixationPoints, fixationStart, lastPoint.Timestamp)
					et.processFixation(fixation)
				}
			}
			return
		}
	}
}

// createFixation 从注视点列表创建注视固定
func (et *EyeTracker) createFixation(points []GazePoint, start, end time.Time) GazeFixation {
	// 计算注视中心
	var sumX, sumY float64
	var totalConf float64
	for _, p := range points {
		sumX += p.X * p.Confidence
		sumY += p.Y * p.Confidence
		totalConf += p.Confidence
	}

	centerX := sumX / totalConf
	centerY := sumY / totalConf

	// 计算稳定性（注视点的离散程度）
	var variance float64
	for _, p := range points {
		dx := p.X - centerX
		dy := p.Y - centerY
		variance += dx*dx + dy*dy
	}
	variance /= float64(len(points))
	stability := 1.0 / (1.0 + variance/10000.0) // 归一化

	// 映射到区域
	region := et.mapper.MapPoint(centerX, centerY)

	fixation := GazeFixation{
		Center: GazePoint{
			X:          centerX,
			Y:          centerY,
			Timestamp:  start,
			Confidence: totalConf / float64(len(points)),
		},
		StartTime: start,
		EndTime:   end,
		Duration:  end.Sub(start),
		TabID:     region.TabID,
		RegionID:  region.RegionID,
		Stability: stability,
	}

	return fixation
}

// processFixation 处理注视固定
func (et *EyeTracker) processFixation(fixation GazeFixation) {
	et.totalFixations.Add(1)

	// 发送到注视通道
	select {
	case et.fixCh <- fixation:
	default:
	}

	// 更新热点
	et.updateHotspot(fixation)

	// 添加到当前路径
	if et.currentPath != nil {
		et.currentPath.Fixations = append(et.currentPath.Fixations, fixation)
		et.currentPath.TotalFixations++
		et.currentPath.TotalDuration += fixation.Duration
	}
}

// updateHotspot 更新注视热点
func (et *EyeTracker) updateHotspot(fixation GazeFixation) {
	et.mu.Lock()
	defer et.mu.Unlock()

	// 查找附近的热点
	for i := range et.hotspots {
		dx := et.hotspots[i].CenterX - fixation.Center.X
		dy := et.hotspots[i].CenterY - fixation.Center.Y
		if dx*dx+dy*dy < 10000 { // 100像素内
			// 合并到现有热点
			et.hotspots[i].VisitCount++
			et.hotspots[i].TotalDuration += fixation.Duration
			// 更新中心（加权平均）
			w := float64(et.hotspots[i].VisitCount)
			et.hotspots[i].CenterX = (et.hotspots[i].CenterX*w + fixation.Center.X) / (w + 1)
			et.hotspots[i].CenterY = (et.hotspots[i].CenterY*w + fixation.Center.Y) / (w + 1)
			// 标记审核状态
			if fixation.Duration >= et.config.GazeTimeout {
				et.hotspots[i].Reviewed = true
				et.hotspots[i].Ignored = false
			} else if fixation.Duration < et.config.FixationThreshold {
				et.hotspots[i].Ignored = true
			}
			return
		}
	}

	// 创建新热点
	hotspot := GazeHotspot{
		CenterX:       fixation.Center.X,
		CenterY:       fixation.Center.Y,
		Radius:        50,
		VisitCount:    1,
		TotalDuration: fixation.Duration,
		TabID:         fixation.TabID,
		Reviewed:      fixation.Duration >= et.config.GazeTimeout,
		Ignored:       fixation.Duration < et.config.FixationThreshold,
	}
	et.hotspots = append(et.hotspots, hotspot)

	// 限制热点数量
	if len(et.hotspots) > 200 {
		et.hotspots = et.hotspots[100:]
	}
}

// finalizePath 完成注视路径
func (et *EyeTracker) finalizePath() {
	if et.currentPath == nil {
		return
	}

	// 计算平均注视时间
	if et.currentPath.TotalFixations > 0 {
		et.currentPath.AvgFixationTime = time.Duration(int64(et.currentPath.TotalDuration) / int64(et.currentPath.TotalFixations))
	}

	// 计算覆盖率
	if et.currentPath.TotalLines > 0 {
		et.currentPath.CoveragePercent = float64(et.currentPath.LinesReviewed) / float64(et.currentPath.TotalLines) * 100
	}

	// 复制热点到路径
	et.mu.Lock()
	et.currentPath.Hotspots = make([]GazeHotspot, len(et.hotspots))
	copy(et.currentPath.Hotspots, et.hotspots)
	et.mu.Unlock()
}

// GetFixationChannel 获取注视通道
func (et *EyeTracker) GetFixationChannel() <-chan GazeFixation {
	return et.fixCh
}

// GetHotspots 获取当前热点
func (et *EyeTracker) GetHotspots() []GazeHotspot {
	et.mu.Lock()
	defer et.mu.Unlock()
	result := make([]GazeHotspot, len(et.hotspots))
	copy(result, et.hotspots)
	return result
}

// GetIgnoredHotspots 获取被忽略的热点（审核盲区）
func (et *EyeTracker) GetIgnoredHotspots() []GazeHotspot {
	et.mu.Lock()
	defer et.mu.Unlock()
	var result []GazeHotspot
	for _, h := range et.hotspots {
		if h.Ignored {
			result = append(result, h)
		}
	}
	return result
}

// GetCurrentPath 获取当前注视路径
func (et *EyeTracker) GetCurrentPath() *GazePath {
	et.mu.Lock()
	defer et.mu.Unlock()
	return et.currentPath
}

// GetStats 获取统计
func (et *EyeTracker) GetStats() EyeTrackerStats {
	return EyeTrackerStats{
		Backend:        et.config.Backend.String(),
		SampleRate:     et.config.SampleRate,
		TotalSamples:   et.totalSamples.Load(),
		TotalFixations: et.totalFixations.Load(),
		HotspotCount:   len(et.hotspots),
		IgnoredCount:   len(et.GetIgnoredHotspots()),
		Running:        et.running.Load(),
	}
}

// EyeTrackerStats 眼控统计
type EyeTrackerStats struct {
	Backend        string `json:"backend"`
	SampleRate     int    `json:"sample_rate"`
	TotalSamples   int64  `json:"total_samples"`
	TotalFixations int64  `json:"total_fixations"`
	HotspotCount   int    `json:"hotspot_count"`
	IgnoredCount   int    `json:"ignored_count"`
	Running        bool   `json:"running"`
}

// ═══════════════════════════════════════════════════════════════
//  GazeIntegrator — 将眼控集成到虚空UI
// ═══════════════════════════════════════════════════════════════

// GazeIntegrator 眼控集成器
// 将眼控追踪与投影引擎和对话转移引擎连接
type GazeIntegrator struct {
	tracker   *EyeTracker
	mapper    *GazeMapper
	phantomUI *PhantomUI
	// 注视超时定时器
	gazeTimers map[string]*time.Timer // tabID → timer
	// 配置
	gazeThreshold time.Duration // 注视触发阈值
	mu            sync.Mutex
	stopCh        chan struct{}
}

// NewGazeIntegrator 创建眼控集成器
func NewGazeIntegrator(tracker *EyeTracker, mapper *GazeMapper, pui *PhantomUI) *GazeIntegrator {
	return &GazeIntegrator{
		tracker:       tracker,
		mapper:        mapper,
		phantomUI:     pui,
		gazeTimers:    make(map[string]*time.Timer),
		gazeThreshold: 1500 * time.Millisecond,
		stopCh:        make(chan struct{}),
	}
}

// Start 启动眼控集成
func (gi *GazeIntegrator) Start() {
	go gi.integrationLoop()
}

// Stop 停止眼控集成
func (gi *GazeIntegrator) Stop() {
	close(gi.stopCh)
}

// integrationLoop 集成循环
func (gi *GazeIntegrator) integrationLoop() {
	fixCh := gi.tracker.GetFixationChannel()

	for {
		select {
		case <-gi.stopCh:
			return
		case fixation := <-fixCh:
			// 处理注视固定
			if fixation.Duration >= gi.gazeThreshold {
				// 注视超过阈值 → 触发对话转移
				if fixation.TabID != "" && fixation.RegionID != "" {
					gi.phantomUI.HandleGaze(fixation.TabID, fixation.Duration.Milliseconds())
				}
			}

			// 更新热点审核状态
			gi.updateReviewStatus(fixation)
		}
	}
}

// updateReviewStatus 更新审核状态
func (gi *GazeIntegrator) updateReviewStatus(fixation GazeFixation) {
	gi.mu.Lock()
	defer gi.mu.Unlock()

	// 如果注视了代码区域且时间足够，标记为已审核
	if fixation.Duration >= gi.gazeThreshold {
		// 这里可以触发 ReviewGate 的审核标记
		// 实际实现需要与 ReviewGate 联动
	}
}

// UpdateRegions 更新区域映射（在虚空UI渲染后调用）
func (gi *GazeIntegrator) UpdateRegions() {
	regions := gi.phantomUI.projectionEngine.GetRegions()
	gi.mapper.UpdateRegions(regions, gi.phantomUI.renderer.width, gi.phantomUI.renderer.height)
}

// GetReviewBlindSpots 获取审核盲区（被忽略的代码区域）
func (gi *GazeIntegrator) GetReviewBlindSpots() []GazeHotspot {
	return gi.tracker.GetIgnoredHotspots()
}

// GetReviewCoverage 获取审核覆盖率
func (gi *GazeIntegrator) GetReviewCoverage() float64 {
	path := gi.tracker.GetCurrentPath()
	if path == nil || path.TotalLines == 0 {
		return 0
	}
	return path.CoveragePercent
}
