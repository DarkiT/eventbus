package eventbus

// EventFilter 定义事件过滤器接口
type EventFilter interface {
	// Filter 返回 true 表示事件可以继续传递，false 表示拦截该事件
	Filter(topic string, payload any) bool
}

// Middleware 定义事件处理中间件接口
type Middleware interface {
	// Before 在事件处理前执行，可以修改payload
	Before(topic string, payload any) any
	// After 在事件处理后执行
	After(topic string, payload any)
}

// Subscription 定义带优先级的订阅信息
type Subscription struct {
	Handler  any
	Priority int
}
