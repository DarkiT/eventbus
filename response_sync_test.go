package eventbus

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
