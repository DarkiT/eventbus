package eventbus

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Handler 定义了一个函数类型，用于处理 T 类型的消息
type Handler[T any] func(payload T)

// PipeResponseHandler 定义了一个函数类型，用于处理 T 类型的消息并返回响应
type PipeResponseHandler[T any] func(payload T) (any, error)

// PipeResponseHandlerWithContext 定义了一个带 Context 的响应处理器
type PipeResponseHandlerWithContext[T any] func(ctx context.Context, payload T) (any, error)

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

// PipeBatchResult 批量发布结果
type PipeBatchResult struct {
	Count        int
	SuccessCount int
	FailedCount  int
	FirstError   error
}

// PipeBatchError 批量发布错误
type PipeBatchError struct {
	Result *PipeBatchResult
}

func (e PipeBatchError) Error() string {
	if e.Result == nil {
		return "pipe batch publish failed"
	}
	return fmt.Sprintf("pipe batch publish failed: %d items failed", e.Result.FailedCount)
}

// HandlerWithPriority 带优先级的处理器
type HandlerWithPriority[T any] struct {
	Handler                    Handler[T]
	ResponseHandler            PipeResponseHandler[T]
	ResponseHandlerWithContext PipeResponseHandlerWithContext[T]
	Priority                   int
	ID                         uintptr
	CustomID                   string // 自定义 ID，用于闭包去重
	expectsContext             bool
}

// SubscribeOption 订阅选项
type SubscribeOption func(*subscribeOptions)

// subscribeOptions 订阅配置
type subscribeOptions struct {
	priority int
	customID string
}

// WithPriority 设置处理器优先级
func WithPriority(priority int) SubscribeOption {
	return func(o *subscribeOptions) {
		o.priority = priority
	}
}

// WithHandlerID 设置自定义处理器 ID（用于闭包去重）
func WithHandlerID(id string) SubscribeOption {
	return func(o *subscribeOptions) {
		o.customID = id
	}
}

// Pipe 优化后的泛型管道
type Pipe[T any] struct {
	sync.RWMutex
	bufferSize       int
	channel          chan T
	handlers         []*HandlerWithPriority[T] // 使用切片支持优先级
	handlerMap       map[uintptr]*HandlerWithPriority[T]
	customIDMap      map[string]*HandlerWithPriority[T] // 自定义 ID 映射，用于闭包去重
	responseHandlers []*HandlerWithPriority[T]          // 响应式处理器
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
		customIDMap:      make(map[string]*HandlerWithPriority[T]),
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

// SubscribeOnce 一次性订阅，处理器仅执行一次后自动退订
func (p *Pipe[T]) SubscribeOnce(handler Handler[T]) error {
	return p.SubscribeOnceWithPriority(handler, 0)
}

// SubscribeOnceWithPriority 带优先级的一次性订阅
// 使用 atomic.Bool 确保并发场景下只执行一次，并在首次执行后自动退订
func (p *Pipe[T]) SubscribeOnceWithPriority(handler Handler[T], priority int) error {
	if p.closed.Load() {
		return ErrChannelClosed
	}

	if handler == nil {
		return ErrHandlerIsNotFunc
	}

	var done atomic.Bool
	var wrapped func(T)
	wrapped = func(payload T) {
		if !done.CompareAndSwap(false, true) {
			return
		}
		defer func() { _ = p.Unsubscribe(wrapped) }()
		handler(payload)
	}

	return p.SubscribeWithPriority(wrapped, priority)
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
		expectsContext:  false,
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

// SubscribeWithResponseContext 向管道添加带 Context 的响应式处理器。
//
// 注意：本方法不返回取消句柄，创建的响应式处理器只能通过 Close 整个 Pipe 退订。
// 如需精细控制单个处理器生命周期，请改用 SubscribeWithResponseContextHandle，
// 它返回 PipeResponseCancel 用于独立退订，避免长生命周期 Pipe 中的订阅泄漏。
func (p *Pipe[T]) SubscribeWithResponseContext(handler PipeResponseHandlerWithContext[T]) error {
	return p.SubscribeWithResponseContextAndPriority(handler, 0)
}

// SubscribeWithResponseContextHandle 向管道添加带 Context 的响应式处理器，并返回可取消句柄。
func (p *Pipe[T]) SubscribeWithResponseContextHandle(handler PipeResponseHandlerWithContext[T]) (PipeResponseCancel, error) {
	return p.SubscribeWithResponseContextHandleAndPriority(handler, 0)
}

// SubscribeWithResponseContextHandleAndPriority 向管道添加带 Context 的响应式处理器并返回可取消句柄。
func (p *Pipe[T]) SubscribeWithResponseContextHandleAndPriority(handler PipeResponseHandlerWithContext[T], priority int) (PipeResponseCancel, error) {
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

	handlerInfo := &HandlerWithPriority[T]{
		ResponseHandlerWithContext: handler,
		Priority:                   priority,
		ID:                         handlerID,
		expectsContext:             true,
	}

	p.Lock()
	defer p.Unlock()

	p.handlerMap[handlerID] = handlerInfo
	p.responseHandlers = append(p.responseHandlers, handlerInfo)
	slices.SortFunc(p.responseHandlers, func(a, b *HandlerWithPriority[T]) int {
		return b.Priority - a.Priority
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

// SubscribeWithResponseContextAndPriority 向管道添加带 Context 的响应式处理器并指定优先级
func (p *Pipe[T]) SubscribeWithResponseContextAndPriority(handler PipeResponseHandlerWithContext[T], priority int) error {
	_, err := p.SubscribeWithResponseContextHandleAndPriority(handler, priority)
	return err
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

// SubscribeWithOptions 使用选项订阅处理器
// 支持自定义 ID（用于闭包去重）和优先级
func (p *Pipe[T]) SubscribeWithOptions(handler Handler[T], opts ...SubscribeOption) error {
	if p.closed.Load() {
		return ErrChannelClosed
	}

	if handler == nil {
		return ErrHandlerIsNotFunc
	}

	// 应用选项
	options := &subscribeOptions{}
	for _, opt := range opts {
		opt(options)
	}

	handlerID, err := functionPointer(handler)
	if err != nil {
		return err
	}

	handlerInfo := &HandlerWithPriority[T]{
		Handler:  handler,
		Priority: options.priority,
		ID:       handlerID,
		CustomID: options.customID,
	}

	p.Lock()
	defer p.Unlock()

	// 如果有自定义 ID，检查是否已存在
	if options.customID != "" {
		if _, exists := p.customIDMap[options.customID]; exists {
			return ErrDuplicateHandler
		}
		p.customIDMap[options.customID] = handlerInfo
	}

	// 添加到映射表
	p.handlerMap[handlerID] = handlerInfo

	// 添加到切片并按优先级排序
	p.handlers = append(p.handlers, handlerInfo)
	slices.SortFunc(p.handlers, func(a, b *HandlerWithPriority[T]) int {
		return b.Priority - a.Priority // 降序排列
	})

	return nil
}

// UnsubscribeByID 通过自定义 ID 移除处理器
func (p *Pipe[T]) UnsubscribeByID(customID string) error {
	if p.closed.Load() {
		return ErrChannelClosed
	}

	if customID == "" {
		return ErrInvalidTopic
	}

	p.Lock()
	defer p.Unlock()

	handlerInfo, exists := p.customIDMap[customID]
	if !exists {
		return ErrNoSubscriber
	}

	// 从自定义 ID 映射中删除
	delete(p.customIDMap, customID)

	// 从函数指针映射中删除
	delete(p.handlerMap, handlerInfo.ID)

	// 从切片中删除
	p.handlers = slices.DeleteFunc(p.handlers, func(h *HandlerWithPriority[T]) bool {
		return h.ID == handlerInfo.ID
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

// Publish 异步发送消息给所有订阅者。
//
// 注意："异步"仅指处理器在独立 goroutine 中执行；无缓冲或队列已满且消费者较慢时，
// Publish 仍会阻塞调用方直至消息入队、超时（受 NewPipeWithTimeout 控制）或管道关闭，
// 即背压会传递给生产者。
func (p *Pipe[T]) Publish(payload T) error {
	if p.closed.Load() {
		return ErrChannelClosed
	}

	p.RLock()
	ch := p.channel
	timeout := p.timeout
	stopCh := p.stopCh
	p.RUnlock()

	// 使用 NewTimer 并在退出时 Stop，与 PublishBatch 实现保持一致，
	// 避免依赖 time.After 的延迟回收语义。
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case ch <- payload:
		return nil
	case <-stopCh:
		return ErrChannelClosed
	case <-timer.C:
		return ErrPublishTimeout
	}
}

// PublishWithContext 带上下文的异步发送
func (p *Pipe[T]) PublishWithContext(ctx context.Context, payload T) error {
	if p.closed.Load() {
		return ErrChannelClosed
	}
	if ctx == nil {
		ctx = context.Background()
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

// PublishBatch 异步批量发送
func (p *Pipe[T]) PublishBatch(payloads []T) (*PipeBatchResult, error) {
	if p.closed.Load() {
		return nil, ErrChannelClosed
	}
	if len(payloads) == 0 {
		return &PipeBatchResult{}, nil
	}

	p.RLock()
	ch := p.channel
	timeout := p.timeout
	stopCh := p.stopCh
	p.RUnlock()

	var firstErr error
	success := 0

	for _, payload := range payloads {
		timer := time.NewTimer(timeout)
		select {
		case ch <- payload:
			timer.Stop()
			success++
		case <-stopCh:
			timer.Stop()
			if firstErr == nil {
				firstErr = ErrChannelClosed
			}
		case <-timer.C:
			if firstErr == nil {
				firstErr = ErrPublishTimeout
			}
		}
	}

	res := &PipeBatchResult{
		Count:        len(payloads),
		SuccessCount: success,
		FailedCount:  len(payloads) - success,
		FirstError:   firstErr,
	}

	if res.FailedCount > 0 {
		return res, PipeBatchError{Result: res}
	}
	return res, nil
}

// PublishBatchSync 同步批量发送
func (p *Pipe[T]) PublishBatchSync(payloads []T) (*PipeBatchResult, error) {
	if p.closed.Load() {
		return nil, ErrChannelClosed
	}
	if len(payloads) == 0 {
		return &PipeBatchResult{}, nil
	}

	var firstErr error
	success := 0

	for _, payload := range payloads {
		if err := p.PublishSync(payload); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		} else {
			success++
		}
	}

	res := &PipeBatchResult{
		Count:        len(payloads),
		SuccessCount: success,
		FailedCount:  len(payloads) - success,
		FirstError:   firstErr,
	}
	if res.FailedCount > 0 {
		return res, PipeBatchError{Result: res}
	}
	return res, nil
}

// publishSyncWithStrategy 响应式同步发布的核心方法
func (p *Pipe[T]) publishSyncWithStrategy(ctx context.Context, payload T, strategy string) (*PipeSyncResult, error) {
	if p.closed.Load() {
		return nil, ErrChannelClosed
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	startTime := time.Now()
	internalTimeoutApplied := false

	var deadlineCancel context.CancelFunc
	if p.timeout > 0 {
		pipeDeadline := time.Now().Add(p.timeout)
		if d, ok := ctx.Deadline(); ok {
			if pipeDeadline.Before(d) {
				ctx, deadlineCancel = context.WithDeadlineCause(ctx, pipeDeadline, context.DeadlineExceeded)
				internalTimeoutApplied = true
			}
		} else {
			ctx, deadlineCancel = context.WithDeadlineCause(ctx, pipeDeadline, context.DeadlineExceeded)
			internalTimeoutApplied = true
		}
	}
	if deadlineCancel == nil {
		deadlineCancel = func() {}
	}

	deadlineCtx := ctx
	runCtx, cancel := context.WithCancelCause(deadlineCtx)
	defer func() {
		cancel(context.Canceled)
		deadlineCancel()
	}()

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
	var resultMu sync.Mutex
	var successCounter atomic.Int32
	successSignal := make(chan struct{}, 1)

	for i, handlerInfo := range responseHandlers {
		results[i].HandlerID = fmt.Sprintf("handler_%d", handlerInfo.ID)
	}

	// 并发执行所有响应式处理器
	for i, handlerInfo := range responseHandlers {
		wg.Add(1)
		go func(index int, handler *HandlerWithPriority[T]) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					resultMu.Lock()
					results[index] = PipeHandlerResult{
						HandlerID: results[index].HandlerID,
						Success:   false,
						Result:    nil,
						Error:     fmt.Errorf("panic recovered: %v\nstack: %s", r, debug.Stack()),
						Duration:  0,
					}
					resultMu.Unlock()
				}
			}()

			handlerStart := time.Now()
			var (
				result any
				err    error
			)

			if handler.expectsContext {
				result, err = handler.ResponseHandlerWithContext(runCtx, payload)
			} else {
				result, err = handler.ResponseHandler(payload)
			}
			duration := time.Since(handlerStart)

			success := err == nil
			resultMu.Lock()
			results[index] = PipeHandlerResult{
				HandlerID: results[index].HandlerID,
				Success:   success,
				Result:    result,
				Error:     err,
				Duration:  duration,
			}
			resultMu.Unlock()

			if success {
				successCounter.Add(1)
				select {
				case successSignal <- struct{}{}:
				default:
				}
				if strategy == "any" {
					cancel(errPublishAnySatisfied)
				}
			}
		}(i, handlerInfo)
	}

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	buildSnapshot := func() *PipeSyncResult {
		resultMu.Lock()
		snapshot := append([]PipeHandlerResult(nil), results...)
		resultMu.Unlock()

		success := 0
		failure := 0
		for _, res := range snapshot {
			switch {
			case res.Success:
				success++
			case res.HandlerID != "":
				failure++
			}
		}

		overallSuccess := failure == 0
		if strategy == "any" {
			overallSuccess = success > 0
		}

		return &PipeSyncResult{
			Success:      overallSuccess,
			HandlerCount: len(responseHandlers),
			SuccessCount: success,
			FailureCount: len(responseHandlers) - success,
			Results:      snapshot,
			TotalTime:    time.Since(startTime),
		}
	}

	select {
	case <-doneCh:
	case <-runCtx.Done():
		cause := context.Cause(runCtx)
		switch {
		case errors.Is(cause, errPublishAnySatisfied):
			select {
			case <-doneCh:
			case <-deadlineCtx.Done():
				return buildSnapshot(), nil
			}
		case errors.Is(cause, context.DeadlineExceeded):
			if internalTimeoutApplied {
				return buildSnapshot(), newPublishTimeoutError(startTime)
			}
			return buildSnapshot(), cause
		default:
			if strategy == "any" {
				if successCounter.Load() > 0 {
					return buildSnapshot(), nil
				}

				grace := p.timeout
				if grace <= 0 || grace > 20*time.Millisecond {
					grace = 20 * time.Millisecond
				}

				timer := time.NewTimer(grace)
				select {
				case <-successSignal:
					timer.Stop()
					return buildSnapshot(), nil
				case <-doneCh:
					timer.Stop()
					return buildSnapshot(), nil
				case <-timer.C:
				}

				if successCounter.Load() > 0 {
					return buildSnapshot(), nil
				}
			}
			return buildSnapshot(), cause
		}
	}

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

	// 上下文取消：all 策略直接返回错误；any 策略在没有成功结果时返回错误
	if errCtx := runCtx.Err(); errCtx != nil {
		if strategy == "all" || successCount == 0 {
			return &PipeSyncResult{
				Success:      false,
				HandlerCount: len(responseHandlers),
				SuccessCount: successCount,
				FailureCount: len(responseHandlers) - successCount,
				Results:      results,
				TotalTime:    time.Since(startTime),
			}, errCtx
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
	return p.publishSyncWithStrategy(context.Background(), payload, "all")
}

// PublishSyncAny 响应式同步发布，只要有一个处理器成功即可
func (p *Pipe[T]) PublishSyncAny(payload T) (*PipeSyncResult, error) {
	return p.publishSyncWithStrategy(context.Background(), payload, "any")
}

// PublishSyncAllWithContext 响应式同步发布，要求所有处理器都成功，支持上下文
func (p *Pipe[T]) PublishSyncAllWithContext(ctx context.Context, payload T) (map[string]any, error) {
	result, err := p.publishSyncWithStrategy(ctx, payload, "all")
	responseMap := make(map[string]any)
	if result != nil {
		for _, res := range result.Results {
			responseMap[res.HandlerID] = res.Result
		}
	}
	return responseMap, err
}

// PublishSyncAllResultWithContext 响应式同步发布，返回完整结果，适合需要统计与诊断信息的调用方。
func (p *Pipe[T]) PublishSyncAllResultWithContext(ctx context.Context, payload T) (*PipeSyncResult, error) {
	return p.publishSyncWithStrategy(ctx, payload, "all")
}

// PublishSyncAnyWithContext 响应式同步发布，只要有一个处理器成功即可，支持上下文
func (p *Pipe[T]) PublishSyncAnyWithContext(ctx context.Context, payload T) (any, error) {
	result, err := p.publishSyncWithStrategy(ctx, payload, "any")
	if err != nil {
		return nil, err
	}

	for _, res := range result.Results {
		if res.Success {
			return res.Result, nil
		}
	}

	return nil, ErrNoSubscriber
}

// PublishSyncAnyResultWithContext 响应式同步发布，返回完整结果，适合需要所有处理器状态的调用方。
func (p *Pipe[T]) PublishSyncAnyResultWithContext(ctx context.Context, payload T) (*PipeSyncResult, error) {
	return p.publishSyncWithStrategy(ctx, payload, "any")
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
func (p *Pipe[T]) GetStats() map[string]any {
	p.RLock()
	defer p.RUnlock()

	return map[string]any{
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
