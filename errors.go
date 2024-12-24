package eventbus

import (
	"errors"
	"fmt"
)

var (
	// ErrHandlerIsNotFunc is returned when the handler is not a function
	ErrHandlerIsNotFunc = errors.New("handler is not a function")

	// ErrHandlerParamNum is returned when the handler doesn't have exactly two parameters
	ErrHandlerParamNum = errors.New("handler must have exactly two parameters")

	// ErrHandlerFirstParam is returned when the handler's first parameter is not string
	ErrHandlerFirstParam = errors.New("handler's first parameter must be string")

	// ErrNoSubscriber is returned when no subscriber exists for a topic
	ErrNoSubscriber = errors.New("no subscriber found for topic")

	// ErrChannelClosed is returned when trying to use a closed channel
	ErrChannelClosed = errors.New("channel is closed")

	// ErrPublishTimeout is returned when publishing times out
	ErrPublishTimeout = errors.New("publish operation timed out")
)

// WrapError wraps an error with additional context
func WrapError(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}
