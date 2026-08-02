package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"adriane/internal/transport"
)

// NATSBus publishes events over a NATS JetStream stream. It is the transport
// for the distributed data plane: workers publish here, the control plane
// subscribes and forwards into its local in-memory bus (which persists to
// Postgres and fans out to SSE). Postgres remains the source of truth for
// replay; the JetStream stream is ephemeral transport.
type NATSBus struct {
	nc     *nats.Conn
	js     nats.JetStreamContext
	stream string
	sub    *nats.Subscription
}

const natsStreamName = "EVENTS"

func NewNATSBus(url string) (*NATSBus, error) {
	nc, err := nats.Connect(url, nats.Timeout(5*time.Second))
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}
	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream unavailable: %w", err)
	}
	// Ensure the events stream exists (memory storage; transport only).
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     natsStreamName,
		Subjects: []string{transport.SubjectEvents},
		Storage:  nats.MemoryStorage,
		MaxAge:   time.Hour,
	})
	if err != nil && err != nats.ErrStreamNameAlreadyInUse {
		nc.Close()
		return nil, fmt.Errorf("ensure events stream: %w", err)
	}
	return &NATSBus{nc: nc, js: js, stream: natsStreamName}, nil
}

func (b *NATSBus) Publish(ctx context.Context, e Event) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = b.js.Publish(transport.SubjectEvents, raw)
	return err
}

// Forward subscribes to the JetStream stream and invokes handler for every
// event until ctx is cancelled. Run it in its own goroutine.
func (b *NATSBus) Forward(ctx context.Context, handler func(Event) error) error {
	sub, err := b.js.SubscribeSync(transport.SubjectEvents)
	if err != nil {
		return fmt.Errorf("subscribe to events: %w", err)
	}
	b.sub = sub
	for {
		msg, err := sub.NextMsgWithContext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		var e Event
		if err := json.Unmarshal(msg.Data, &e); err != nil {
			_ = msg.Ack()
			continue
		}
		if err := handler(e); err != nil {
			// Forwarding failure must not block the stream; ack and continue.
			_ = msg.Ack()
			continue
		}
		_ = msg.Ack()
	}
}

func (b *NATSBus) Close() error {
	if b.sub != nil {
		_ = b.sub.Unsubscribe()
	}
	b.nc.Close()
	return nil
}
