package adapter

import "context"

type remoteContextKey struct{}

// MessageFromContext 从本地 EventBus 的 context handler 中取出远端消息信封。
func MessageFromContext(ctx context.Context) (Message, bool) {
	if ctx == nil {
		return Message{}, false
	}
	msg, ok := ctx.Value(remoteContextKey{}).(Message)
	if !ok {
		return Message{}, false
	}
	return msg.Clone(), true
}

func markRemoteContext(ctx context.Context, msg Message) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, remoteContextKey{}, msg.Clone())
}
