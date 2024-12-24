package eventbus

// singleton 是一个指向无缓冲 EventBus 实例的指针，该实例将在必要时创建。
var singleton *EventBus

func init() {
	ResetSingleton()
}

// ResetSingleton 重置单例对象。如果单例对象不为 nil，
// 它会首先关闭旧的单例对象，然后创建一个新的单例实例。
func ResetSingleton() {
	if singleton != nil {
		singleton.Close()
	}
	singleton = New()
}

// Unsubscribe 从一个主题中移除已定义的处理器。
// 如果该主题没有订阅者，返回错误。
func Unsubscribe(topic string, handler any) error {
	return singleton.Unsubscribe(topic, handler)
}

// Subscribe 订阅一个主题，如果处理器不是函数则返回错误。
// 处理器必须有两个参数：第一个参数必须是字符串，
// 第二个参数的类型必须与 `Publish()` 中的 payload 类型一致。
func Subscribe(topic string, handler any) error {
	return singleton.Subscribe(topic, handler)
}

// Publish 触发为某个主题定义的处理器。`payload` 参数将传递给处理器。
// payload 的类型必须与 `Subscribe()` 中处理器的第二个参数类型一致。
func Publish(topic string, payload any) error {
	return singleton.Publish(topic, payload)
}

// PublishSync 是 Publish 的同步版本，它会触发为某个主题定义的处理器，并使用给定的 payload。
// payload 的类型必须与 `Subscribe()` 中处理器的第二个参数类型一致。
func PublishSync(topic string, payload any) error {
	return singleton.Publish(topic, payload)
}

// Close 关闭 EventBus 的单例实例。
func Close() {
	if singleton != nil {
		singleton.Close()
		singleton = nil
	}
}
