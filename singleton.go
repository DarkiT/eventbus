package eventbus

import (
	"context"
	"sync"
	"sync/atomic"
)

// DefaultBus 全局默认事件总线实例
// 建议：对于生产环境，推荐显式创建和管理 EventBus 实例以获得更好的控制和测试性
var DefaultBus = New(defaultBufferSize)

// singleton 全局单例实例（内部使用）
var (
	singletonVal atomic.Value
	initialized  atomic.Bool // 替代 sync.Once，支持安全重置
	mu           sync.RWMutex
)

// getSingleton 获取单例实例，使用懒加载
func getSingleton() *EventBus {
	if bus := loadSingleton(); bus != nil {
		return bus
	}

	mu.Lock()
	defer mu.Unlock()

	// 二次检查，避免重复创建
	if bus := loadSingleton(); bus != nil {
		return bus
	}

	// 使用 atomic.Bool 控制初始化，支持安全重置
	if initialized.CompareAndSwap(false, true) {
		storeSingleton(DefaultBus)
	}

	return loadSingleton()
}

// ResetSingleton 重置单例对象，主要用于测试
func ResetSingleton() {
	mu.Lock()
	defer mu.Unlock()

	if bus := loadSingleton(); bus != nil {
		bus.Close()
	}

	storeSingleton((*EventBus)(nil))
	initialized.Store(false) // 原子重置，支持重新创建

	// 重新创建 DefaultBus 实例，确保测试隔离
	DefaultBus = New(defaultBufferSize)
}

// Subscribe 订阅主题，使用全局单例
func Subscribe(topic string, handler any) error {
	return getSingleton().Subscribe(topic, handler)
}

// SubscribeWithPriority 带优先级订阅，使用全局单例
func SubscribeWithPriority(topic string, handler any, priority int) error {
	return getSingleton().SubscribeWithPriority(topic, handler, priority)
}

// SubscribeOnce 一次性订阅，使用全局单例
func SubscribeOnce(topic string, handler any) error {
	return getSingleton().SubscribeOnce(topic, handler)
}

// SubscribeOnceWithPriority 带优先级一次性订阅，使用全局单例
func SubscribeOnceWithPriority(topic string, handler any, priority int) error {
	return getSingleton().SubscribeOnceWithPriority(topic, handler, priority)
}

// SubscribeWithResponse 响应式订阅，使用全局单例
func SubscribeWithResponse(topic string, handler ResponseHandler) error {
	return getSingleton().SubscribeWithResponse(topic, handler)
}

// SubscribeWithResponseContext 带上下文的响应式订阅，使用全局单例
func SubscribeWithResponseContext(topic string, handler ResponseHandlerWithContext) error {
	return getSingleton().SubscribeWithResponseContext(topic, handler)
}

// SubscribeReliable 可靠订阅，使用全局单例。
func SubscribeReliable(topic string, handler ReliableHandler, opts ...RetryOption) error {
	return getSingleton().SubscribeReliable(topic, handler, opts...)
}

// Unsubscribe 取消订阅主题，使用全局单例
func Unsubscribe(topic string, handler any) error {
	return getSingleton().Unsubscribe(topic, handler)
}

// UnsubscribeAll 取消主题的所有订阅，使用全局单例
func UnsubscribeAll(topic string) error {
	return getSingleton().UnsubscribeAll(topic)
}

// Publish 异步发布消息，使用全局单例
func Publish(topic string, payload any) error {
	return getSingleton().Publish(topic, payload)
}

// PublishSync 同步发布消息，使用全局单例
func PublishSync(topic string, payload any) error {
	return getSingleton().PublishSync(topic, payload)
}

// PublishSyncAll 响应式同步发布（全部成功），使用全局单例
func PublishSyncAll(topic string, payload any) (*SyncResult, error) {
	return getSingleton().PublishSyncAll(topic, payload)
}

// PublishSyncAllWithContext 响应式同步发布（全部成功，透传上下文）
func PublishSyncAllWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error) {
	return getSingleton().PublishSyncAllWithContext(ctx, topic, payload)
}

// PublishSyncAny 响应式同步发布（任一成功），使用全局单例
func PublishSyncAny(topic string, payload any) (*SyncResult, error) {
	return getSingleton().PublishSyncAny(topic, payload)
}

// PublishSyncAnyWithContext 响应式同步发布（任一成功，透传上下文）
func PublishSyncAnyWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error) {
	return getSingleton().PublishSyncAnyWithContext(ctx, topic, payload)
}

// PublishSyncAnyValue 快速返回首个成功处理器的结果，使用全局单例。
func PublishSyncAnyValue(topic string, payload any) (any, error) {
	return getSingleton().PublishSyncAnyValue(topic, payload)
}

// PublishSyncAnyValueWithContext 快速返回首个成功处理器的结果，透传上下文。
func PublishSyncAnyValueWithContext(ctx context.Context, topic string, payload any) (any, error) {
	return getSingleton().PublishSyncAnyValueWithContext(ctx, topic, payload)
}

// PublishWithContext 带上下文发布消息，使用全局单例
func PublishWithContext(ctx context.Context, topic string, payload any) error {
	return getSingleton().PublishWithContext(ctx, topic, payload)
}

// PublishAsyncWithContext 带上下文异步发布，使用全局单例
func PublishAsyncWithContext(ctx context.Context, topic string, payload any) error {
	return getSingleton().PublishAsyncWithContext(ctx, topic, payload)
}

// PublishSyncWithContext 带上下文同步发布，使用全局单例
func PublishSyncWithContext(ctx context.Context, topic string, payload any) error {
	return getSingleton().PublishSyncWithContext(ctx, topic, payload)
}

// AddFilter 添加过滤器，使用全局单例
func AddFilter(filter EventFilter) {
	getSingleton().AddFilter(filter)
}

// SetTracer 设置追踪器，使用全局单例
func SetTracer(tracer EventTracer) {
	getSingleton().SetTracer(tracer)
}

// Use 添加中间件，使用全局单例
func Use(middleware IMiddleware) {
	getSingleton().Use(middleware)
}

// GetStats 获取统计信息，使用全局单例
func GetStats() map[string]any {
	return getSingleton().GetStats()
}

// NewGroup 基于全局单例创建主题组
func NewGroup(prefix string) *TopicGroup {
	return getSingleton().NewGroup(prefix)
}

// Close 关闭全局单例。
//
// Close 为永久关闭语义：关闭后底层单例处于已关闭状态，包级函数会持续返回
// ErrEventBusClosed。getSingleton 在单例为空时回落到 DefaultBus，而 Close 不重建
// DefaultBus，因此关闭后单例不可用。如需恢复为全新可用实例，请使用 ResetSingleton
// （它会重建 DefaultBus）。该行为由 Test_SingletonUnsubscribe 等契约锁定。
func Close() {
	mu.Lock()
	defer mu.Unlock()

	if bus := loadSingleton(); bus != nil {
		bus.Close()
	}
	storeSingleton((*EventBus)(nil))
	initialized.Store(false) // 重置标志；DefaultBus 仍为已关闭实例，恢复需 ResetSingleton
}

// Shutdown 优雅关闭全局单例。
//
// Shutdown 会关闭当前 DefaultBus/单例并进入永久关闭语义；如需测试或重建可用实例，
// 调用 ResetSingleton。
func Shutdown(ctx context.Context) error {
	mu.Lock()
	defer mu.Unlock()

	bus := loadSingleton()
	if bus == nil && initialized.CompareAndSwap(false, true) {
		storeSingleton(DefaultBus)
		bus = DefaultBus
	}
	if bus == nil {
		return nil
	}
	err := bus.Shutdown(ctx)
	storeSingleton((*EventBus)(nil))
	initialized.Store(false)
	return err
}

// HealthCheck 健康检查，使用全局单例
func HealthCheck() error {
	return getSingleton().HealthCheck()
}

// loadSingleton 统一从 atomic.Value 读取并做类型断言
func loadSingleton() *EventBus {
	v := singletonVal.Load()
	if v == nil {
		return nil
	}
	bus, _ := v.(*EventBus)
	return bus
}

// storeSingleton 封装 atomic.Value.Store，便于统一管理
func storeSingleton(bus *EventBus) {
	singletonVal.Store(bus)
}
