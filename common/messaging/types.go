package messaging

import "context"

type Message struct {
	ID         string
	Type       string
	RoutingKey string
	Body       []byte
}

type Publisher interface {
	Publish(ctx context.Context, msg Message) error
}
