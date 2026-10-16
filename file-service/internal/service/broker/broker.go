package broker

import (
	"context"
	"errors"
	"fmt"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/common/messaging"
	"github.com/aleksandarv/file-uploader/file-service/internal/outbox"
)

var routingKeys = map[string]string{
	events.EventTypeFileUploaded: "file.uploaded",
}

type Service interface {
	PublishFileEvents(ctx context.Context) error
}

type service struct {
	outboxRepo outbox.Repository
	publisher  messaging.Publisher
	batchSize  int
}

func New(outboxRepo outbox.Repository, publisher messaging.Publisher, batchSize int) Service {
	return &service{outboxRepo: outboxRepo, publisher: publisher, batchSize: batchSize}
}

func (s *service) PublishFileEvents(ctx context.Context) error {
	log := logger.FromCtx(ctx)

	pending, err := s.outboxRepo.GetUnpublished(ctx, events.AggregateTypeFile, s.batchSize)
	if err != nil {
		return fmt.Errorf("get unpublished file events: %w", err)
	}

	var errs []error
	published := 0
	for _, event := range pending {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}

		routingKey, ok := routingKeys[event.EventType]
		if !ok {
			errs = append(errs, fmt.Errorf("file event %s: unknown event type %q", event.ID, event.EventType))
			continue
		}

		err := s.publisher.Publish(ctx, messaging.Message{
			ID:         event.ID,
			Type:       event.EventType,
			RoutingKey: routingKey,
			Body:       event.Payload,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("file event %s: publish: %w", event.ID, err))
			continue
		}

		if err := s.outboxRepo.MarkPublished(ctx, event.ID); err != nil {
			errs = append(errs, fmt.Errorf("file event %s: published but not marked, it will be published again: %w", event.ID, err))
			continue
		}
		published++
	}

	if published > 0 {
		log.Info("file events published", "count", published)
	}
	return errors.Join(errs...)
}
