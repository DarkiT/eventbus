package eventbus

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBatchError_Error(t *testing.T) {
	res := &BatchResult{
		FailedCount: 2,
	}
	err := BatchError{Result: res}
	msg := err.Error()
	if !strings.Contains(msg, "2 items failed") {
		t.Fatalf("error message not contain failed count: %s", msg)
	}
}

func TestChannelPublishBatchAsync_ContextCanceled(t *testing.T) {
	bus := New()
	// 无缓冲，确保 publishBatchAsync 阻塞等待并响应 ctx.Done
	ch := newChannel("ctx", -1, bus)
	defer ch.close()

	// 停掉 loop，确保无消费者，这样写入必定阻塞
	ch.cancel()
	ch.loopWG.Wait()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env := messageEnvelope{ctx: ctx, topic: "ctx", payload: 1}
	err := ch.publishBatchAsync([]messageEnvelope{env})
	if err == nil || (!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)) {
		t.Fatalf("expected context cancel, got %v", err)
	}
}

func TestChannelPublishBatchAsync_StopClosed(t *testing.T) {
	bus := New()
	ch := newChannel("stop", 1, bus)
	ch.close()
	env := messageEnvelope{ctx: context.Background(), topic: "stop", payload: 1}
	err := ch.publishBatchAsync([]messageEnvelope{env})
	if !errors.Is(err, ErrChannelClosed) {
		t.Fatalf("expected channel closed, got %v", err)
	}
}

func TestChannelPublishBatchAsync_TimeoutNoReceiver(t *testing.T) {
	bus := New()
	bus.SetTimeout(time.Millisecond * 5)
	ch := newChannel("timeout", 0, bus)
	// 停止 loop，制造无接收者场景
	ch.cancel()
	ch.loopWG.Wait()

	env := messageEnvelope{ctx: context.Background(), topic: "timeout", payload: 1}
	err := ch.publishBatchAsync([]messageEnvelope{env})
	if !errors.Is(err, ErrPublishTimeout) {
		t.Fatalf("expected publish timeout, got %v", err)
	}
}

func TestChannelPublishBatchAsync_QueueFull(t *testing.T) {
	bus := New(1)
	tracer := NewMetricsTracer()
	bus.SetTracer(tracer)
	ch := newChannel("full", 1, bus)
	defer ch.close()

	// 停掉 loop，避免消费
	ch.cancel()
	ch.loopWG.Wait()
	// 先占满队列
	ch.channel <- messageEnvelope{ctx: context.Background(), topic: "full", payload: 0}

	env := messageEnvelope{ctx: context.Background(), topic: "full", payload: 1}
	_ = ch.publishBatchAsync([]messageEnvelope{env})

	stats := tracer.GetQueueMetrics()
	if len(stats) == 0 {
		t.Fatalf("queue metrics should not be empty")
	}
}

func TestPublishBatch_SlowConsumerTrace(t *testing.T) {
	bus := New(10)
	tracer := NewMetricsTracer()
	bus.SetTracer(tracer)
	defer bus.Close()

	var done sync.WaitGroup
	done.Add(1)

	_ = bus.Subscribe("slow", func(topic string, payload any) {
		time.Sleep(time.Millisecond * 150)
		done.Done()
	})

	msgs := []BatchMessage{{Topic: "slow", Payload: 1}}
	_, _ = bus.PublishBatch(msgs)
	done.Wait()

	m := tracer.GetLatencyMetrics()
	if len(m) == 0 {
		t.Fatalf("expected latency metrics")
	}
}

func TestEventBus_SetTimeout(t *testing.T) {
	bus := New()
	defer bus.Close()

	bus.SetTimeout(10 * time.Second)
	if bus.getTimeout() != 10*time.Second {
		t.Fatalf("timeout not set")
	}

	bus.SetTimeout(0)
	if bus.getTimeout() != 10*time.Second {
		t.Fatalf("zero timeout should be ignored")
	}

	bus.SetTimeout(-time.Second)
	if bus.getTimeout() != 10*time.Second {
		t.Fatalf("negative timeout should be ignored")
	}
}

func TestChannelLoopPanicRecover(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count int32
	ch := newChannel("panic", 1, bus)
	_ = ch.subscribe(func(topic string, payload any) {
		atomic.AddInt32(&count, 1)
		panic("boom")
	})
	_ = ch.subscribe(func(topic string, payload any) {
		atomic.AddInt32(&count, 1)
	})

	_ = ch.publishAsync(context.Background(), "panic", 1)
	time.Sleep(time.Millisecond * 10)

	if atomic.LoadInt32(&count) != 2 {
		t.Fatalf("expected both handlers executed despite panic")
	}

	ch.close()
}
