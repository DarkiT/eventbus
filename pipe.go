package eventbus

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Handler 定义了一个函数类型，用于处理 T 类型的消息
type Handler[T any] func(payload T)

// PipeResponseHandler 定义了一个函数类型，用于处理 T 类型的消息并返回响应
type PipeResponseHandler[T any] func(payload T) (any, error)

// PipeResponseCancel 用于撤销响应式处理器订阅
type PipeResponseCancel func()

// PipeHandlerResult 处理器执行结果
type PipeHandlerResult struct {
	HandlerID string        `json:"handler_id"` // 处理器唯一标识
	Success   bool          `json:"success"`    // 执行是否成功
	Result    any           `json:"result"`     // 返回值
	Error     error         `json:"error"`      // 错误信息
	Duration  time.Duration `json:"duration"`   // 执行耗时
}

// PipeSyncResult 同步发布结果
type PipeSyncResult struct {
	Success      bool                `json:"success"`       // 整体是否成功
	HandlerCount int                 `json:"handler_count"` // 处理器总数
	SuccessCount int                 `json:"success_count"` // 成功数量
	FailureCount int                 `json:"failure_count"` // 失败数量
	Results      []PipeHandlerResult `json:"results"`       // 详细结果
	TotalTime    time.Duration       `json:"total_time"`    // 总耗时
}

// HandlerWithPriority 带优先级的处理器
type HandlerWithPriority[T any] struct {
	Handler         Handler[T]
	ResponseHandler PipeResponseHandler[T]
	Priority        int
	ID              uintptr
}

// Pipe 优化后的泛型管道
type Pipe[T any] struct {
	sync.RWMutex
	bufferSize       int
	channel          chan T
	handlers         []*HandlerWithPriority[T] // 使用切片支持优先级
	handlerMap       map[uintptr]*HandlerWithPriority[T]
	responseHandlers []*HandlerWithPriority[T] // 响应式处理器
	closed           atomic.Bool
	stopCh           chan struct{}
	timeout          time.Duration
	ctx              context.Context
	cancel           context.CancelFunc
}

// NewPipe 创建一个无缓冲的管道
func NewPipe[T any]() *Pipe[T] {
	return newPipe[T](-1, DefaultTimeout)
}

// NewBufferedPipe 创建一个带指定缓冲区大小的管道
func NewBufferedPipe[T any](bufferSize int) *Pipe[T] {
	return newPipe[T](bufferSize, DefaultTimeout)
}

// NewPipeWithTimeout 创建一个无缓冲管道并自定义超时
func NewPipeWithTimeout[T any](timeout time.Duration) *Pipe[T] {
	return newPipe[T](-1, timeout)
}

// NewBufferedPipeWithTimeout 创建一个带缓冲的管道并自定义超时
func NewBufferedPipeWithTimeout[T any](bufferSize int, timeout time.Duration) *Pipe[T] {
	return newPipe[T](bufferSize, timeout)
}

func newPipe[T any](bufferSize int, timeout time.Duration) *Pipe[T] {
	var ch chan T
	if bufferSize <= 0 {
		ch = make(chan T)
		bufferSize = 0
	} else {
		ch = make(chan T, bufferSize)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	p := &Pipe[T]{
		bufferSize:       bufferSize,
		channel:          ch,
		handlers:         make([]*HandlerWithPriority[T], 0),
		handlerMap:       make(map[uintptr]*HandlerWithPriority[T]),
		responseHandlers: make([]*HandlerWithPriority[T], 0),
		stopCh:           make(chan struct{}),
		timeout:          timeout,
		ctx:              ctx,
		cancel:           cancel,
	}

	go p.loop()
	return p
}

func (p *Pipe[T]) loop() {
	defer func() {
		_ = recover()
	}()

	for {
		select {
		case payload, ok := <-p.channel:
			if !ok {
				return
			}
			p.transfer(payload)
		case <-p.ctx.Done():
			return
		case <-p.stopCh:
			return
		}
	}
}

// transfer 按优先级顺序传递消息
func (p *Pipe[T]) transfer(payload T) {
	p.RLock()
	handlers := slices.Clone(p.handlers)
	p.RUnlock()

	// 按优先级顺序执行处理器
	for _, handlerInfo := range handlers {
		func() {
			defer func() {
				_ = recover()
			}()
			if handlerInfo.Handler != nil {
				handlerInfo.Handler(payload)
			}
		}()
	}
}

// Subscribe 向管道添加一个处理器
func (p *Pipe[T]) Subscribe(handler Handler[T]) error {
	return p.SubscribeWithPriority(handler, 0)
}

// SubscribeWithResponse 向管道添加一个响应式处理器
func (p *Pipe[T]) SubscribeWithResponse(handler PipeResponseHandler[T]) (PipeResponseCancel, error) {
	return p.SubscribeWithResponseAndPriority(handler, 0)
}

// SubscribeWithResponseAndPriority 向管道添加一个带优先级的响应式处理器
func (p *Pipe[T]) SubscribeWithResponseAndPriority(handler PipeResponseHandler[T], priority int) (PipeResponseCancel, error) {
	if p.closed.Load() {
		return nil, ErrChannelClosed
	}

	if handler == nil {
		return nil, ErrHandlerIsNotFunc
	}

	handlerID, err := functionPointer(handler)
	if err != nil {
		return nil, err
	}

	// 使用结构体地址生成唯一ID
	handlerInfo := &HandlerWithPriority[T]{
		ResponseHandler: handler,
		Priority:        priority,
		ID:              handlerID,
	}

	p.Lock()
	defer p.Unlock()

	// 添加到映射表
	p.handlerMap[handlerID] = handlerInfo

	// 添加到响应式处理器切片并按优先级排序
	p.responseHandlers = append(p.responseHandlers, handlerInfo)
	slices.SortFunc(p.responseHandlers, func(a, b *HandlerWithPriority[T]) int {
		return b.Priority - a.Priority // 降序排列
	})

	return func() {
		p.Lock()
		defer p.Unlock()
		if p.closed.Load() {
			return
		}
		delete(p.handlerMap, handlerID)
		p.responseHandlers = slices.DeleteFunc(p.responseHandlers, func(h *HandlerWithPriority[T]) bool {
			return h.ID == handlerID
		})
	}, nil
}

// SubscribeWithPriority 向管道添加一个带优先级的处理器
func (p *Pipe[T]) SubscribeWithPriority(handler Handler[T], priority int) error {
	if p.closed.Load() {
		return ErrChannelClosed
	}

	if handler == nil {
		return ErrHandlerIsNotFunc
	}

	handlerID, err := functionPointer(handler)
	if err != nil {
		return err
	}

	handlerInfo := &HandlerWithPriority[T]{
		Handler:  handler,
		Priority: priority,
		ID:       handlerID,
	}

	p.Lock()
	defer p.Unlock()

	// 添加到映射表
	p.handlerMap[handlerID] = handlerInfo

	// 添加到切片并按优先级排序
	p.handlers = append(p.handlers, handlerInfo)
	slices.SortFunc(p.handlers, func(a, b *HandlerWithPriority[T]) int {
		return b.Priority - a.Priority // 降序排列
	})

	return nil
}

// Unsubscribe 从管道中移除一个处理器
func (p *Pipe[T]) Unsubscribe(handler Handler[T]) error {
	if p.closed.Load() {
		return ErrChannelClosed
	}

	handlerID, err := functionPointer(handler)
	if err != nil {
		return err
	}

	p.Lock()
	defer p.Unlock()

	// 从映射表中删除
	if _, exists := p.handlerMap[handlerID]; !exists {
		return ErrNoSubscriber
	}
	delete(p.handlerMap, handlerID)

	// 从切片中删除
	p.handlers = slices.DeleteFunc(p.handlers, func(h *HandlerWithPriority[T]) bool {
		return h.ID == handlerID
	})

	return nil
}

// Publish 异步发送消息给所有订阅者
func (p *Pipe[T]) Publish(payload T) error {
	if p.closed.Load() {
		return ErrChannelClosed
	}

	p.RLock()
	ch := p.channel
	timeout := p.timeout
	stopCh := p.stopCh
	p.RUnlock()

	select {
	case ch <- payload:
		return nil
	case <-stopCh:
		return ErrChannelClosed
	case <-time.After(timeout):
		return ErrPublishTimeout
	}
}

// PublishWithContext 带上下文的异步发送
func (p *Pipe[T]) PublishWithContext(ctx context.Context, payload T) error {
	if p.closed.Load() {
		return ErrChannelClosed
	}

	p.RLock()
	ch := p.channel
	stopCh := p.stopCh
	p.RUnlock()

	select {
	case ch <- payload:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-stopCh:
		return ErrChannelClosed
	}
}

// PublishSync 同步发送消息给所有订阅者
func (p *Pipe[T]) PublishSync(payload T) error {
	if p.closed.Load() {
		return ErrChannelClosed
	}

	p.transfer(payload)
	return nil
}

// publishSyncWithStrategy 响应式同步发布的核心方法
func (p *Pipe[T]) publishSyncWithStrategy(payload T, strategy string) (*PipeSyncResult, error) {
	if p.closed.Load() {
		return nil, ErrChannelClosed
	}

	startTime := time.Now()

	p.RLock()
	responseHandlers := slices.Clone(p.responseHandlers)
	p.RUnlock()

	if len(responseHandlers) == 0 {
		return &PipeSyncResult{
			Success:      true,
			HandlerCount: 0,
			SuccessCount: 0,
			FailureCount: 0,
			Results:      []PipeHandlerResult{},
			TotalTime:    time.Since(startTime),
		}, nil
	}

	results := make([]PipeHandlerResult, len(responseHandlers))
	var wg sync.WaitGroup

	// 并发执行所有响应式处理器
	for i, handlerInfo := range responseHandlers {
		wg.Add(1)
		go func(index int, handler *HandlerWithPriority[T]) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					results[index] = PipeHandlerResult{
						HandlerID: fmt.Sprintf("handler_%d", handler.ID),
						Success:   false,
						Result:    nil,
						Error:     fmt.Errorf("panic recovered: %v", r),
						Duration:  0,
					}
				}
			}()

			handlerStart := time.Now()
			result, err := handler.ResponseHandler(payload)
			duration := time.Since(handlerStart)

			results[index] = PipeHandlerResult{
				HandlerID: fmt.Sprintf("handler_%d", handler.ID),
				Success:   err == nil,
				Result:    result,
				Error:     err,
				Duration:  duration,
			}
		}(i, handlerInfo)
	}

	wg.Wait()

	// 统计结果
	successCount := 0
	failureCount := 0
	for _, result := range results {
		if result.Success {
			successCount++
		} else {
			failureCount++
		}
	}

	// 根据策略确定整体成功状态
	var overallSuccess bool
	switch strategy {
	case "all":
		overallSuccess = failureCount == 0
	case "any":
		overallSuccess = successCount > 0
	default:
		overallSuccess = failureCount == 0
	}

	return &PipeSyncResult{
		Success:      overallSuccess,
		HandlerCount: len(responseHandlers),
		SuccessCount: successCount,
		FailureCount: failureCount,
		Results:      results,
		TotalTime:    time.Since(startTime),
	}, nil
}

// PublishSyncAll 响应式同步发布，要求所有处理器都成功
func (p *Pipe[T]) PublishSyncAll(payload T) (*PipeSyncResult, error) {
	return p.publishSyncWithStrategy(payload, "all")
}

// PublishSyncAny 响应式同步发布，只要有一个处理器成功即可
func (p *Pipe[T]) PublishSyncAny(payload T) (*PipeSyncResult, error) {
	return p.publishSyncWithStrategy(payload, "any")
}

// Close 关闭管道
func (p *Pipe[T]) Close() {
	if !p.closed.CompareAndSwap(false, true) {
		return // 已经关闭
	}

	p.cancel() // 取消上下文
	close(p.stopCh)

	p.Lock()
	p.handlers = nil
	p.handlerMap = nil
	p.responseHandlers = nil
	p.Unlock()
}

// GetStats 获取管道统计信息
func (p *Pipe[T]) GetStats() map[string]interface{} {
	p.RLock()
	defer p.RUnlock()

	return map[string]interface{}{
		"handler_count":          len(p.handlers),
		"response_handler_count": len(p.responseHandlers),
		"buffer_size":            p.bufferSize,
		"queue_length":           len(p.channel),
		"closed":                 p.closed.Load(),
		"timeout":                p.timeout,
	}
}

// functionPointer 获取函数的唯一指针，用于订阅去重
func functionPointer(fn any) (uintptr, error) {
	if fn == nil {
		return 0, ErrHandlerIsNotFunc
	}
	val := reflect.ValueOf(fn)
	if !val.IsValid() || val.Kind() != reflect.Func {
		return 0, ErrHandlerIsNotFunc
	}
	if val.IsNil() {
		return 0, ErrHandlerIsNotFunc
	}
	return val.Pointer(), nil
}
