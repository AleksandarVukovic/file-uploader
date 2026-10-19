package broker

import (
	"context"
	"errors"
	"fmt"

	"github.com/aleksandarv/file-uploader/common/events"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/common/messaging"
	"github.com/aleksandarv/file-uploader/common/telemetry"
	"github.com/aleksandarv/file-uploader/file-service/internal/outbox"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

var tracer = otel.Tracer("file-service/broker")

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

		if err := s.publishEvent(ctx, event); err != nil {
			errs = append(errs, err)
			continue
		}
		published++
	}

	if published > 0 {
		log.Info("file events published", "count", published)
	}
	return errors.Join(errs...)
}

func (s *service) publishEvent(ctx context.Context, event outbox.Event) (err error) {
	ctx, span := tracer.Start(ctx, "publish file event")
	defer span.End()
	telemetry.SetFileID(ctx, event.AggregateID)
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
	}()

	routingKey, ok := routingKeys[event.EventType]
	if !ok {
		return fmt.Errorf("file event %s (fileId %s): unknown event type %q", event.ID, event.AggregateID, event.EventType)
	}

	err = s.publisher.Publish(ctx, messaging.Message{
		ID:         event.ID,
		Type:       event.EventType,
		RoutingKey: routingKey,
		Body:       event.Payload,
	})
	if err != nil {
		return fmt.Errorf("file event %s (fileId %s): publish: %w", event.ID, event.AggregateID, err)
	}

	if err := s.outboxRepo.MarkPublished(ctx, event.ID); err != nil {
		return fmt.Errorf("file event %s (fileId %s): published but not marked, it will be published again: %w", event.ID, event.AggregateID, err)
	}
	return nil
}
