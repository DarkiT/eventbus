package eventbus

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func pipeHandlerOne(val int) {
}

func pipeHandlerTwo(val int) {
}

func Test_NewPipe(t *testing.T) {
	p := NewPipe[int]()
	assert.NotNil(t, p)
	assert.NotNil(t, p.channel)
	assert.NotNil(t, p.stopCh)
	assert.NotNil(t, p.handlers)
	p.Close()
}

func Test_NewPipeWithTimeout(t *testing.T) {
	const custom = 42 * time.Millisecond
	p := NewPipeWithTimeout[int](custom)
	defer p.Close()

	stats := p.GetStats()
	assert.Equal(t, custom, stats["timeout"])
}

func Test_NewBufferedPipe(t *testing.T) {
	p := NewBufferedPipe[int](100)
	assert.NotNil(t, p)
	assert.NotNil(t, p.channel)
	assert.Equal(t, 100, cap(p.channel))
	assert.NotNil(t, p.stopCh)
	assert.NotNil(t, p.handlers)
	p.Close()

	pipeZero := NewBufferedPipe[int](0)
	assert.NotNil(t, pipeZero)
	assert.NotNil(t, pipeZero.channel)
	assert.Equal(t, 0, cap(pipeZero.channel))
	assert.NotNil(t, pipeZero.stopCh)
	assert.NotNil(t, pipeZero.handlers)
	pipeZero.Close()
}

func Test_NewBufferedPipeWithTimeout(t *testing.T) {
	const custom = 150 * time.Millisecond
	p := NewBufferedPipeWithTimeout[int](10, custom)
	defer p.Close()

	stats := p.GetStats()
	assert.Equal(t, custom, stats["timeout"])
	assert.Equal(t, 10, stats["buffer_size"])
}

func Test_PipeSubscribe(t *testing.T) {
	p := NewPipe[int]()
	assert.NotNil(t, p)
	assert.NotNil(t, p.channel)

	err := p.Subscribe(pipeHandlerOne)
	assert.Nil(t, err)
	p.Close()
	err = p.Subscribe(pipeHandlerTwo)
	assert.Equal(t, ErrChannelClosed, err)
}

func Test_PipeUnsubscribe(t *testing.T) {
	p := NewPipe[int]()
	assert.NotNil(t, p)
	assert.NotNil(t, p.channel)

	err := p.Subscribe(pipeHandlerOne)
	assert.Nil(t, err)
	err = p.Unsubscribe(pipeHandlerOne)
	assert.Nil(t, err)

	err = p.Subscribe(pipeHandlerOne)
	assert.Nil(t, err)
	p.Close()
	err = p.Unsubscribe(pipeHandlerOne)
	assert.Equal(t, ErrChannelClosed, err)
}

func Test_PipePublish(t *testing.T) {
	p := NewPipe[int]()
	assert.NotNil(t, p)
	assert.NotNil(t, p.channel)

	assert.Nil(t, p.Subscribe(pipeHandlerOne))
	time.Sleep(time.Millisecond)

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 1000 {
			err := p.Publish(i)
			assert.Nil(t, err)
		}
	})
	wg.Wait()

	p.Close()
	err := p.Publish(1)
	assert.Equal(t, ErrChannelClosed, err)
}

func Test_PipePublishSync(t *testing.T) {
	p := NewPipe[int]()
	assert.NotNil(t, p)
	assert.NotNil(t, p.channel)

	assert.Nil(t, p.Subscribe(pipeHandlerOne))
	time.Sleep(time.Millisecond)

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 1000 {
			err := p.PublishSync(i)
			assert.Nil(t, err)
		}
	})
	wg.Wait()

	p.Close()
	err := p.PublishSync(1)
	assert.Equal(t, ErrChannelClosed, err)
}

func Test_PipeSubscribeOnce_Concurrent(t *testing.T) {
	p := NewPipe[int]()
	defer p.Close()

	var count atomic.Int32
	assert.NoError(t, p.SubscribeOnce(func(v int) {
		count.Add(1)
	}))

	var wg sync.WaitGroup
	const concurrency = 200
	wg.Add(concurrency)
	for i := range concurrency {
		go func() {
			defer wg.Done()
			_ = p.Publish(i)
		}()
	}
	wg.Wait()
	time.Sleep(20 * time.Millisecond)

	assert.Equal(t, int32(1), count.Load())

	// 再次发送不会重复
	_ = p.PublishSync(999)
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, int32(1), count.Load())
}

func Test_PipeSubscribeOnce_WhenClosed(t *testing.T) {
	p := NewPipe[int]()
	p.Close()

	err := p.SubscribeOnce(func(v int) {})
	assert.ErrorIs(t, err, ErrChannelClosed)
}

func Test_PipeClose(t *testing.T) {
	p := NewPipe[int]()
	assert.NotNil(t, p)
	assert.NotNil(t, p.channel)

	err := p.Subscribe(pipeHandlerOne)
	assert.Nil(t, err)
	p.Close()
	err = p.Unsubscribe(pipeHandlerOne)
	assert.Equal(t, ErrChannelClosed, err)
	p.Close()
}

func TestPipeCloseConcurrentPublishDoesNotPanic(t *testing.T) {
	for i := range 200 {
		p := NewPipe[int]()

		panicCh := make(chan any, 1)
		errCh := make(chan error, 1)

		var wg sync.WaitGroup
		wg.Go(func() {
			var err error
			defer func() {
				if r := recover(); r != nil {
					panicCh <- r
				}
				errCh <- err
			}()
			err = p.Publish(i)
		})

		time.Sleep(50 * time.Microsecond)
		p.Close()
		wg.Wait()

		select {
		case panicVal := <-panicCh:
			t.Fatalf("发生 panic: %v", panicVal)
		default:
		}

		err := <-errCh
		if err != nil {
			assert.Equal(t, ErrChannelClosed, err)
		}
	}
}
