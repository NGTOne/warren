package amqp_0_9_1

import (
	"errors"
	"github.com/streadway/amqp"

	"github.com/NGTOne/warren/conn"
)

// streadway/amqp doesn't provide interfaces around a couple of its core types,
// making it impossible to mock them using library builtins
// So we'll create our own instead
type AMQPConn interface {
	Channel() (AMQPChan, error)
	Close()
}

type AMQPChan interface {
	Close()
	Qos(prefetchCount, prefetchSize int, global bool) error
	Ack(deliveryTag uint64, multiple bool) error
	Nack(deliveryTag uint64, multiple bool, requeue bool) error
	Publish(
		exchange, key string,
		mandatory, immediate bool,
		msg amqp.Publishing,
	) error

	Consume(
		queue, consumer string,
		autoAck, exclusive, noLocal, noWait bool,
		args amqp.Table,
	) (<-chan amqp.Delivery, error)

	QueueDeclare(
		name string,
		durable, autoDelete, exclusive, noWait bool,
		args amqp.Table,
	) (amqp.Queue, error)
	ExchangeDeclare(
		name, kind string,
		durable, autoDelete, internal, noWait bool,
		args amqp.Table,
	) error
	QueueBind(
		name, key, exchange string,
		noWait bool,
		args amqp.Table,
	) error
}

// Provides a couple of thin convenience wrappers around streadway's AMQP
// library, so we can plug it into warren in a consistent way
type connection struct {
	qConn AMQPConn
	qChan AMQPChan
	queue    string
}

func NewConn(qConn AMQPConn) (*connection, error) {
	qChan, err := qConn.Channel()
	if err != nil {
		return nil, err
	}

	err = qChan.Qos(1, 0, false)
	if err != nil {
		return nil, err
	}

	return &connection{
		qConn: qConn,
		qChan: qChan,
	}, nil
}

func (c *connection) AckMsg(m conn.Message) error {
	tag, err := m.GetHeaderValue("DeliveryTag")

	if err != nil {
		return err
	}

	return c.qChan.Ack(tag.(uint64), false)
}

func (c *connection) NackMsg(m conn.Message) error {
	tag, err := m.GetHeaderValue("DeliveryTag")

	if err != nil {
		return err
	}

	return c.qChan.Nack(tag.(uint64), false, true)
}

func (c *connection) Listen(f func(conn.Message)) error {
	if c.queue == "" {
		return errors.New(
			"Need to create a queue before attempting to listen",
		)
	}

	msgs, err := c.qChan.Consume(
		c.queue,
		"",
		false,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		return err
	}

	forever := make(chan bool)
	defer c.qChan.Close()

	go func() {
		for d := range msgs {
			f(message{inner: d})
		}
	}()

	<-forever

	return nil
}

func (c *connection) SendResponse(
	original conn.Message,
	response conn.Message,
) error {
	// For the moment, we're gonna assume that the original is untouched
	// and that we can just type-assert it directly
	// Not _100%_ satisfied with it, but not much I can do about it
	// TODO: Figure out how to avoid the original message being mutated
	//       that doesn't involve storing it in the connection (and making
	//       it stateful in the process)
	amqpOriginal := original.(message)

	return c.qChan.Publish(
		"",
		amqpOriginal.inner.ReplyTo,
		false,
		false,
		amqp.Publishing{
			ContentType: "text/plain",
			CorrelationId: amqpOriginal.inner.CorrelationId,
			Headers: response.GetAllHeaders(),
			Body: response.GetBody(),
		},
	)
}
