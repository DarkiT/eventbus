package eventbus

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPipeSubscribeWithResponse(t *testing.T) {
	pipe := NewPipe[string]()
	defer pipe.Close()

	handler := func(payload string) (any, error) {
		return payload + "_response", nil
	}

	cancel, err := pipe.SubscribeWithResponse(handler)
	if err != nil {
		t.Fatalf("Failed to subscribe with response: %v", err)
	}

	if cancel == nil {
		t.Fatalf("Expected subscription handle, got nil")
	}

	stats := pipe.GetStats()
	if stats["response_handler_count"] != 1 {
		t.Fatalf("Expected 1 response handler, got %v", stats["response_handler_count"])
	}

	cancel()
	stats = pipe.GetStats()
	if stats["response_handler_count"] != 0 {
		t.Fatalf("Expected cancel to remove handler, got %v", stats["response_handler_count"])
	}
}

func TestPipePublishSyncAll_Success(t *testing.T) {
	pipe := NewPipe[string]()
	defer pipe.Close()

	// 添加3个响应式处理器，都会成功
	for range 3 {
		handler := func(payload string) (any, error) {
			return payload + "_response", nil
		}
		_, err := pipe.SubscribeWithResponse(handler)
		if err != nil {
			t.Fatalf("Failed to subscribe handler: %v", err)
		}
	}

	result, err := pipe.PublishSyncAll("test_payload")
	if err != nil {
		t.Fatalf("PublishSyncAll failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success=true, got %v", result.Success)
	}

	if result.HandlerCount != 3 {
		t.Fatalf("Expected 3 handlers, got %d", result.HandlerCount)
	}

	if result.SuccessCount != 3 {
		t.Fatalf("Expected 3 successes, got %d", result.SuccessCount)
	}

	if result.FailureCount != 0 {
		t.Fatalf("Expected 0 failures, got %d", result.FailureCount)
	}
}

func TestPipePublishSyncAll_Failure(t *testing.T) {
	pipe := NewPipe[string]()
	defer pipe.Close()

	// 添加成功的处理器
	successHandler := func(payload string) (any, error) {
		return payload + "_success", nil
	}
	_, _ = pipe.SubscribeWithResponse(successHandler)

	// 添加失败的处理器
	failureHandler := func(payload string) (any, error) {
		return nil, errors.New("handler failed")
	}
	_, _ = pipe.SubscribeWithResponse(failureHandler)

	result, err := pipe.PublishSyncAll("test_payload")
	if err != nil {
		t.Fatalf("PublishSyncAll failed: %v", err)
	}

	if result.Success {
		t.Fatalf("Expected success=false due to failure, got %v", result.Success)
	}

	if result.HandlerCount != 2 {
		t.Fatalf("Expected 2 handlers, got %d", result.HandlerCount)
	}

	if result.SuccessCount != 1 {
		t.Fatalf("Expected 1 success, got %d", result.SuccessCount)
	}

	if result.FailureCount != 1 {
		t.Fatalf("Expected 1 failure, got %d", result.FailureCount)
	}
}

func TestPipePublishSyncAny_Success(t *testing.T) {
	pipe := NewPipe[string]()
	defer pipe.Close()

	// 添加成功的处理器
	successHandler := func(payload string) (any, error) {
		return payload + "_success", nil
	}
	_, _ = pipe.SubscribeWithResponse(successHandler)

	// 添加失败的处理器
	failureHandler := func(payload string) (any, error) {
		return nil, errors.New("handler failed")
	}
	_, _ = pipe.SubscribeWithResponse(failureHandler)

	result, err := pipe.PublishSyncAny("test_payload")
	if err != nil {
		t.Fatalf("PublishSyncAny failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success=true (any success), got %v", result.Success)
	}

	if result.HandlerCount != 2 {
		t.Fatalf("Expected 2 handlers, got %d", result.HandlerCount)
	}

	if result.SuccessCount != 1 {
		t.Fatalf("Expected 1 success, got %d", result.SuccessCount)
	}

	if result.FailureCount != 1 {
		t.Fatalf("Expected 1 failure, got %d", result.FailureCount)
	}
}

func TestPipePublishSyncAny_AllFailure(t *testing.T) {
	pipe := NewPipe[string]()
	defer pipe.Close()

	// 添加2个失败的处理器
	for range 2 {
		failureHandler := func(payload string) (any, error) {
			return nil, errors.New("handler failed")
		}
		_, _ = pipe.SubscribeWithResponse(failureHandler)
	}

	result, err := pipe.PublishSyncAny("test_payload")
	if err != nil {
		t.Fatalf("PublishSyncAny failed: %v", err)
	}

	if result.Success {
		t.Fatalf("Expected success=false (all failed), got %v", result.Success)
	}

	if result.SuccessCount != 0 {
		t.Fatalf("Expected 0 successes, got %d", result.SuccessCount)
	}

	if result.FailureCount != 2 {
		t.Fatalf("Expected 2 failures, got %d", result.FailureCount)
	}
}

func TestPipePublishSyncAllWithContextTimeout(t *testing.T) {
	pipe := NewPipe[int]()
	defer pipe.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	// 处理器模拟长耗时，超时后应该中断
	_ = pipe.SubscribeWithResponseContextAndPriority(func(ctx context.Context, payload int) (any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return payload * 2, nil
		}
	}, 0)

	res, err := pipe.PublishSyncAllWithContext(ctx, 1)
	require.Error(t, err)
	require.Equal(t, context.DeadlineExceeded, err)
	require.Len(t, res, 1)
}

func TestPipePublishSyncAll_DefaultTimeout(t *testing.T) {
	pipe := NewPipeWithTimeout[int](20 * time.Millisecond)
	defer pipe.Close()

	_ = pipe.SubscribeWithResponseContextAndPriority(func(ctx context.Context, payload int) (any, error) {
		time.Sleep(80 * time.Millisecond)
		return payload * 2, nil
	}, 0)

	start := time.Now()
	result, err := pipe.PublishSyncAll(1)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrPublishTimeout)
	require.NotNil(t, result)
	require.Less(t, elapsed, 80*time.Millisecond)
}

func TestPipeSubscribeWithResponseContextHandle(t *testing.T) {
	pipe := NewPipe[int]()
	defer pipe.Close()

	cancel, err := pipe.SubscribeWithResponseContextHandle(func(ctx context.Context, payload int) (any, error) {
		return payload + 1, nil
	})
	require.NoError(t, err)
	require.NotNil(t, cancel)

	stats := pipe.GetStats()
	require.Equal(t, 1, stats["response_handler_count"])

	cancel()
	stats = pipe.GetStats()
	require.Equal(t, 0, stats["response_handler_count"])
}

func TestPipePublishSyncAllResultWithContext(t *testing.T) {
	pipe := NewPipe[int]()
	defer pipe.Close()

	cancel, err := pipe.SubscribeWithResponseContextHandle(func(ctx context.Context, payload int) (any, error) {
		return payload * 2, nil
	})
	require.NoError(t, err)
	defer cancel()

	result, err := pipe.PublishSyncAllResultWithContext(context.Background(), 21)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success)
	require.Len(t, result.Results, 1)
	require.Equal(t, 42, result.Results[0].Result)
}

func TestPipePublishSyncAnyWithContextCancelDoesNotRollbackCompleted(t *testing.T) {
	pipe := NewPipe[int]()
	defer pipe.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// 快速完成的处理器
	_ = pipe.SubscribeWithResponseContext(func(ctx context.Context, payload int) (any, error) {
		return payload + 1, nil
	})

	// 慢处理器，在开始后立即取消上下文，但不应影响已完成的结果
	_ = pipe.SubscribeWithResponseContextAndPriority(func(ctx context.Context, payload int) (any, error) {
		cancel()
		time.Sleep(50 * time.Millisecond)
		return payload + 2, nil
	}, -1)

	result, err := pipe.PublishSyncAnyWithContext(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 11, result)
}

func TestPipePublishSyncWithLegacyHandlerStillWorks(t *testing.T) {
	pipe := NewPipe[int]()
	defer pipe.Close()

	_, err := pipe.SubscribeWithResponse(func(payload int) (any, error) {
		return payload * 3, nil
	})
	require.NoError(t, err)

	ctx := context.Background()
	res, err := pipe.PublishSyncAllWithContext(ctx, 2)
	require.NoError(t, err)
	require.Len(t, res, 1)
}

func TestPipePublishSyncWithPanic(t *testing.T) {
	pipe := NewPipe[string]()
	defer pipe.Close()

	// 添加正常处理器
	normalHandler := func(payload string) (any, error) {
		return payload + "_normal", nil
	}
	_, _ = pipe.SubscribeWithResponse(normalHandler)

	// 添加会panic的处理器
	panicHandler := func(payload string) (any, error) {
		panic("intentional panic for testing")
	}
	_, _ = pipe.SubscribeWithResponse(panicHandler)

	result, err := pipe.PublishSyncAll("test_payload")
	if err != nil {
		t.Fatalf("PublishSyncAll failed: %v", err)
	}

	if result.Success {
		t.Fatalf("Expected success=false due to panic, got %v", result.Success)
	}

	if result.HandlerCount != 2 {
		t.Fatalf("Expected 2 handlers, got %d", result.HandlerCount)
	}

	if result.SuccessCount != 1 {
		t.Fatalf("Expected 1 success, got %d", result.SuccessCount)
	}

	if result.FailureCount != 1 {
		t.Fatalf("Expected 1 failure due to panic, got %d", result.FailureCount)
	}

	// 检查panic恢复的错误信息
	found := false
	for _, res := range result.Results {
		if !res.Success && res.Error != nil {
			if strings.Contains(res.Error.Error(), "panic recovered: intentional panic for testing") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("Expected to find panic recovery error in results")
	}
}

func TestPipePublishSyncNoResponseHandlers(t *testing.T) {
	pipe := NewPipe[string]()
	defer pipe.Close()

	// 只添加普通处理器，不添加响应式处理器
	normalHandler := func(payload string) {
		// 普通处理器，无返回值
	}
	require.NoError(t, pipe.Subscribe(normalHandler))

	result, err := pipe.PublishSyncAll("test_payload")
	if err != nil {
		t.Fatalf("PublishSyncAll failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success=true for no response handlers, got %v", result.Success)
	}

	if result.HandlerCount != 0 {
		t.Fatalf("Expected 0 response handlers, got %d", result.HandlerCount)
	}
}

func TestPipeConcurrentResponsePublish(t *testing.T) {
	pipe := NewPipe[int]()
	defer pipe.Close()

	// 添加多个响应式处理器
	for range 5 {
		handler := func(payload int) (any, error) {
			time.Sleep(10 * time.Millisecond) // 模拟处理时间
			return payload * 2, nil
		}
		_, _ = pipe.SubscribeWithResponse(handler)
	}

	var wg sync.WaitGroup
	results := make([]*PipeSyncResult, 10)
	errors := make([]error, 10)

	// 并发发布
	for i := range 10 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			result, err := pipe.PublishSyncAll(index)
			results[index] = result
			errors[index] = err
		}(i)
	}

	wg.Wait()

	// 检查所有发布都成功
	for i, result := range results {
		if errors[i] != nil {
			t.Fatalf("Concurrent publish %d failed: %v", i, errors[i])
		}
		if !result.Success {
			t.Fatalf("Concurrent publish %d not successful", i)
		}
		if result.HandlerCount != 5 {
			t.Fatalf("Concurrent publish %d expected 5 handlers, got %d", i, result.HandlerCount)
		}
	}
}

// 基准测试
func BenchmarkPipePublishSyncAll(b *testing.B) {
	pipe := NewPipe[string]()
	defer pipe.Close()

	// 添加5个响应式处理器
	for range 5 {
		handler := func(payload string) (any, error) {
			return payload + "_response", nil
		}
		_, _ = pipe.SubscribeWithResponse(handler)
	}

	b.ResetTimer()
	for range b.N {
		_, err := pipe.PublishSyncAll("benchmark_payload")
		if err != nil {
			b.Fatalf("PublishSyncAll failed: %v", err)
		}
	}
}

func BenchmarkPipeTraditionalPublishSync(b *testing.B) {
	pipe := NewPipe[string]()
	defer pipe.Close()

	// 添加5个普通处理器
	for range 5 {
		handler := func(payload string) {
			// 传统处理器，无返回值
		}
		if err := pipe.Subscribe(handler); err != nil {
			b.Fatalf("订阅失败: %v", err)
		}
	}

	b.ResetTimer()
	for range b.N {
		err := pipe.PublishSync("benchmark_payload")
		if err != nil {
			b.Fatalf("PublishSync failed: %v", err)
		}
	}
}
