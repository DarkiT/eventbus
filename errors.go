package eventbus

import (
	"errors"
	"fmt"
)

var (
	// ErrHandlerIsNotFunc 当处理器不是一个函数时返回
	ErrHandlerIsNotFunc = errors.New("handler is not a function")

	// ErrHandlerParamNum 当处理器的参数数量不等于两个时返回
	ErrHandlerParamNum = errors.New("handler must have exactly two parameters")

	// ErrHandlerFirstParam 当处理器的第一个参数不是字符串时返回
	ErrHandlerFirstParam = errors.New("handler's first parameter must be string")

	// ErrHandlerReturnNum 当普通处理器返回值数量非法时返回
	ErrHandlerReturnNum = errors.New("handler must not return values")

	// ErrResponseReturnNum 当响应式处理器返回值数量非法时返回
	ErrResponseReturnNum = errors.New("response handler must return (any, error)")

	// ErrResponseReturnType 当响应式处理器第二个返回值不是 error 时返回
	ErrResponseReturnType = errors.New("response handler second return value must be error")

	// ErrNoSubscriber 当某个主题没有订阅者时返回
	ErrNoSubscriber = errors.New("no subscriber found for topic")

	// ErrChannelClosed 当尝试使用已关闭的通道时返回
	ErrChannelClosed = errors.New("channel is closed")

	// ErrPublishTimeout 当发布操作超时时返回
	ErrPublishTimeout = errors.New("publish operation timed out")

	// ErrInvalidTopic 当主题格式无效时返回
	ErrInvalidTopic = errors.New("invalid topic format")

	// ErrEventBusClosed 当事件总线已关闭时返回
	ErrEventBusClosed = errors.New("event bus is closed")
)

// WrapError 用附加的上下文信息包装错误
func WrapError(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}
