package adapter

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkit/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTransport struct {
	mu            sync.Mutex
	capabilities  Capabilities
	published     []Message
	handlers      []TransportHandler
	closed        bool
	subscriptions int
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{capabilities: Capabilities{Wildcard: true}}
}

func (f *fakeTransport) Publish(ctx context.Context, msg Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, msg.Clone())
	return nil
}

func (f *fakeTransport) Subscribe(ctx context.Context, pattern string, handler TransportHandler) (Subscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers = append(f.handlers, handler)
	f.subscriptions++
	return fakeSubscription{closeFn: func() {
		f.mu.Lock()
		f.subscriptions--
		f.mu.Unlock()
	}}, nil
}

func (f *fakeTransport) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

func (f *fakeTransport) Capabilities() Capabilities { return f.capabilities }

func (f *fakeTransport) emit(ctx context.Context, msg Message) error {
	f.mu.Lock()
	handlers := append([]TransportHandler(nil), f.handlers...)
	f.mu.Unlock()
	for _, handler := range handlers {
		if err := handler(ctx, msg.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeTransport) publishedLen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.published)
}

func (f *fakeTransport) lastPublished() Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.published[len(f.published)-1].Clone()
}

type fakeSubscription struct{ closeFn func() }

func (s fakeSubscription) Close() error {
	if s.closeFn != nil {
		s.closeFn()
	}
	return nil
}

func TestJSONCodecRoundTrip(t *testing.T) {
	codec := JSONCodec{}
	data, headers, err := codec.Encode(context.Background(), "json.topic", map[string]any{"n": 1})
	require.NoError(t, err)
	assert.Equal(t, "application/json", headers[HeaderContentType])

	payload, err := codec.Decode(context.Background(), Message{Payload: data})
	require.NoError(t, err)
	decoded := payload.(map[string]any)
	assert.Equal(t, "1", decoded["n"].(interface{ String() string }).String())
}

func TestBridgePublishesLocalEventsToTransport(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()
	transport := newFakeTransport()

	bridge, err := NewBridge(bus, transport, WithNodeID("node-a"), WithRemotePatterns("cluster.#"))
	require.NoError(t, err)
	require.NoError(t, bridge.Start(context.Background()))
	defer bridge.Close()

	require.NoError(t, bus.PublishSync("cluster.created", map[string]string{"id": "1"}))
	require.Eventually(t, func() bool { return transport.publishedLen() == 1 }, time.Second, time.Millisecond)

	msg := transport.lastPublished()
	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, "cluster.created", msg.Topic)
	assert.Equal(t, "node-a", msg.Origin)
	assert.Equal(t, "application/json", msg.Headers[HeaderContentType])
	assert.Equal(t, uint64(1), bridge.Stats().OutboundPublished)
}

func TestBridgePublishesRemoteEventsToLocalAndPreventsEcho(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()
	transport := newFakeTransport()

	bridge, err := NewBridge(bus, transport, WithNodeID("node-a"), WithRemotePatterns("remote.#"))
	require.NoError(t, err)
	require.NoError(t, bridge.Start(context.Background()))
	defer bridge.Close()

	received := make(chan Message, 1)
	require.NoError(t, bus.Subscribe("remote.created", func(ctx context.Context, topic string, payload any) {
		msg, ok := MessageFromContext(ctx)
		require.True(t, ok)
		received <- msg
	}))

	data, _, err := JSONCodec{}.Encode(context.Background(), "remote.created", map[string]string{"id": "2"})
	require.NoError(t, err)
	require.NoError(t, transport.emit(context.Background(), Message{
		ID:      "m-1",
		Topic:   "remote.created",
		Origin:  "node-b",
		Payload: data,
	}))

	select {
	case msg := <-received:
		assert.Equal(t, "m-1", msg.ID)
		assert.Equal(t, "node-b", msg.Origin)
	case <-time.After(time.Second):
		t.Fatal("remote event was not published locally")
	}

	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, 0, transport.publishedLen())
	assert.Equal(t, uint64(1), bridge.Stats().InboundPublished)
}

func TestBridgeSkipsOwnOriginAndDuplicateMessages(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()
	transport := newFakeTransport()

	bridge, err := NewBridge(bus, transport, WithNodeID("node-a"), WithRemotePatterns("dedup.#"))
	require.NoError(t, err)
	require.NoError(t, bridge.Start(context.Background()))
	defer bridge.Close()

	var count atomic.Int32
	require.NoError(t, bus.Subscribe("dedup.created", func(topic string, payload any) {
		count.Add(1)
	}))
	data, _, err := JSONCodec{}.Encode(context.Background(), "dedup.created", "payload")
	require.NoError(t, err)

	require.NoError(t, transport.emit(context.Background(), Message{
		ID:      "own",
		Topic:   "dedup.created",
		Origin:  "node-a",
		Payload: data,
	}))
	require.NoError(t, transport.emit(context.Background(), Message{
		ID:      "dup",
		Topic:   "dedup.created",
		Origin:  "node-b",
		Payload: data,
	}))
	require.NoError(t, transport.emit(context.Background(), Message{
		ID:      "dup",
		Topic:   "dedup.created",
		Origin:  "node-b",
		Payload: data,
	}))

	require.Eventually(t, func() bool { return count.Load() == 1 }, time.Second, time.Millisecond)
	assert.Equal(t, uint64(1), bridge.Stats().DuplicateDropped)
	assert.Equal(t, uint64(1), bridge.Stats().InboundDropped)
}

func TestBridgeValidatesTransportWildcardCapability(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()
	transport := newFakeTransport()
	transport.capabilities = Capabilities{Wildcard: false}

	bridge, err := NewBridge(bus, transport, WithNodeID("node-a"))
	require.NoError(t, err)
	assert.ErrorIs(t, bridge.Start(context.Background()), ErrWildcardUnsupported)

	bridge, err = NewBridge(bus, transport, WithNodeID("node-a"), WithRemotePatterns("exact.topic"))
	require.NoError(t, err)
	require.NoError(t, bridge.Start(context.Background()))
	defer bridge.Close()
}

func TestBridgeCloseReleasesSubscriptionsAndOwnedTransport(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()
	transport := newFakeTransport()

	bridge, err := NewBridge(bus, transport, WithNodeID("node-a"), WithOwnTransport(true))
	require.NoError(t, err)
	require.NoError(t, bridge.Start(context.Background()))
	require.NoError(t, bridge.Close())

	transport.mu.Lock()
	defer transport.mu.Unlock()
	assert.True(t, transport.closed)
	assert.Equal(t, 0, transport.subscriptions)
}
