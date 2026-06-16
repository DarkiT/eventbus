package eventbus

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"
)

// ReliableHandler 可靠处理器：返回 error 表示处理失败需重试，返回 nil 表示成功。
//
// ctx 贯穿重试全过程——drain（Shutdown）、超时或显式取消都会中断退避等待，
// 因此处理器内部应尊重 ctx 以便及时退出。panic 会被 recover 并视为可重试错误。
type ReliableHandler func(ctx context.Context, topic string, payload any) error

// BackoffFunc 根据尝试次数（从 1 开始，1 表示首次失败后等待的时长）返回
// 下一次重试前的退避时长。返回 0 或负值表示立即重试。
type BackoffFunc func(attempt int) time.Duration

// DeadLetterHandler 在所有重试尝试均失败、或遇到不可重试错误后调用，
// 传入最后一次错误，便于落库、告警或转入外部死信队列。
type DeadLetterHandler func(topic string, payload any, err error)

// retryConfig 可靠订阅的重试配置，由 RetryOption 填充。
type retryConfig struct {
	maxAttempts int
	backoff     BackoffFunc
	retryIf     func(error) bool
	deadLetter  DeadLetterHandler
}

// RetryOption 以 functional options 模式配置可靠订阅的重试行为。
type RetryOption func(*retryConfig)

// defaultRetryConfig 返回生产可用的默认配置：3 次尝试、指数退避带 jitter、默认全重试。
func defaultRetryConfig() *retryConfig {
	return &retryConfig{
		maxAttempts: 3,
		backoff:     ExponentialBackoff(100*time.Millisecond, time.Second),
		retryIf:     func(error) bool { return true },
	}
}

// applyRetryOptions 在默认配置之上应用用户选项，返回不可变的最终配置。
func applyRetryOptions(opts []RetryOption) *retryConfig {
	cfg := defaultRetryConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	if cfg.maxAttempts < 1 {
		cfg.maxAttempts = 1
	}
	if cfg.backoff == nil {
		cfg.backoff = ConstantBackoff(0)
	}
	if cfg.retryIf == nil {
		cfg.retryIf = func(error) bool { return true }
	}
	return cfg
}

// WithMaxAttempts 设置最大尝试次数（含首次执行），小于 1 时取 1。
func WithMaxAttempts(n int) RetryOption {
	return func(c *retryConfig) {
		if n >= 1 {
			c.maxAttempts = n
		}
	}
}

// WithBackoff 设置退避策略。传 nil 时保留默认。
func WithBackoff(b BackoffFunc) RetryOption {
	return func(c *retryConfig) {
		if b != nil {
			c.backoff = b
		}
	}
}

// WithRetryIf 设置错误过滤：返回 false 的错误不重试，直接触发死信。
// 未设置时默认对所有错误重试。可用于跳过参数校验、权限等不可恢复错误。
func WithRetryIf(f func(error) bool) RetryOption {
	return func(c *retryConfig) {
		if f != nil {
			c.retryIf = f
		}
	}
}

// WithDeadLetter 设置死信回调：重试耗尽或遇到不可重试错误时调用。
func WithDeadLetter(h DeadLetterHandler) RetryOption {
	return func(c *retryConfig) {
		c.deadLetter = h
	}
}

// ConstantBackoff 返回固定退避策略：每次重试前等待 d。
func ConstantBackoff(d time.Duration) BackoffFunc {
	return func(int) time.Duration { return d }
}

// LinearBackoff 返回线性退避策略：第 n 次重试前等待 start*n。
func LinearBackoff(start time.Duration) BackoffFunc {
	return func(attempt int) time.Duration {
		if attempt < 1 {
			attempt = 1
		}
		return start * time.Duration(attempt)
	}
}

// ExponentialBackoff 返回指数退避策略：第 n 次重试前等待 start*2^(n-1)，
// 叠加 ±25% 随机抖动（防惊群），并封顶为 max。start/max 非正时退化为立即重试。
func ExponentialBackoff(start, max time.Duration) BackoffFunc {
	return func(attempt int) time.Duration {
		if start <= 0 || max <= 0 {
			return 0
		}
		if attempt < 1 {
			attempt = 1
		}
		d := start
		for i := 1; i < attempt; i++ {
			next := d * 2
			if next <= d || next >= max { // 溢出或已达上限
				d = max
				break
			}
			d = next
		}
		if d > max {
			d = max
		}
		return jitter(d, max)
	}
}

// jitter 在 [0.75d, 1.25d] 区间内叠加随机抖动并封顶 max。
// 使用 math/rand/v2 顶层函数，并发安全且自动播种。
func jitter(d, max time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	delta := int64(d / 4)
	off := rand.Int64N(delta*2+1) - delta // [-delta, +delta]
	jittered := d + time.Duration(off)
	switch {
	case jittered < 0:
		return 0
	case jittered > max:
		return max
	default:
		return jittered
	}
}

// callReliableWithRetry 执行可靠处理器，按配置同步重试；所有尝试失败或遇到
// 不可重试错误时触发死信回调。全程尊重 ctx：drain/超时/取消会中断退避等待。
// 返回最后一次错误（成功返回 nil）。
func callReliableWithRetry(info *HandlerInfo, ctx context.Context, topic string, payload any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := info.retryConfig
	if cfg == nil {
		cfg = defaultRetryConfig()
	}

	var lastErr error
	exhausted := false
	for attempt := 1; attempt <= cfg.maxAttempts; attempt++ {
		// 每次尝试前检查 ctx，使 drain/取消尽快生效
		if cerr := ctx.Err(); cerr != nil {
			lastErr = cerr
			exhausted = true
			break
		}
		callErr := safeReliableCall(info, ctx, topic, payload)
		if callErr == nil {
			return nil
		}
		lastErr = callErr
		// 不可重试错误直接结束
		if cfg.retryIf != nil && !cfg.retryIf(callErr) {
			exhausted = true
			break
		}
		if attempt >= cfg.maxAttempts {
			exhausted = true
			break
		}
		// 退避等待，可被 ctx 中断
		if backoff := cfg.backoff(attempt); backoff > 0 {
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				lastErr = ctx.Err()
				exhausted = true
				goto done
			case <-timer.C:
			}
		}
	}

done:
	if exhausted && cfg.deadLetter != nil && lastErr != nil {
		invokeDeadLetter(cfg.deadLetter, topic, payload, lastErr)
	}
	return lastErr
}

// safeReliableCall 在 recover 保护下调用可靠处理器，panic 转为 error 以参与重试。
func safeReliableCall(info *HandlerInfo, ctx context.Context, topic string, payload any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("reliable handler panic: %v", r)
		}
	}()
	return info.reliableFn(ctx, topic, payload)
}

// invokeDeadLetter 在 recover 保护下调用死信回调，避免回调异常拖垮投递路径。
func invokeDeadLetter(h DeadLetterHandler, topic string, payload any, err error) {
	defer func() { _ = recover() }()
	h(topic, payload, err)
}
