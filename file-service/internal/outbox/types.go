package outbox

import "context"

type Event struct {
	ID            string
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       []byte
}

type Repository interface {
	Insert(ctx context.Context, event Event) error
	GetUnpublished(ctx context.Context, aggregateType string, limit int) ([]Event, error)
	MarkPublished(ctx context.Context, id string) error
}
