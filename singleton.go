package eventbus

import (
	"context"
	"sync"
)

// DefaultBus 全局默认事件总线实例
// 建议：对于生产环境，推荐显式创建和管理 EventBus 实例以获得更好的控制和测试性
var DefaultBus = New(defaultBufferSize)

// singleton 全局单例实例（内部使用）
var (
	singleton *EventBus
	once      sync.Once
	mu        sync.RWMutex
)

// getSingleton 获取单例实例，使用懒加载
func getSingleton() *EventBus {
	once.Do(func() {
		singleton = DefaultBus
	})
	return singleton
}

// ResetSingleton 重置单例对象，主要用于测试
func ResetSingleton() {
	mu.Lock()
	defer mu.Unlock()

	if singleton != nil {
		singleton.Close()
	}

	// 重置 once，允许重新创建
	once = sync.Once{}
	singleton = nil

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

// SubscribeWithResponse 响应式订阅，使用全局单例
func SubscribeWithResponse(topic string, handler ResponseHandler) error {
	return getSingleton().SubscribeWithResponse(topic, handler)
}

// SubscribeWithResponseContext 带上下文的响应式订阅，使用全局单例
func SubscribeWithResponseContext(topic string, handler ResponseHandlerWithContext) error {
	return getSingleton().SubscribeWithResponseContext(topic, handler)
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
func GetStats() map[string]interface{} {
	return getSingleton().GetStats()
}

// NewGroup 基于全局单例创建主题组
func NewGroup(prefix string) *TopicGroup {
	return getSingleton().NewGroup(prefix)
}

// Close 关闭全局单例
func Close() {
	mu.Lock()
	defer mu.Unlock()

	if singleton != nil {
		singleton.Close()
		singleton = nil
		once = sync.Once{} // 重置 once，允许重新创建
	}
}

// HealthCheck 健康检查，使用全局单例
func HealthCheck() error {
	return getSingleton().HealthCheck()
}
