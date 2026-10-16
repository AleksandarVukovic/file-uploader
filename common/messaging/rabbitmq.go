package messaging

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wagslane/go-rabbitmq"
)

const contentTypeJSON = "application/json"

type rabbitMQPublisher struct {
	publisher      *rabbitmq.Publisher
	exchange       string
	confirmTimeout time.Duration
}

func NewRabbitMQPublisher(publisher *rabbitmq.Publisher, exchange string, confirmTimeout time.Duration) Publisher {
	return &rabbitMQPublisher{publisher: publisher, exchange: exchange, confirmTimeout: confirmTimeout}
}

func (p *rabbitMQPublisher) Publish(ctx context.Context, msg Message) error {
	confirmations, err := p.publisher.PublishWithDeferredConfirmWithContext(ctx, msg.Body, []string{msg.RoutingKey},
		rabbitmq.WithPublishOptionsExchange(p.exchange),
		rabbitmq.WithPublishOptionsContentType(contentTypeJSON),
		rabbitmq.WithPublishOptionsPersistentDelivery,
		rabbitmq.WithPublishOptionsMessageID(msg.ID),
		rabbitmq.WithPublishOptionsType(msg.Type),
	)
	if err != nil {
		return fmt.Errorf("rabbitmq publish: %w", err)
	}
	if len(confirmations) != 1 {
		return errors.New("rabbitmq publish: publisher is not in confirm mode")
	}

	confirmCtx, cancel := context.WithTimeout(ctx, p.confirmTimeout)
	defer cancel()
	acked, err := confirmations[0].WaitContext(confirmCtx)
	if err != nil {
		return fmt.Errorf("rabbitmq publish: waiting for confirmation: %w", err)
	}
	if !acked {
		return errors.New("rabbitmq publish: message was nacked by the broker")
	}
	return nil
}
