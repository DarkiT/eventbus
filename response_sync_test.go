package eventbus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscribeWithResponse(t *testing.T) {
	bus := New()
	defer bus.Close()

	// 测试订阅响应处理器
	err := bus.SubscribeWithResponse("test.topic", func(topic string, payload any) (any, error) {
		return "success", nil
	})
	assert.NoError(t, err)

	// 测试 nil 处理器
	err = bus.SubscribeWithResponse("test.topic", nil)
	assert.Equal(t, ErrHandlerIsNotFunc, err)
}

func TestPublishSyncAll_AllSuccess(t *testing.T) {
	bus := New()
	defer bus.Close()

	// 添加多个成功的响应处理器
	for i := range 3 {
		id := i
		err := bus.SubscribeWithResponse("test.topic", func(topic string, payload any) (any, error) {
			return map[string]any{"handler": id, "payload": payload}, nil
		})
		assert.NoError(t, err)
	}

	// 执行 PublishSyncAll
	result, err := bus.PublishSyncAll("test.topic", "test_payload")
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// 验证结果
	assert.True(t, result.Success)
	assert.Equal(t, 3, result.HandlerCount)
	assert.Equal(t, 3, result.SuccessCount)
	assert.Equal(t, 0, result.FailureCount)
	assert.Len(t, result.Results, 3)

	// 验证每个处理器都成功
	for _, handlerResult := range result.Results {
		assert.True(t, handlerResult.Success)
		assert.NoError(t, handlerResult.Error)
		assert.NotNil(t, handlerResult.Result)
		assert.Greater(t, handlerResult.Duration, time.Duration(0))
	}
}

func TestPublishSyncAll_PartialFailure(t *testing.T) {
	bus := New()
	defer bus.Close()

	// 添加成功的处理器
	err := bus.SubscribeWithResponse("test.topic", func(topic string, payload any) (any, error) {
		return "success", nil
	})
	assert.NoError(t, err)

	// 添加失败的处理器
	err = bus.SubscribeWithResponse("test.topic", func(topic string, payload any) (any, error) {
		return nil, errors.New("handler failed")
	})
	assert.NoError(t, err)

	// 执行 PublishSyncAll
	result, err := bus.PublishSyncAll("test.topic", "test_payload")
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// 验证结果 - 应该失败因为不是所有处理器都成功
	assert.False(t, result.Success)
	assert.Equal(t, 2, result.HandlerCount)
	assert.Equal(t, 1, result.SuccessCount)
	assert.Equal(t, 1, result.FailureCount)
	assert.Len(t, result.Results, 2)
}

func TestPublishSyncAny_PartialSuccess(t *testing.T) {
	bus := New()
	defer bus.Close()

	// 添加成功的处理器
	err := bus.SubscribeWithResponse("test.topic", func(topic string, payload any) (any, error) {
		return "success", nil
	})
	assert.NoError(t, err)

	// 添加失败的处理器
	err = bus.SubscribeWithResponse("test.topic", func(topic string, payload any) (any, error) {
		return nil, errors.New("handler failed")
	})
	assert.NoError(t, err)

	// 执行 PublishSyncAny
	result, err := bus.PublishSyncAny("test.topic", "test_payload")
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// 验证结果 - 应该成功因为至少有一个处理器成功
	assert.True(t, result.Success)
	assert.Equal(t, 2, result.HandlerCount)
	assert.Equal(t, 1, result.SuccessCount)
	assert.Equal(t, 1, result.FailureCount)
	assert.Len(t, result.Results, 2)
}

func TestPublishSyncAny_AllFailure(t *testing.T) {
	bus := New()
	defer bus.Close()

	// 添加失败的处理器
	for range 3 {
		err := bus.SubscribeWithResponse("test.topic", func(topic string, payload any) (any, error) {
			return nil, errors.New("handler failed")
		})
		assert.NoError(t, err)
	}

	// 执行 PublishSyncAny
	result, err := bus.PublishSyncAny("test.topic", "test_payload")
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// 验证结果 - 应该失败因为所有处理器都失败
	assert.False(t, result.Success)
	assert.Equal(t, 3, result.HandlerCount)
	assert.Equal(t, 0, result.SuccessCount)
	assert.Equal(t, 3, result.FailureCount)
	assert.Len(t, result.Results, 3)
}

func TestPublishSyncAll_NoHandlers(t *testing.T) {
	bus := New()
	defer bus.Close()

	// 没有处理器的情况
	result, err := bus.PublishSyncAll("nonexistent.topic", "test_payload")
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// 验证结果 - 没有处理器应该返回成功
	assert.True(t, result.Success)
	assert.Equal(t, 0, result.HandlerCount)
	assert.Equal(t, 0, result.SuccessCount)
	assert.Equal(t, 0, result.FailureCount)
	assert.Len(t, result.Results, 0)
}

func TestPublishSyncAny_NoHandlers(t *testing.T) {
	bus := New()
	defer bus.Close()

	result, err := bus.PublishSyncAny("nonexistent.topic", "test_payload")
	assert.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Success)
	assert.Equal(t, 0, result.HandlerCount)
	assert.Equal(t, 0, result.SuccessCount)
	assert.Equal(t, 0, result.FailureCount)
	assert.Len(t, result.Results, 0)
}

func TestPublishSyncAnyWithContextCancelDoesNotHideCompletedResult(t *testing.T) {
	bus := New()
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	fastDone := make(chan struct{})
	require.NoError(t, bus.SubscribeWithResponseContextAndPriority("test.any.cancel", func(ctx context.Context, topic string, payload any) (any, error) {
		close(fastDone)
		return "fast", nil
	}, 1))
	require.NoError(t, bus.SubscribeWithResponseContextAndPriority("test.any.cancel", func(ctx context.Context, topic string, payload any) (any, error) {
		<-fastDone
		cancel()
		time.Sleep(20 * time.Millisecond)
		return "slow", nil
	}, -1))

	result, err := bus.PublishSyncAnyWithContext(ctx, "test.any.cancel", "payload")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)
	assert.GreaterOrEqual(t, result.SuccessCount, 1)
}

func TestPublishSyncAll_HandlerPanic(t *testing.T) {
	bus := New()
	defer bus.Close()

	// 添加会panic的处理器
	err := bus.SubscribeWithResponse("test.topic", func(topic string, payload any) (any, error) {
		panic("test panic")
	})
	assert.NoError(t, err)

	// 添加正常的处理器
	err = bus.SubscribeWithResponse("test.topic", func(topic string, payload any) (any, error) {
		return "success", nil
	})
	assert.NoError(t, err)

	// 执行 PublishSyncAll
	result, err := bus.PublishSyncAll("test.topic", "test_payload")
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// 验证结果 - panic应该被捕获并转换为错误
	assert.False(t, result.Success) // 因为有一个处理器失败
	assert.Equal(t, 2, result.HandlerCount)
	assert.Equal(t, 1, result.SuccessCount)
	assert.Equal(t, 1, result.FailureCount)

	// 检查panic处理器的结果
	panicFound := false
	for _, handlerResult := range result.Results {
		if !handlerResult.Success {
			assert.Contains(t, handlerResult.Error.Error(), "handler panic")
			panicFound = true
		}
	}
	assert.True(t, panicFound, "应该找到panic处理器的错误")
}

func TestPublishSyncAll_PriorityOrder(t *testing.T) {
	bus := New()
	defer bus.Close()

	// 高优先级处理器
	require.NoError(t, bus.SubscribeWithResponseAndPriority("priority.topic", func(topic string, payload any) (any, error) {
		return "p10", nil
	}, 10))

	// 低优先级处理器
	require.NoError(t, bus.SubscribeWithResponseAndPriority("priority.*", func(topic string, payload any) (any, error) {
		return "p1", nil
	}, 1))

	result, err := bus.PublishSyncAll("priority.topic", "payload")
	require.NoError(t, err)
	require.Len(t, result.Results, 2)
	assert.Equal(t, "p10", result.Results[0].Result)
	assert.Equal(t, "p1", result.Results[1].Result)
}

func TestSetTimeoutAffectsSyncPublish(t *testing.T) {
	bus := New()
	defer bus.Close()

	bus.SetTimeout(30 * time.Millisecond)

	require.NoError(t, bus.SubscribeWithResponse("timeout.topic", func(topic string, payload any) (any, error) {
		time.Sleep(80 * time.Millisecond)
		return "late", nil
	}))

	_, err := bus.PublishSyncAll("timeout.topic", "payload")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPublishTimeout)
}

func TestPublishSyncAll_FilterBlocks(t *testing.T) {
	bus := New()
	defer bus.Close()

	var called atomic.Int32
	bus.AddFilter(FilterFunc(func(topic string, payload any) bool {
		return false
	}))

	require.NoError(t, bus.SubscribeWithResponse("filter.topic", func(topic string, payload any) (any, error) {
		called.Add(1)
		return "ok", nil
	}))

	result, err := bus.PublishSyncAll("filter.topic", "payload")
	require.NoError(t, err)

	assert.True(t, result.Success)
	assert.Equal(t, 0, result.HandlerCount)
	assert.Equal(t, int32(0), called.Load())
}

func TestPublishSyncAll_MiddlewareApplied(t *testing.T) {
	bus := New()
	defer bus.Close()

	mwCall := atomic.Int32{}
	bus.Use(&testMiddleware{
		beforeFunc: func(topic string, payload any) any {
			mwCall.Add(1)
			if s, ok := payload.(string); ok {
				return s + "_mw"
			}
			return payload
		},
		afterFunc: func(topic string, payload any) {
			mwCall.Add(1)
		},
	})

	var got string
	require.NoError(t, bus.SubscribeWithResponse("middleware.topic", func(topic string, payload any) (any, error) {
		got = payload.(string)
		return payload, nil
	}))

	result, err := bus.PublishSyncAll("middleware.topic", "origin")
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "origin_mw", got)
	assert.Equal(t, int32(2), mwCall.Load())
}

func TestPublishSyncAll_WithMQTTWildcard(t *testing.T) {
	bus := New()
	defer bus.Close()

	// 使用通配符订阅
	err := bus.SubscribeWithResponse("sensor/+/temperature", func(topic string, payload any) (any, error) {
		return map[string]any{"matched_topic": topic, "payload": payload}, nil
	})
	assert.NoError(t, err)

	err = bus.SubscribeWithResponse("sensor/#", func(topic string, payload any) (any, error) {
		return map[string]any{"wildcard_match": topic}, nil
	})
	assert.NoError(t, err)

	// 发布到匹配的主题
	result, err := bus.PublishSyncAll("sensor/room1/temperature", "25°C")
	assert.NoError(t, err)
	assert.NotNil(t, result)

	// 验证结果 - 两个通配符都应该匹配
	assert.True(t, result.Success)
	assert.Equal(t, 2, result.HandlerCount)
	assert.Equal(t, 2, result.SuccessCount)
	assert.Equal(t, 0, result.FailureCount)

	// 验证传递的主题是原始格式
	for _, handlerResult := range result.Results {
		assert.True(t, handlerResult.Success)
		resultMap := handlerResult.Result.(map[string]any)
		if topic, ok := resultMap["matched_topic"]; ok {
			assert.Equal(t, "sensor/room1/temperature", topic)
		}
		if topic, ok := resultMap["wildcard_match"]; ok {
			assert.Equal(t, "sensor/room1/temperature", topic)
		}
	}
}

func TestPublishSyncWithPriority(t *testing.T) {
	bus := New()
	defer bus.Close()

	results := make([]int, 0)
	var mutex sync.Mutex

	// 添加不同优先级的处理器
	err := bus.SubscribeWithResponseAndPriority("test.topic", func(topic string, payload any) (any, error) {
		mutex.Lock()
		results = append(results, 1)
		mutex.Unlock()
		return "handler1", nil
	}, 1)
	assert.NoError(t, err)

	err = bus.SubscribeWithResponseAndPriority("test.topic", func(topic string, payload any) (any, error) {
		mutex.Lock()
		results = append(results, 3)
		mutex.Unlock()
		return "handler3", nil
	}, 3)
	assert.NoError(t, err)

	err = bus.SubscribeWithResponseAndPriority("test.topic", func(topic string, payload any) (any, error) {
		mutex.Lock()
		results = append(results, 2)
		mutex.Unlock()
		return "handler2", nil
	}, 2)
	assert.NoError(t, err)

	// 执行发布
	result, err := bus.PublishSyncAll("test.topic", "test")
	assert.NoError(t, err)
	assert.True(t, result.Success)

	// 注意：由于并发执行，这里无法测试执行顺序
	// 但可以验证所有处理器都被执行了
	assert.Equal(t, 3, result.SuccessCount)
}

// 性能测试
func BenchmarkPublishSyncAll(b *testing.B) {
	bus := New()
	defer bus.Close()

	// 添加多个响应处理器
	for i := range 10 {
		id := i
		err := bus.SubscribeWithResponse("bench.topic", func(topic string, payload any) (any, error) {
			// 模拟少量处理时间
			time.Sleep(time.Microsecond)
			return map[string]any{"handler": id, "result": "ok"}, nil
		})
		if err != nil {
			b.Fatalf("订阅失败: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := bus.PublishSyncAll("bench.topic", "benchmark_payload")
		if err != nil || !result.Success {
			b.Fatalf("PublishSyncAll 失败: %v", err)
		}
	}
}

func BenchmarkPublishSyncAny(b *testing.B) {
	bus := New()
	defer bus.Close()

	// 添加多个响应处理器
	for i := range 10 {
		id := i
		err := bus.SubscribeWithResponse("bench.topic", func(topic string, payload any) (any, error) {
			// 模拟少量处理时间
			time.Sleep(time.Microsecond)
			return map[string]any{"handler": id, "result": "ok"}, nil
		})
		if err != nil {
			b.Fatalf("订阅失败: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := bus.PublishSyncAny("bench.topic", "benchmark_payload")
		if err != nil || !result.Success {
			b.Fatalf("PublishSyncAny 失败: %v", err)
		}
	}
}

func BenchmarkPublishSyncAll_vs_PublishSync(b *testing.B) {
	// 对比传统PublishSync和新的PublishSyncAll的性能差异
	bus := New()
	defer bus.Close()

	// 为传统方式添加处理器
	for range 10 {
		err := bus.Subscribe("traditional.topic", func(topic string, payload any) {
			time.Sleep(time.Microsecond)
		})
		if err != nil {
			b.Fatalf("订阅失败: %v", err)
		}
	}

	// 为响应式添加处理器
	for i := range 10 {
		id := i
		err := bus.SubscribeWithResponse("response.topic", func(topic string, payload any) (any, error) {
			time.Sleep(time.Microsecond)
			return map[string]any{"handler": id}, nil
		})
		if err != nil {
			b.Fatalf("响应式订阅失败: %v", err)
		}
	}

	b.Run("TraditionalPublishSync", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := bus.PublishSync("traditional.topic", "payload")
			if err != nil {
				b.Fatalf("PublishSync 失败: %v", err)
			}
		}
	})

	b.Run("NewPublishSyncAll", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			result, err := bus.PublishSyncAll("response.topic", "payload")
			if err != nil || !result.Success {
				b.Fatalf("PublishSyncAll 失败: %v", err)
			}
		}
	})
}

func BenchmarkPublishSyncAll_vs_PublishSync_FrameworkOnly(b *testing.B) {
	// 纯框架开销对比：不引入业务 sleep 或额外对象构造
	bus := New()
	defer bus.Close()

	for range 10 {
		err := bus.Subscribe("traditional.framework.topic", func(topic string, payload any) {})
		if err != nil {
			b.Fatalf("订阅失败: %v", err)
		}
	}

	for range 10 {
		err := bus.SubscribeWithResponse("response.framework.topic", func(topic string, payload any) (any, error) {
			return 1, nil
		})
		if err != nil {
			b.Fatalf("响应式订阅失败: %v", err)
		}
	}

	b.Run("TraditionalPublishSync_NoWork", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := bus.PublishSync("traditional.framework.topic", "payload")
			if err != nil {
				b.Fatalf("PublishSync 失败: %v", err)
			}
		}
	})

	b.Run("NewPublishSyncAll_NoWork", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			result, err := bus.PublishSyncAll("response.framework.topic", "payload")
			if err != nil || !result.Success {
				b.Fatalf("PublishSyncAll 失败: %v", err)
			}
		}
	})
}

func BenchmarkConcurrentPublishSyncAll(b *testing.B) {
	bus := New()
	defer bus.Close()

	// 添加响应处理器
	for i := range 5 {
		id := i
		err := bus.SubscribeWithResponse("concurrent.topic", func(topic string, payload any) (any, error) {
			time.Sleep(time.Microsecond)
			return map[string]any{"handler": id}, nil
		})
		if err != nil {
			b.Fatalf("订阅失败: %v", err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			result, err := bus.PublishSyncAll("concurrent.topic", "payload")
			if err != nil || !result.Success {
				b.Fatalf("并发 PublishSyncAll 失败: %v", err)
			}
		}
	})
}

func TestPublishSyncAny_WithCallerCanceledContext_IsNotMaskedByLaterSuccess(t *testing.T) {
	bus := New()
	defer bus.Close()

	require.NoError(t, bus.SubscribeWithResponse("cancel.topic", func(topic string, payload any) (any, error) {
		time.Sleep(20 * time.Millisecond)
		return "late-success", nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := bus.PublishSyncAnyWithContext(ctx, "cancel.topic", "payload")
	assert.Nil(t, result)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPublishSyncAny_NonCooperativeHandlerExceedsTimeout(t *testing.T) {
	bus := New()
	defer bus.Close()
	bus.SetTimeout(20 * time.Millisecond)

	require.NoError(t, bus.SubscribeWithResponse("slow.any", func(topic string, payload any) (any, error) {
		time.Sleep(60 * time.Millisecond)
		return "done", nil
	}))

	start := time.Now()
	result, err := bus.PublishSyncAny("slow.any", "payload")
	elapsed := time.Since(start)

	assert.Nil(t, result)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPublishTimeout)
	assert.Less(t, elapsed, 60*time.Millisecond)
}

func TestPublishSyncAnyValueWithContext_FastReturnOnSuccess(t *testing.T) {
	bus := New()
	defer bus.Close()

	require.NoError(t, bus.SubscribeWithResponseContext("fast.value", func(ctx context.Context, topic string, payload any) (any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(300 * time.Millisecond):
			return nil, errors.New("too slow")
		}
	}))
	require.NoError(t, bus.SubscribeWithResponse("fast.value", func(topic string, payload any) (any, error) {
		return "ok", nil
	}))

	start := time.Now()
	value, err := bus.PublishSyncAnyValueWithContext(context.Background(), "fast.value", "payload")
	elapsed := time.Since(start)
	require.NoError(t, err)
	assert.Equal(t, "ok", value)
	assert.Less(t, elapsed, 250*time.Millisecond)
}
