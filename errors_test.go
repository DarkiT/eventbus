package eventbus

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrors(t *testing.T) {
	// Test predefined errors
	assert.Equal(t, "handler is not a function", ErrHandlerIsNotFunc.Error())
	assert.Equal(t, "handler must have exactly two parameters", ErrHandlerParamNum.Error())
	assert.Equal(t, "handler's first parameter must be string", ErrHandlerFirstParam.Error())
	assert.Equal(t, "no subscriber found for topic", ErrNoSubscriber.Error())
	assert.Equal(t, "channel is closed", ErrChannelClosed.Error())
	assert.Equal(t, "publish operation timed out", ErrPublishTimeout.Error())
}

func TestWrapError(t *testing.T) {
	// Test with nil error
	assert.Nil(t, WrapError(nil, "test message"))

	// Test with basic error
	baseErr := errors.New("base error")
	wrappedErr := WrapError(baseErr, "additional context")
	assert.ErrorIs(t, wrappedErr, baseErr)
	assert.Contains(t, wrappedErr.Error(), "additional context")
	assert.Contains(t, wrappedErr.Error(), "base error")

	// Test with format arguments
	wrappedErr = WrapError(baseErr, "error at position %d: %s", 1, "invalid input")
	assert.ErrorIs(t, wrappedErr, baseErr)
	assert.Contains(t, wrappedErr.Error(), "error at position 1")
	assert.Contains(t, wrappedErr.Error(), "invalid input")
	assert.Contains(t, wrappedErr.Error(), "base error")

	// Test error wrapping chain
	err1 := errors.New("error 1")
	err2 := WrapError(err1, "context 2")
	err3 := WrapError(err2, "context 3")

	assert.ErrorIs(t, err3, err1)
	assert.Contains(t, err3.Error(), "context 3")
	assert.Contains(t, err3.Error(), "context 2")
	assert.Contains(t, err3.Error(), "error 1")
}

func TestErrorComparison(t *testing.T) {
	// Test error comparison using Is
	err := WrapError(ErrHandlerIsNotFunc, "failed to add handler")
	assert.True(t, errors.Is(err, ErrHandlerIsNotFunc))

	// Test error not matching
	assert.False(t, errors.Is(err, ErrHandlerParamNum))

	// Test multiple wrapping levels
	wrapped := WrapError(err, "outer context")
	assert.True(t, errors.Is(wrapped, ErrHandlerIsNotFunc))
}
