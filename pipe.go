package eventbus

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// Handler 定义了一个函数类型，用于处理 T 类型的消息
type Handler[T any] func(payload T)

// HandlerWithPriority 带优先级的处理器
type HandlerWithPriority[T any] struct {
	Handler  Handler[T]
	Priority int
	ID       uintptr
}

// Pipe 优化后的泛型管道
type Pipe[T any] struct {
	sync.RWMutex
	bufferSize int
	channel    chan T
	handlers   []*HandlerWithPriority[T] // 使用切片支持优先级
	handlerMap map[uintptr]*HandlerWithPriority[T]
	closed     atomic.Bool
	stopCh     chan struct{}
	timeout    time.Duration
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewPipe 创建一个无缓冲的管道
func NewPipe[T any]() *Pipe[T] {
	return newPipe[T](-1)
}

// NewBufferedPipe 创建一个带指定缓冲区大小的管道
func NewBufferedPipe[T any](bufferSize int) *Pipe[T] {
	return newPipe[T](bufferSize)
}

func newPipe[T any](bufferSize int) *Pipe[T] {
	var ch chan T
	if bufferSize <= 0 {
		ch = make(chan T, 1)
	} else {
		ch = make(chan T, bufferSize)
	}

	ctx, cancel := context.WithCancel(context.Background())

	p := &Pipe[T]{
		bufferSize: bufferSize,
		channel:    ch,
		handlers:   make([]*HandlerWithPriority[T], 0),
		handlerMap: make(map[uintptr]*HandlerWithPriority[T]),
		stopCh:     make(chan struct{}),
		timeout:    DefaultTimeout,
		ctx:        ctx,
		cancel:     cancel,
	}

	go p.loop()
	return p
}

// SetTimeout 设置发布操作的超时时间
func (p *Pipe[T]) SetTimeout(timeout time.Duration) {
	p.Lock()
	defer p.Unlock()
	p.timeout = timeout
}

func (p *Pipe[T]) loop() {
	defer func() {
		if r := recover(); r != nil {
			// 处理 panic，避免管道崩溃
		}
	}()

	for {
		select {
		case payload := <-p.channel:
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
				if r := recover(); r != nil {
					// 处理单个处理器的 panic，不影响其他处理器
				}
			}()
			handlerInfo.Handler(payload)
		}()
	}
}

// Subscribe 向管道添加一个处理器
func (p *Pipe[T]) Subscribe(handler Handler[T]) error {
	return p.SubscribeWithPriority(handler, 0)
}

// SubscribeWithPriority 向管道添加一个带优先级的处理器
func (p *Pipe[T]) SubscribeWithPriority(handler Handler[T], priority int) error {
	if p.closed.Load() {
		return ErrChannelClosed
	}

	// 生成唯一ID
	handlerID := uintptr(unsafe.Pointer(&handler))

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

	handlerID := uintptr(unsafe.Pointer(&handler))

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
	p.RUnlock()

	select {
	case ch <- payload:
		return nil
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
	p.RUnlock()

	select {
	case ch <- payload:
		return nil
	case <-ctx.Done():
		return ctx.Err()
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

// Close 关闭管道
func (p *Pipe[T]) Close() {
	if !p.closed.CompareAndSwap(false, true) {
		return // 已经关闭
	}

	p.cancel() // 取消上下文
	close(p.stopCh)

	// 安全关闭通道
	go func() {
		time.Sleep(100 * time.Millisecond) // 给处理器一些时间完成
		close(p.channel)
	}()

	p.Lock()
	p.handlers = nil
	p.handlerMap = nil
	p.Unlock()
}

// IsClosed 返回管道是否已关闭
func (p *Pipe[T]) IsClosed() bool {
	return p.closed.Load()
}

// Len 返回订阅者的数量
func (p *Pipe[T]) Len() int {
	p.RLock()
	defer p.RUnlock()
	return len(p.handlers)
}

// GetStats 获取管道统计信息
func (p *Pipe[T]) GetStats() map[string]interface{} {
	p.RLock()
	defer p.RUnlock()

	return map[string]interface{}{
		"handler_count": len(p.handlers),
		"buffer_size":   p.bufferSize,
		"queue_length":  len(p.channel),
		"closed":        p.closed.Load(),
		"timeout":       p.timeout,
	}
}
