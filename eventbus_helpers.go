package eventbus

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"
)

var (
	contextType            = reflect.TypeFor[context.Context]()
	errorInterface         = reflect.TypeFor[error]()
	errPublishAnySatisfied = errors.New("publish sync any satisfied")
)

// isBatchError 判断错误是否为 BatchError
func isBatchError(err error) bool {
	var be BatchError
	return errors.As(err, &be)
}

// newHandlerInfo 构建处理器信息，并校验函数签名
func newHandlerInfo(handler any, priority int, isResponse bool) (*HandlerInfo, error) {
	if handler == nil {
		return nil, ErrHandlerIsNotFunc
	}

	// 快速路径：检查是否为具体的ResponseHandler类型
	if isResponse {
		switch h := handler.(type) {
		case ResponseHandler:
			fn := reflect.ValueOf(h)
			id := fn.Pointer()
			return &HandlerInfo{
				fn:             fn,
				responseFn:     h,
				Priority:       priority,
				ID:             id,
				HandlerID:      "handler_" + strconv.FormatUint(uint64(id), 10),
				IsResponse:     true,
				expectsContext: false,
			}, nil
		case ResponseHandlerWithContext:
			fn := reflect.ValueOf(h)
			id := fn.Pointer()
			return &HandlerInfo{
				fn:             fn,
				responseCtxFn:  h,
				Priority:       priority,
				ID:             id,
				HandlerID:      "handler_" + strconv.FormatUint(uint64(id), 10),
				IsResponse:     true,
				expectsContext: true,
			}, nil
		case func(topic string, payload any) (any, error):
			fn := reflect.ValueOf(h)
			id := fn.Pointer()
			return &HandlerInfo{
				fn:             fn,
				responseFn:     h,
				Priority:       priority,
				ID:             id,
				HandlerID:      "handler_" + strconv.FormatUint(uint64(id), 10),
				IsResponse:     true,
				expectsContext: false,
			}, nil
		case func(ctx context.Context, topic string, payload any) (any, error):
			fn := reflect.ValueOf(h)
			id := fn.Pointer()
			return &HandlerInfo{
				fn:             fn,
				responseCtxFn:  h,
				Priority:       priority,
				ID:             id,
				HandlerID:      "handler_" + strconv.FormatUint(uint64(id), 10),
				IsResponse:     true,
				expectsContext: true,
			}, nil
		}
	}

	// 普通处理器快路径：常见签名可绕过反射调用
	if !isResponse {
		switch h := handler.(type) {
		case func(topic string, payload any):
			fn := reflect.ValueOf(h)
			id := fn.Pointer()
			return &HandlerInfo{
				fn:             fn,
				handlerFn:      h,
				Priority:       priority,
				ID:             id,
				HandlerID:      "handler_" + strconv.FormatUint(uint64(id), 10),
				IsResponse:     false,
				expectsContext: false,
			}, nil
		case func(ctx context.Context, topic string, payload any):
			fn := reflect.ValueOf(h)
			id := fn.Pointer()
			return &HandlerInfo{
				fn:             fn,
				handlerCtxFn:   h,
				Priority:       priority,
				ID:             id,
				HandlerID:      "handler_" + strconv.FormatUint(uint64(id), 10),
				IsResponse:     false,
				expectsContext: true,
			}, nil
		}
	}

	// 通用路径：使用反射验证
	fn := reflect.ValueOf(handler)
	if !fn.IsValid() || fn.Kind() != reflect.Func {
		return nil, ErrHandlerIsNotFunc
	}

	typ := fn.Type()
	paramIndex := 0
	expectsContext := false
	if typ.NumIn() > 0 && typ.In(0) == contextType {
		expectsContext = true
		paramIndex = 1
	}

	// 需要 topic 和 payload 两个参数
	if typ.NumIn() != paramIndex+2 {
		return nil, ErrHandlerParamNum
	}
	if typ.In(paramIndex).Kind() != reflect.String {
		return nil, ErrHandlerFirstParam
	}

	if isResponse {
		if typ.NumOut() != 2 {
			return nil, ErrResponseReturnNum
		}
		if !typ.Out(1).Implements(errorInterface) {
			return nil, ErrResponseReturnType
		}
	} else if typ.NumOut() != 0 {
		return nil, ErrHandlerReturnNum
	}

	id := fn.Pointer()
	return &HandlerInfo{
		fn:             fn,
		Priority:       priority,
		ID:             id,
		HandlerID:      "handler_" + strconv.FormatUint(uint64(id), 10),
		IsResponse:     isResponse,
		expectsContext: expectsContext,
	}, nil
}

// callHandler 调用普通处理器，优先走快路径，反射仅用于通用兜底。
func callHandler(info *HandlerInfo, ctx context.Context, topic string, payload any) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if info == nil {
		return false, nil
	}
	if info.filter != nil {
		allowed, err := safeFilterInvoke(info.filter, topic, payload)
		if err != nil {
			return false, err
		}
		if !allowed {
			return false, nil
		}
	}

	if info.handlerFn != nil {
		info.handlerFn(topic, payload)
		return true, nil
	}
	if info.handlerCtxFn != nil {
		info.handlerCtxFn(ctx, topic, payload)
		return true, nil
	}

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

	info.fn.Call(args[:n])
	return true, nil
}

// compareHandlerOrder 比较两个处理器的执行顺序：优先级高的先执行，同优先级按订阅顺序稳定排序。
func compareHandlerOrder(a, b *HandlerInfo) int {
	if a == nil || b == nil {
		switch {
		case a == nil && b == nil:
			return 0
		case a == nil:
			return 1
		default:
			return -1
		}
	}

	if a.Priority != b.Priority {
		return b.Priority - a.Priority
	}

	if a.Sequence != b.Sequence {
		if a.Sequence < b.Sequence {
			return -1
		}
		return 1
	}

	if a.ID < b.ID {
		return -1
	}
	if a.ID > b.ID {
		return 1
	}
	return 0
}

// newPublishTimeoutError 统一构造超时错误，保留 ErrPublishTimeout 供 errors.Is 判断。
func newPublishTimeoutError(start time.Time) error {
	return fmt.Errorf("%w after %v", ErrPublishTimeout, time.Since(start))
}
