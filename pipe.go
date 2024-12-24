package eventbus

import (
	"sync"
	"time"
)

// Handler defines a function type that handles messages of type T
type Handler[T any] func(payload T)

// Pipe is a generic wrapper for channel-based message passing
type Pipe[T any] struct {
	sync.RWMutex
	bufferSize int
	channel    chan T
	handlers   *CowMap
	closed     bool
	stopCh     chan struct{}
	timeout    time.Duration
}

// NewPipe creates an unbuffered pipe
func NewPipe[T any]() *Pipe[T] {
	return newPipe[T](-1)
}

// NewBufferedPipe creates a buffered pipe with the specified buffer size
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
		handlers:   NewCowMap(),
		stopCh:     make(chan struct{}),
		timeout:    DefaultTimeout,
	}

	go p.loop()
	return p
}

// SetTimeout sets the timeout duration for publish operations
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

// Subscribe adds a handler to the pipe
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

// Unsubscribe removes a handler from the pipe
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

// Publish sends a message to all subscribers asynchronously with timeout
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

// PublishSync sends a message to all subscribers synchronously
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

// Close shuts down the pipe
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

// IsClosed returns whether the pipe is closed
func (p *Pipe[T]) IsClosed() bool {
	p.RLock()
	defer p.RUnlock()
	return p.closed
}

// Len returns the number of subscribers
func (p *Pipe[T]) Len() uint32 {
	return p.handlers.Len()
}
