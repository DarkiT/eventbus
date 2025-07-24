package eventbus

import (
	"context"
	"sync"
)

// singleton 全局单例实例
var (
	singleton *EventBus
	once      sync.Once
	mu        sync.RWMutex
)

// getSingleton 获取单例实例，使用懒加载
func getSingleton() *EventBus {
	once.Do(func() {
		singleton = New(defaultBufferSize)
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
}

// Subscribe 订阅主题，使用全局单例
func Subscribe(topic string, handler any) error {
	return getSingleton().Subscribe(topic, handler)
}

// SubscribeWithPriority 带优先级订阅主题，使用全局单例
func SubscribeWithPriority(topic string, handler any, priority int) error {
	return getSingleton().SubscribeWithPriority(topic, handler, priority)
}

// Unsubscribe 取消订阅主题，使用全局单例
func Unsubscribe(topic string, handler any) error {
	return getSingleton().Unsubscribe(topic, handler)
}

// Publish 异步发布消息，使用全局单例
func Publish(topic string, payload any) error {
	return getSingleton().Publish(topic, payload)
}

// PublishSync 同步发布消息，使用全局单例
func PublishSync(topic string, payload any) error {
	return getSingleton().PublishSync(topic, payload)
}

// PublishWithContext 带上下文发布消息，使用全局单例
func PublishWithContext(ctx context.Context, topic string, payload any) error {
	return getSingleton().PublishWithContext(ctx, topic, payload)
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
func Use(middleware Middleware) {
	getSingleton().Use(middleware)
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
