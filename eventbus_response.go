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

// ResponseHandler 响应式处理器（不带context参数）
// 签名: func(topic string, payload any) (any, error)
type ResponseHandler func(topic string, payload any) (any, error)

// ResponseHandlerWithContext 响应式处理器（带context参数）
// 签名: func(ctx context.Context, topic string, payload any) (any, error)
type ResponseHandlerWithContext func(ctx context.Context, topic string, payload any) (any, error)

// HandlerResult 处理器执行结果
type HandlerResult struct {
	HandlerID string        `json:"handler_id"` // 处理器唯一标识
	Success   bool          `json:"success"`    // 执行是否成功
	Result    any           `json:"result"`     // 返回值
	Error     error         `json:"error"`      // 错误信息
	Duration  time.Duration `json:"duration"`   // 执行耗时
}

// SyncResult 同步发布结果
type SyncResult struct {
	Success      bool            `json:"success"`       // 整体是否成功
	HandlerCount int             `json:"handler_count"` // 处理器总数
	SuccessCount int             `json:"success_count"` // 成功数量
	FailureCount int             `json:"failure_count"` // 失败数量
	Results      []HandlerResult `json:"results"`       // 详细结果
	TotalTime    time.Duration   `json:"total_time"`    // 总耗时
}

// publishSyncWithStrategy 核心同步发布实现
func (e *EventBus) publishSyncWithStrategy(
	ctx context.Context,
	topic string,
	payload any,
	strategy string,
	timeout time.Duration,
) (*SyncResult, error) {
	if err := e.beginPublish(); err != nil {
		return nil, err
	}
	defer e.endPublish()

	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	startTime := time.Now()
	originalTopic := topic
	if err := validateTopic(topic); err != nil {
		e.traceError(topic, err)
		return nil, err
	}
	normalizedTopic, err := normalizeTopic(topic)
	if err != nil {
		e.traceError(topic, err)
		return nil, err
	}

	metadata := PublishMetadata{
		Timestamp: startTime,
		Async:     false,
		QueueSize: e.bufferSize,
	}
	e.tracePublish(originalTopic, payload, metadata)

	// 获取过滤器和中间件快照（无锁读取，COW 优化）
	filters := e.getFilters()
	middlewares := e.getMiddlewares()

	// 过滤与中间件编排与异步 / 批量路径共用同一实现；After 失败仅记录，不影响响应式结果。
	payload, allowed, after, fmErr := applyFiltersAndMiddlewares(filters, middlewares, normalizedTopic, payload)
	if fmErr != nil {
		e.traceError(normalizedTopic, fmErr)
		return nil, fmErr
	}
	if !allowed {
		totalTime := time.Since(startTime)
		result := &SyncResult{
			Success:      true,
			HandlerCount: 0,
			SuccessCount: 0,
			FailureCount: 0,
			Results:      nil,
			TotalTime:    totalTime,
		}
		e.traceComplete(originalTopic, CompleteMetadata{
			StartTime:      startTime,
			EndTime:        startTime.Add(totalTime),
			ProcessingTime: totalTime,
			HandlerCount:   0,
			Success:        true,
		})
		return result, nil
	}
	defer func() {
		if afterErr := after(); afterErr != nil {
			e.traceError(normalizedTopic, afterErr)
		}
	}()

	// 合并超时：始终尊重事件总线级别的超时上限
	var deadlineCancel context.CancelFunc
	if timeout > 0 {
		busDeadline := time.Now().Add(timeout)
		if d, ok := ctx.Deadline(); ok {
			if busDeadline.Before(d) {
				ctx, deadlineCancel = context.WithDeadlineCause(ctx, busDeadline, context.DeadlineExceeded)
			}
		} else {
			ctx, deadlineCancel = context.WithDeadlineCause(ctx, busDeadline, context.DeadlineExceeded)
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

	// 获取所有响应处理器并按优先级降序排序，保证跨通道也稳定
	responseHandlers := e.getResponseHandlers(normalizedTopic)
	slices.SortStableFunc(responseHandlers, compareHandlerOrder)
	if len(responseHandlers) == 0 {
		totalTime := time.Since(startTime)
		success := strategy == "all"
		result := &SyncResult{
			Success:      success,
			HandlerCount: 0,
			TotalTime:    totalTime,
		}
		e.traceComplete(originalTopic, CompleteMetadata{
			StartTime:      startTime,
			EndTime:        startTime.Add(totalTime),
			ProcessingTime: totalTime,
			HandlerCount:   0,
			Success:        success,
		})
		return result, nil
	}

	// 执行处理器并收集结果
	results := make([]HandlerResult, len(responseHandlers))
	var wg sync.WaitGroup
	var resultMu sync.Mutex
	successCount := int32(0)
	successSignal := make(chan struct{}, 1)

	for i, handlerInfo := range responseHandlers {
		results[i].HandlerID = handlerInfo.HandlerID
	}

	buildResult := func(failureErr error) *SyncResult {
		resultMu.Lock()
		snapshot := make([]HandlerResult, len(results))
		copy(snapshot, results)
		for i := range snapshot {
			if snapshot[i].HandlerID == "" {
				snapshot[i].HandlerID = responseHandlers[i].HandlerID
			}
			if !snapshot[i].Success && snapshot[i].Error == nil && failureErr != nil {
				snapshot[i].Error = failureErr
			}
		}
		resultMu.Unlock()

		successes := 0
		for _, handlerResult := range snapshot {
			if handlerResult.Success {
				successes++
			}
		}

		overallSuccess := successes == len(responseHandlers)
		if strategy == "any" {
			overallSuccess = successes > 0
		}

		return &SyncResult{
			Success:      overallSuccess,
			HandlerCount: len(responseHandlers),
			SuccessCount: successes,
			FailureCount: len(responseHandlers) - successes,
			Results:      snapshot,
			TotalTime:    time.Since(startTime),
		}
	}

	traceResult := func(result *SyncResult) {
		if result == nil {
			return
		}
		if result.TotalTime > slowConsumerThreshold {
			e.traceSlowConsumer(originalTopic, result.TotalTime)
		}
		e.traceComplete(originalTopic, CompleteMetadata{
			StartTime:      startTime,
			EndTime:        startTime.Add(result.TotalTime),
			ProcessingTime: result.TotalTime,
			HandlerCount:   result.HandlerCount,
			Success:        result.Success,
		})
	}

	for i, handlerInfo := range responseHandlers {
		wg.Add(1)
		go func(idx int, info *HandlerInfo) {
			defer wg.Done()
			if !info.fn.IsValid() {
				return
			}

			defer func() {
				if r := recover(); r != nil {
					resultMu.Lock()
					results[idx] = HandlerResult{
						HandlerID: info.HandlerID,
						Success:   false,
						Error:     fmt.Errorf("handler panic: %v\nstack: %s", r, debug.Stack()),
						Duration:  0,
					}
					resultMu.Unlock()
				}
			}()

			execStart := time.Now()
			result, err := callResponseHandler(info, runCtx, originalTopic, payload)
			duration := time.Since(execStart)

			success := err == nil
			resultMu.Lock()
			results[idx] = HandlerResult{
				HandlerID: info.HandlerID,
				Success:   success,
				Result:    result,
				Error:     err,
				Duration:  duration,
			}
			resultMu.Unlock()

			if success {
				atomic.AddInt32(&successCount, 1)
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

	select {
	case <-doneCh:
	case <-runCtx.Done():
		cause := context.Cause(runCtx)
		switch {
		case errors.Is(cause, errPublishAnySatisfied):
			select {
			case <-doneCh:
			case <-deadlineCtx.Done():
				deadlineErr := context.Cause(deadlineCtx)
				if deadlineErr == nil {
					deadlineErr = deadlineCtx.Err()
				}
				if errors.Is(deadlineErr, context.DeadlineExceeded) {
					deadlineErr = newPublishTimeoutError(startTime)
				}
				result := buildResult(deadlineErr)
				traceResult(result)
				return result, nil
			}
		case errors.Is(cause, context.DeadlineExceeded):
			// deadline 到点时处理器可能恰好已全部完成：若已收敛则按结果返回，避免误报超时。
			select {
			case <-doneCh:
				// 已全部完成，落到下方正常收敛路径
			default:
				e.traceError(normalizedTopic, cause)
				return nil, newPublishTimeoutError(startTime)
			}
		default:
			if strategy == "any" {
				if atomic.LoadInt32(&successCount) > 0 {
					result := buildResult(nil)
					traceResult(result)
					return result, nil
				}

				timer := time.NewTimer(20 * time.Millisecond)
				select {
				case <-successSignal:
					timer.Stop()
					result := buildResult(nil)
					traceResult(result)
					return result, nil
				case <-doneCh:
					timer.Stop()
					result := buildResult(nil)
					traceResult(result)
					return result, nil
				case <-timer.C:
				}

				if atomic.LoadInt32(&successCount) > 0 {
					result := buildResult(nil)
					traceResult(result)
					return result, nil
				}
			}
			e.traceError(normalizedTopic, cause)
			return nil, cause
		}
	}

	totalTime := time.Since(startTime)
	finalSuccessCount := int(atomic.LoadInt32(&successCount))

	// 根据策略判断成功
	var overallSuccess bool
	switch strategy {
	case "all":
		overallSuccess = finalSuccessCount == len(responseHandlers)
	case "any":
		overallSuccess = finalSuccessCount > 0
	default:
		return nil, fmt.Errorf("unknown strategy: %s", strategy)
	}

	result := buildResult(nil)
	result.Success = overallSuccess
	result.SuccessCount = finalSuccessCount
	result.FailureCount = len(responseHandlers) - finalSuccessCount
	result.TotalTime = totalTime
	traceResult(result)

	return result, nil
}

type firstSuccessOutcome struct {
	result any
	err    error
}

func (e *EventBus) publishSyncAnyValueInternal(
	ctx context.Context,
	topic string,
	payload any,
	timeout time.Duration,
) (any, error) {
	if err := e.beginPublish(); err != nil {
		return nil, err
	}
	defer e.endPublish()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	startTime := time.Now()
	originalTopic := topic
	if err := validateTopic(topic); err != nil {
		e.traceError(topic, err)
		return nil, err
	}
	normalizedTopic, err := normalizeTopic(topic)
	if err != nil {
		e.traceError(topic, err)
		return nil, err
	}

	metadata := PublishMetadata{
		Timestamp: startTime,
		Async:     false,
		QueueSize: e.bufferSize,
	}
	e.tracePublish(originalTopic, payload, metadata)

	filters := e.getFilters()
	middlewares := e.getMiddlewares()
	// 复用统一的过滤与中间件编排；After 失败仅记录，不影响 AnyValue 结果。
	payload, allowed, after, fmErr := applyFiltersAndMiddlewares(filters, middlewares, normalizedTopic, payload)
	if fmErr != nil {
		e.traceError(normalizedTopic, fmErr)
		return nil, fmErr
	}
	if !allowed {
		return nil, ErrNoSubscriber
	}
	defer func() {
		if afterErr := after(); afterErr != nil {
			e.traceError(normalizedTopic, afterErr)
		}
	}()

	var deadlineCancel context.CancelFunc
	if timeout > 0 {
		busDeadline := time.Now().Add(timeout)
		if d, ok := ctx.Deadline(); ok {
			if busDeadline.Before(d) {
				ctx, deadlineCancel = context.WithDeadlineCause(ctx, busDeadline, context.DeadlineExceeded)
			}
		} else {
			ctx, deadlineCancel = context.WithDeadlineCause(ctx, busDeadline, context.DeadlineExceeded)
		}
	}
	if deadlineCancel == nil {
		deadlineCancel = func() {}
	}

	ctx, cancel := context.WithCancelCause(ctx)
	defer func() {
		cancel(context.Canceled)
		deadlineCancel()
	}()

	responseHandlers := e.getResponseHandlers(normalizedTopic)
	slices.SortStableFunc(responseHandlers, compareHandlerOrder)
	if len(responseHandlers) == 0 {
		return nil, ErrNoSubscriber
	}

	outcomeCh := make(chan firstSuccessOutcome, len(responseHandlers))
	var wg sync.WaitGroup
	for _, handlerInfo := range responseHandlers {
		wg.Add(1)
		go func(info *HandlerInfo) {
			defer wg.Done()
			outcome := firstSuccessOutcome{}
			defer func() {
				if r := recover(); r != nil {
					outcome.err = fmt.Errorf("handler panic: %v\nstack: %s", r, debug.Stack())
				}
				outcomeCh <- outcome
			}()
			result, callErr := callResponseHandler(info, ctx, originalTopic, payload)
			outcome.result = result
			outcome.err = callErr
		}(handlerInfo)
	}

	cleanup := func(success bool) {
		go func() {
			wg.Wait()
			totalTime := time.Since(startTime)
			if totalTime > slowConsumerThreshold {
				e.traceSlowConsumer(originalTopic, totalTime)
			}
			e.traceComplete(originalTopic, CompleteMetadata{
				StartTime:      startTime,
				EndTime:        startTime.Add(totalTime),
				ProcessingTime: totalTime,
				HandlerCount:   len(responseHandlers),
				Success:        success,
			})
		}()
	}

	var firstErr error
	remaining := len(responseHandlers)
	for remaining > 0 {
		select {
		case outcome := <-outcomeCh:
			remaining--
			if outcome.err == nil {
				cancel(errPublishAnySatisfied)
				cleanup(true)
				return outcome.result, nil
			}
			if firstErr == nil {
				firstErr = outcome.err
			}
		case <-ctx.Done():
			cleanup(false)
			cause := context.Cause(ctx)
			switch {
			case errors.Is(cause, errPublishAnySatisfied):
				return nil, ErrNoSubscriber
			case errors.Is(cause, context.DeadlineExceeded):
				e.traceError(normalizedTopic, cause)
				return nil, newPublishTimeoutError(startTime)
			default:
				e.traceError(normalizedTopic, cause)
				return nil, cause
			}
		}
	}

	cleanup(false)
	if firstErr != nil {
		return nil, firstErr
	}
	return nil, ErrNoSubscriber
}

// callResponseHandler 调用响应式处理器，并解析返回值
func callResponseHandler(info *HandlerInfo, ctx context.Context, topic string, payload any) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if info.responseFn != nil {
		return info.responseFn(topic, payload)
	}
	if info.responseCtxFn != nil {
		return info.responseCtxFn(ctx, topic, payload)
	}

	// 使用栈上固定数组，避免每次调用分配 args 切片。
	var args [3]reflect.Value
	n := 0
	if info.expectsContext {
		args[n] = reflect.ValueOf(ctx)
		n++
	}
	args[n] = reflect.ValueOf(topic)
	n++
	args[n] = reflect.ValueOf(payload)
	n++

	outputs := info.fn.Call(args[:n])
	var result any
	if len(outputs) > 0 {
		result = outputs[0].Interface()
	}

	var err error
	if len(outputs) > 1 {
		errVal := outputs[1]
		if errVal.IsValid() && !errVal.IsNil() {
			err = errVal.Interface().(error)
		}
	}

	return result, err
}
