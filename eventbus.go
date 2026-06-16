package eventbus

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultTimeout 默认超时时间
	DefaultTimeout = 5 * time.Second
	// slowConsumerThreshold 慢消费者阈值
	slowConsumerThreshold = 100 * time.Millisecond
	// shutdownPollInterval Shutdown 排空判定时的队列轮询间隔
	shutdownPollInterval = 5 * time.Millisecond
)

// defaultBufferSize 默认缓冲区大小，根据CPU核心数动态设置
var defaultBufferSize = runtime.NumCPU() * 64

// validateTopic 校验主题是否合法（非空）
func validateTopic(topic string) error {
	if strings.TrimSpace(topic) == "" {
		return ErrInvalidTopic
	}
	return nil
}

// HandlerInfo 处理器信息，支持优先级
type HandlerInfo struct {
	fn             reflect.Value
	handlerFn      func(topic string, payload any)
	handlerCtxFn   func(ctx context.Context, topic string, payload any)
	responseFn     func(topic string, payload any) (any, error)
	responseCtxFn  func(ctx context.Context, topic string, payload any) (any, error)
	reliableFn     func(ctx context.Context, topic string, payload any) error
	retryConfig    *retryConfig
	filter         EventFilter
	Priority       int
	ID             uintptr // 用于唯一标识处理器
	HandlerID      string  // 预计算的处理器标识，避免热路径格式化分配
	Sequence       uint64  // 订阅顺序，用于同优先级下保持稳定顺序
	IsResponse     bool    // 标识是否为响应处理器
	expectsContext bool    // 标识处理器是否期望 context
}

type tracerHolder struct {
	tracer EventTracer
}

// messageEnvelope 封装通道中的消息，附带上下文信息
type messageEnvelope struct {
	ctx     context.Context
	topic   string
	payload any
}

// applyFiltersAndMiddlewares 应用过滤器与中间件，返回转换后的 payload、是否允许、After 回调及错误
func applyFiltersAndMiddlewares(filters []EventFilter, middlewares []IMiddleware, topic string, payload any) (any, bool, func() error, error) {
	for _, filter := range filters {
		allowed, filterErr := safeFilterInvoke(filter, topic, payload)
		if filterErr != nil {
			return payload, false, nil, filterErr
		}
		if !allowed {
			return payload, false, nil, nil
		}
	}

	if len(middlewares) == 0 {
		return payload, true, func() error { return nil }, nil
	}

	applied := 0
	for _, mw := range middlewares {
		nextPayload, err := safeMiddlewareBefore(mw, topic, payload)
		if err != nil {
			return payload, false, nil, err
		}
		payload = nextPayload
		applied++
	}

	after := func() error {
		return runMiddlewareAfter(middlewares[:applied], topic, payload)
	}
	return payload, true, after, nil
}

// EventBus 优化后的事件总线
type EventBus struct {
	bufferSize          int
	channels            *cowMap
	topicIndex          *topicTrie   // Trie 树索引，用于高效通配符匹配
	timeout             atomic.Int64 // 以纳秒存储超时，保证并发安全
	subscriptionSeq     atomic.Uint64
	filtersSnapshot     atomic.Value // 存储 []EventFilter 快照，COW 优化
	middlewaresSnapshot atomic.Value // 存储 []IMiddleware 快照，COW 优化
	tracer              atomic.Value // 存储 EventTracer，避免读写竞态
	filtersMu           sync.Mutex   // 保护 filters 写操作
	middlewaresMu       sync.Mutex   // 保护 middlewares 写操作
	closed              atomic.Bool
	draining            atomic.Bool  // Shutdown 期间置位，拒绝新发布以允许队列排空
	activePublishes     atomic.Int64 // 已通过接收检查、尚未完成入队/同步分发的发布调用
}

// New 创建事件总线
// 可选参数：bufferSize - 缓冲区大小，不传或传0表示无缓冲，负数使用默认缓冲大小
func New(bufferSize ...int) *EventBus {
	size := -1 // 默认无缓冲
	if len(bufferSize) > 0 {
		if bufferSize[0] > 0 {
			size = bufferSize[0]
		} else if bufferSize[0] == 0 {
			size = -1 // 显式指定无缓冲
		} else {
			size = defaultBufferSize // 负数使用默认缓冲大小
		}
	}

	bus := &EventBus{
		bufferSize: size,
		channels:   newCowMap(),
		topicIndex: newTopicTrie(),
	}
	// 初始化 COW 快照为空切片
	bus.filtersSnapshot.Store([]EventFilter{})
	bus.middlewaresSnapshot.Store([]IMiddleware{})
	bus.timeout.Store(int64(DefaultTimeout))
	return bus
}

// getTracer 获取当前追踪器的快照（无锁，避免竞态）
func (e *EventBus) getTracer() EventTracer {
	if e == nil {
		return nil
	}
	if v := e.tracer.Load(); v != nil {
		if holder, ok := v.(tracerHolder); ok {
			return holder.tracer
		}
	}
	return nil
}

// checkClosed 统一的关闭状态检查
func (e *EventBus) checkClosed() error {
	if e == nil {
		return ErrEventBusClosed
	}
	if e.closed.Load() {
		return ErrEventBusClosed
	}
	return nil
}

// checkAccepting 在发布路径上检查总线是否仍接收新消息：
// 已关闭返回 ErrEventBusClosed，正在 Shutdown 排空返回 ErrShuttingDown。
func (e *EventBus) checkAccepting() error {
	if err := e.checkClosed(); err != nil {
		return err
	}
	if e.draining.Load() {
		return ErrShuttingDown
	}
	return nil
}

// beginPublish 标记一次已通过关闭检查的发布调用正在进行。
//
// Shutdown 会先置 draining，再等待 activePublishes 归零，以避免刚通过接收检查、
// 尚未入队的消息在关闭边界上被静默丢弃。
func (e *EventBus) beginPublish() error {
	if err := e.checkClosed(); err != nil {
		return err
	}
	e.activePublishes.Add(1)
	if err := e.checkAccepting(); err != nil {
		e.activePublishes.Add(-1)
		return err
	}
	return nil
}

func (e *EventBus) endPublish() {
	if e != nil {
		e.activePublishes.Add(-1)
	}
}

// traceError 封装错误追踪，避免重复的 nil 检查
func (e *EventBus) traceError(topic string, err error) {
	if err == nil {
		return
	}
	if tracer := e.getTracer(); tracer != nil {
		tracer.OnError(topic, err)
	}
}

// traceComplete 封装完成追踪
func (e *EventBus) traceComplete(topic string, metadata CompleteMetadata) {
	if tracer := e.getTracer(); tracer != nil {
		tracer.OnComplete(topic, metadata)
	}
}

// traceSlowConsumer 封装慢消费者追踪，自动兼容响应式 tracer
func (e *EventBus) traceSlowConsumer(topic string, latency time.Duration) {
	if tracer := e.getTracer(); tracer != nil {
		if resp, ok := tracer.(ResponseAwareTracer); ok {
			resp.OnSlowConsumerResponse(topic, latency)
			return
		}
		tracer.OnSlowConsumer(topic, latency)
	}
}

// traceSubscribe 封装订阅追踪
func (e *EventBus) traceSubscribe(topic string, handler any) {
	if tracer := e.getTracer(); tracer != nil {
		tracer.OnSubscribe(topic, handler)
	}
}

// traceUnsubscribe 封装退订追踪
func (e *EventBus) traceUnsubscribe(topic string, handler any) {
	if tracer := e.getTracer(); tracer != nil {
		tracer.OnUnsubscribe(topic, handler)
	}
}

// traceQueueFull 封装队列满追踪
func (e *EventBus) traceQueueFull(topic string, size int) {
	if tracer := e.getTracer(); tracer != nil {
		tracer.OnQueueFull(topic, size)
	}
}

// tracePublish 封装发布追踪
func (e *EventBus) tracePublish(topic string, payload any, metadata PublishMetadata) {
	if tracer := e.getTracer(); tracer != nil {
		tracer.OnPublish(topic, payload, metadata)
	}
}

// getTimeout 获取当前超时设置
func (e *EventBus) getTimeout() time.Duration {
	if e == nil {
		return DefaultTimeout
	}
	if v := e.timeout.Load(); v > 0 {
		return time.Duration(v)
	}
	return DefaultTimeout
}

// SetTimeout 动态调整总线级超时时间（仅正数生效）
func (e *EventBus) SetTimeout(timeout time.Duration) {
	if e == nil {
		return
	}
	if timeout <= 0 {
		return
	}
	e.timeout.Store(int64(timeout))
}

// nextSequence 返回全局单调递增的订阅序号，用于稳定排序。
func (e *EventBus) nextSequence() uint64 {
	if e == nil {
		return 0
	}
	return e.subscriptionSeq.Add(1)
}

// AddFilter 添加过滤器（COW 模式，写时复制）
func (e *EventBus) AddFilter(filter EventFilter) {
	if e == nil || filter == nil {
		return
	}
	e.filtersMu.Lock()
	defer e.filtersMu.Unlock()

	// 获取当前快照
	current := e.getFilters()

	// 创建新切片并存储
	newFilters := make([]EventFilter, len(current)+1)
	copy(newFilters, current)
	newFilters[len(current)] = filter
	e.filtersSnapshot.Store(newFilters)
}

// SetTracer 设置追踪器
func (e *EventBus) SetTracer(tracer EventTracer) {
	if e == nil {
		return
	}
	e.tracer.Store(tracerHolder{tracer: tracer})
}

// Use 添加中间件（COW 模式，写时复制）
func (e *EventBus) Use(middleware IMiddleware) {
	if e == nil || middleware == nil {
		return
	}
	e.middlewaresMu.Lock()
	defer e.middlewaresMu.Unlock()

	// 获取当前快照
	current := e.getMiddlewares()

	// 创建新切片并存储
	newMiddlewares := make([]IMiddleware, len(current)+1)
	copy(newMiddlewares, current)
	newMiddlewares[len(current)] = middleware
	e.middlewaresSnapshot.Store(newMiddlewares)
}

// getFilters 获取过滤器快照（无锁读取）
func (e *EventBus) getFilters() []EventFilter {
	if v := e.filtersSnapshot.Load(); v != nil {
		return v.([]EventFilter)
	}
	return nil
}

// getMiddlewares 获取中间件快照（无锁读取）
func (e *EventBus) getMiddlewares() []IMiddleware {
	if v := e.middlewaresSnapshot.Load(); v != nil {
		return v.([]IMiddleware)
	}
	return nil
}
