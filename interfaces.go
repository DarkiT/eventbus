package eventbus

import (
	"strings"
	"sync"
	"time"
)

// EventFilter 定义事件过滤器接口
type EventFilter interface {
	// Filter 返回 true 表示事件可以继续传递，false 表示拦截该事件
	Filter(topic string, payload any) bool
}

// IMiddleware 定义事件处理中间件接口
type IMiddleware interface {
	// Before 在事件处理前执行，可以修改payload
	Before(topic string, payload any) any
	// After 在事件处理后执行
	After(topic string, payload any)
}

// Subscription 定义带优先级的订阅信息。
// 为兼容历史版本保留该导出类型。
type Subscription struct {
	Handler  any
	Priority int
}

// FilterFunc 函数式过滤器适配器，便于直接使用函数
type FilterFunc func(topic string, payload any) bool

// Filter 实现 EventFilter 接口
func (f FilterFunc) Filter(topic string, payload any) bool {
	if f == nil {
		return true
	}
	return f(topic, payload)
}

// SmartFilter 提供基础限流与主题阻断能力
type SmartFilter struct {
	mu       sync.Mutex
	limits   map[string]int
	counters map[string]*smartCounter
	blocked  map[string]struct{}
	window   time.Duration
	stopCh   chan struct{}
	stopped  bool
}

type smartCounter struct {
	count       int
	windowStart time.Time
}

// NewSmartFilter 创建智能过滤器，默认限流窗口为 1 分钟
func NewSmartFilter() *SmartFilter {
	return &SmartFilter{
		limits:   make(map[string]int),
		counters: make(map[string]*smartCounter),
		blocked:  make(map[string]struct{}),
		window:   time.Minute,
		stopCh:   make(chan struct{}),
	}
}

// StartCleanup 启动后台清理协程，定期清理过期计数器。
//
// 注意：该协程独立于 EventBus 生命周期，bus.Close 不会自动停止它。
// SmartFilter 不再使用时必须显式调用 Stop 释放，否则会造成 goroutine 泄漏。
func (f *SmartFilter) StartCleanup(interval time.Duration) {
	if f == nil || interval <= 0 {
		return
	}
	f.mu.Lock()
	if f.stopped {
		f.mu.Unlock()
		return
	}
	f.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				f.cleanup()
			case <-f.stopCh:
				return
			}
		}
	}()
}

// cleanup 清理过期的计数器
func (f *SmartFilter) cleanup() {
	if f == nil {
		return
	}
	now := time.Now()
	f.mu.Lock()
	defer f.mu.Unlock()
	for topic, counter := range f.counters {
		if now.Sub(counter.windowStart) >= f.window {
			delete(f.counters, topic)
		}
	}
}

// Stop 停止后台清理协程
func (f *SmartFilter) Stop() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.stopped {
		f.stopped = true
		close(f.stopCh)
	}
}

// SetLimit 设置指定主题的限流次数（窗口内最大可通过次数）
func (f *SmartFilter) SetLimit(topic string, limit int) {
	if f == nil {
		return
	}
	normalized, err := normalizeTopic(topic)
	if err != nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if limit <= 0 {
		delete(f.limits, normalized)
		delete(f.counters, normalized)
		return
	}
	f.limits[normalized] = limit
}

// BlockTopic 阻断指定主题（含其子层级）
func (f *SmartFilter) BlockTopic(topic string) {
	if f == nil {
		return
	}
	normalized, err := normalizeTopic(topic)
	if err != nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blocked[normalized] = struct{}{}
}

// UnblockTopic 解除主题阻断
func (f *SmartFilter) UnblockTopic(topic string) {
	if f == nil {
		return
	}
	normalized, err := normalizeTopic(topic)
	if err != nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.blocked, normalized)
}

// SetWindow 调整限流窗口大小
func (f *SmartFilter) SetWindow(window time.Duration) {
	if f == nil {
		return
	}
	if window <= 0 {
		return
	}
	f.mu.Lock()
	f.window = window
	f.mu.Unlock()
}

// Filter 实现 EventFilter 接口
func (f *SmartFilter) Filter(topic string, payload any) bool {
	if f == nil {
		return true
	}
	normalized, err := normalizeTopic(topic)
	if err != nil {
		return true
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	for blocked := range f.blocked {
		if normalized == blocked || strings.HasPrefix(normalized, blocked+".") {
			return false
		}
	}

	if limit, ok := f.limits[normalized]; ok && limit > 0 {
		now := time.Now()
		counter := f.counters[normalized]
		if counter == nil || now.Sub(counter.windowStart) >= f.window {
			counter = &smartCounter{windowStart: now}
			f.counters[normalized] = counter
		}
		if counter.count >= limit {
			return false
		}
		counter.count++
	}

	return true
}

// Middleware 提供基础性能统计与可选的负载转换能力
type Middleware struct {
	mu          sync.Mutex
	stats       map[string]*TopicStat
	startQueues map[string][]time.Time
	transform   func(topic string, payload any) any
}

// TopicStat 记录主题处理统计信息
type TopicStat struct {
	Count     int
	TotalTime time.Duration
}

// NewMiddleware 创建增强中间件实例
func NewMiddleware() *Middleware {
	return &Middleware{
		stats:       make(map[string]*TopicStat),
		startQueues: make(map[string][]time.Time),
	}
}

// SetTransformer 设置负载转换函数（可选）
func (m *Middleware) SetTransformer(fn func(topic string, payload any) any) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.transform = fn
	m.mu.Unlock()
}

// Before 记录开始时间，可选地转换负载。
//
// 注意：耗时统计基于 per-topic 的 FIFO 队列配对。在多 goroutine 并发发布同一 topic
// 且投递耗时不一时，Before/After 的配对顺序可能交错，统计为近似值，适用于趋势观测
// 而非精确基准。如需精确计时，请在处理器内部自行埋点。
func (m *Middleware) Before(topic string, payload any) any {
	if m == nil {
		return payload
	}
	now := time.Now()
	m.mu.Lock()
	m.startQueues[topic] = append(m.startQueues[topic], now)
	transform := m.transform
	m.mu.Unlock()

	if transform != nil {
		return transform(topic, payload)
	}
	return payload
}

// After 记录耗时
func (m *Middleware) After(topic string, payload any) {
	if m == nil {
		return
	}
	now := time.Now()
	m.mu.Lock()
	starts := m.startQueues[topic]
	var start time.Time
	if len(starts) > 0 {
		start = starts[0]
		m.startQueues[topic] = starts[1:]
	} else {
		start = now
	}
	stat := m.stats[topic]
	if stat == nil {
		stat = &TopicStat{}
		m.stats[topic] = stat
	}
	stat.Count++
	stat.TotalTime += now.Sub(start)
	m.mu.Unlock()
}

// MiddlewareFunc 适配器，便于测试快速注入 Before/After
type MiddlewareFunc struct {
	BeforeFn func(topic string, payload any) any
	AfterFn  func(topic string, payload any)
}

func (m MiddlewareFunc) Before(topic string, payload any) any {
	if m.BeforeFn != nil {
		return m.BeforeFn(topic, payload)
	}
	return payload
}

func (m MiddlewareFunc) After(topic string, payload any) {
	if m.AfterFn != nil {
		m.AfterFn(topic, payload)
	}
}

// GetStats 获取性能统计快照
func (m *Middleware) GetStats() map[string]TopicStat {
	result := make(map[string]TopicStat)
	if m == nil {
		return result
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for topic, stat := range m.stats {
		result[topic] = *stat
	}
	return result
}

// Reset 清空统计信息
func (m *Middleware) Reset() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.stats = make(map[string]*TopicStat)
	m.startQueues = make(map[string][]time.Time)
	m.mu.Unlock()
}
