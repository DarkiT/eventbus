package eventbus

import (
	"sync"
	"time"
)

// Handler 定义了一个函数类型，用于处理 T 类型的消息
type Handler[T any] func(payload T)

// Pipe 是一个通用的管道封装，用于基于通道的消息传递
type Pipe[T any] struct {
	sync.RWMutex
	bufferSize int
	channel    chan T
	handlers   *_CowMap
	closed     bool
	stopCh     chan struct{}
	timeout    time.Duration
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

	p := &Pipe[T]{
		bufferSize: bufferSize,
		channel:    ch,
		handlers:   newCowMap(),
		stopCh:     make(chan struct{}),
		timeout:    DefaultTimeout,
	}

	go p.loop()
	return p
}

// SetTimeout 设置发布操作的超时时间
func (p *Pipe[T]) SetTimeout(timeout time.Duration) {
	p.timeout = timeout
}

func (p *Pipe[T]) loop() {
	for {
		select {
		case payload := <-p.channel:
			p.handlers.Range(func(key, value interface{}) bool {
				if handler, ok := value.(Handler[T]); ok {
					handler(payload)
				}
				return true
			})
		case <-p.stopCh:
			return
		}
	}
}

// Subscribe 向管道添加一个处理器
func (p *Pipe[T]) Subscribe(handler Handler[T]) error {
	p.RLock()
	if p.closed {
		p.RUnlock()
		return ErrChannelClosed
	}
	p.RUnlock()

	p.handlers.Store(&handler, handler)
	return nil
}

// Unsubscribe 从管道中移除一个处理器
func (p *Pipe[T]) Unsubscribe(handler Handler[T]) error {
	p.RLock()
	if p.closed {
		p.RUnlock()
		return ErrChannelClosed
	}
	p.RUnlock()

	p.handlers.Delete(&handler)
	return nil
}

// Publish 异步发送消息给所有订阅者，并设置超时时间
func (p *Pipe[T]) Publish(payload T) error {
	p.RLock()
	if p.closed {
		p.RUnlock()
		return ErrChannelClosed
	}
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

// PublishSync 同步发送消息给所有订阅者
func (p *Pipe[T]) PublishSync(payload T) error {
	p.RLock()
	if p.closed {
		p.RUnlock()
		return ErrChannelClosed
	}
	p.RUnlock()

	p.handlers.Range(func(key, value interface{}) bool {
		if handler, ok := value.(Handler[T]); ok {
			handler(payload)
		}
		return true
	})
	return nil
}

// Close 关闭管道
func (p *Pipe[T]) Close() {
	p.Lock()
	defer p.Unlock()

	if p.closed {
		return
	}

	p.closed = true
	close(p.stopCh)
	close(p.channel)
	p.handlers.Clear()
}

// IsClosed 返回管道是否已关闭
func (p *Pipe[T]) IsClosed() bool {
	p.RLock()
	defer p.RUnlock()
	return p.closed
}

// Len 返回订阅者的数量
func (p *Pipe[T]) Len() uint32 {
	return p.handlers.Len()
}
