// Package messaging provides event-driven messaging for agent communication.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/dnakitare/aether/pkg/api"
)

// KafkaConfig holds Kafka configuration.
type KafkaConfig struct {
	// Brokers is the list of Kafka broker addresses
	Brokers []string

	// TopicPrefix for namespacing topics
	TopicPrefix string

	// NumPartitions for new topics
	NumPartitions int

	// ReplicationFactor for new topics
	ReplicationFactor int

	// Compression type (gzip, snappy, lz4, zstd)
	Compression string

	// BatchSize for producer batching
	BatchSize int

	// BatchTimeout for producer batching
	BatchTimeout time.Duration
}

// DefaultKafkaConfig returns default Kafka configuration.
func DefaultKafkaConfig() KafkaConfig {
	return KafkaConfig{
		Brokers:           []string{"localhost:9092"},
		TopicPrefix:       "aether.",
		NumPartitions:     3,
		ReplicationFactor: 1,
		Compression:       "snappy",
		BatchSize:         100,
		BatchTimeout:      10 * time.Millisecond,
	}
}

// KafkaClient provides Kafka-based messaging.
type KafkaClient struct {
	logger *slog.Logger
	config KafkaConfig
	conn   *kafka.Conn
}

// NewKafkaClient creates a new Kafka client.
func NewKafkaClient(logger *slog.Logger, config KafkaConfig) (*KafkaClient, error) {
	// Connect to any broker to create topics
	conn, err := kafka.Dial("tcp", config.Brokers[0])
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Kafka: %w", err)
	}

	return &KafkaClient{
		logger: logger.With("component", "kafka_client"),
		config: config,
		conn:   conn,
	}, nil
}

// Close closes the Kafka connection.
func (kc *KafkaClient) Close() error {
	if kc.conn != nil {
		return kc.conn.Close()
	}
	return nil
}

// CreateTopic creates a Kafka topic if it doesn't exist.
func (kc *KafkaClient) CreateTopic(ctx context.Context, topic string) error {
	fullTopic := kc.config.TopicPrefix + topic

	controller, err := kc.conn.Controller()
	if err != nil {
		return fmt.Errorf("failed to get controller: %w", err)
	}

	controllerConn, err := kafka.Dial("tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		return fmt.Errorf("failed to connect to controller: %w", err)
	}
	defer controllerConn.Close()

	topicConfigs := []kafka.TopicConfig{
		{
			Topic:             fullTopic,
			NumPartitions:     kc.config.NumPartitions,
			ReplicationFactor: kc.config.ReplicationFactor,
		},
	}

	err = controllerConn.CreateTopics(topicConfigs...)
	if err != nil {
		// Topic might already exist, which is fine
		kc.logger.DebugContext(ctx, "topic creation result", "topic", fullTopic, "error", err)
	} else {
		kc.logger.InfoContext(ctx, "topic created", "topic", fullTopic)
	}

	return nil
}

// Producer creates a new Kafka producer.
func (kc *KafkaClient) Producer(topic string) *Producer {
	fullTopic := kc.config.TopicPrefix + topic

	var compression kafka.Compression
	switch kc.config.Compression {
	case "gzip":
		compression = kafka.Gzip
	case "snappy":
		compression = kafka.Snappy
	case "lz4":
		compression = kafka.Lz4
	case "zstd":
		compression = kafka.Zstd
	default:
		compression = kafka.Snappy
	}

	writer := &kafka.Writer{
		Addr:         kafka.TCP(kc.config.Brokers...),
		Topic:        fullTopic,
		Balancer:     &kafka.Hash{}, // Hash by key for ordering
		Compression:  compression,
		BatchSize:    kc.config.BatchSize,
		BatchTimeout: kc.config.BatchTimeout,
		RequiredAcks: kafka.RequireOne, // Wait for leader ack
	}

	return &Producer{
		logger: kc.logger.With("topic", fullTopic),
		writer: writer,
		topic:  fullTopic,
	}
}

// Consumer creates a new Kafka consumer.
func (kc *KafkaClient) Consumer(topic, groupID string) *Consumer {
	fullTopic := kc.config.TopicPrefix + topic

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        kc.config.Brokers,
		Topic:          fullTopic,
		GroupID:        groupID,
		MinBytes:       1,
		MaxBytes:       10e6, // 10MB
		CommitInterval: 1 * time.Second,
		StartOffset:    kafka.LastOffset,
	})

	return &Consumer{
		logger: kc.logger.With("topic", fullTopic, "group_id", groupID),
		reader: reader,
		topic:  fullTopic,
	}
}

// Producer wraps Kafka writer for publishing messages.
type Producer struct {
	logger *slog.Logger
	writer *kafka.Writer
	topic  string
}

// Publish publishes a message to the topic.
func (p *Producer) Publish(ctx context.Context, key string, value interface{}) error {
	// Serialize value to JSON
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	msg := kafka.Message{
		Key:   []byte(key),
		Value: data,
		Time:  time.Now(),
	}

	err = p.writer.WriteMessages(ctx, msg)
	if err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	p.logger.DebugContext(ctx, "message published", "key", key, "size", len(data))
	return nil
}

// PublishBatch publishes multiple messages in a batch.
func (p *Producer) PublishBatch(ctx context.Context, messages []Message) error {
	kafkaMessages := make([]kafka.Message, len(messages))

	for i, msg := range messages {
		data, err := json.Marshal(msg.Value)
		if err != nil {
			return fmt.Errorf("failed to marshal message %d: %w", i, err)
		}

		kafkaMessages[i] = kafka.Message{
			Key:   []byte(msg.Key),
			Value: data,
			Time:  time.Now(),
		}
	}

	err := p.writer.WriteMessages(ctx, kafkaMessages...)
	if err != nil {
		return fmt.Errorf("failed to write batch: %w", err)
	}

	p.logger.DebugContext(ctx, "batch published", "count", len(messages))
	return nil
}

// Close closes the producer.
func (p *Producer) Close() error {
	return p.writer.Close()
}

// Consumer wraps Kafka reader for consuming messages.
type Consumer struct {
	logger *slog.Logger
	reader *kafka.Reader
	topic  string
}

// Consume consumes messages from the topic.
func (c *Consumer) Consume(ctx context.Context, handler MessageHandler) error {
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("consumer stopped")
			return ctx.Err()
		default:
			msg, err := c.reader.FetchMessage(ctx)
			if err != nil {
				c.logger.ErrorContext(ctx, "failed to fetch message", "error", err)
				continue
			}

			// Parse message
			var value interface{}
			if err := json.Unmarshal(msg.Value, &value); err != nil {
				c.logger.ErrorContext(ctx, "failed to unmarshal message", "error", err)
				// Still commit to avoid reprocessing bad messages
				c.reader.CommitMessages(ctx, msg)
				continue
			}

			// Handle message
			message := Message{
				Key:       string(msg.Key),
				Value:     value,
				Timestamp: msg.Time,
				Offset:    msg.Offset,
				Partition: msg.Partition,
			}

			err = handler(ctx, message)
			if err != nil {
				c.logger.ErrorContext(ctx, "handler error", "error", err, "key", message.Key)
				// Optionally: send to dead letter queue
				// For now, we'll commit to avoid reprocessing
			}

			// Commit message
			if err := c.reader.CommitMessages(ctx, msg); err != nil {
				c.logger.ErrorContext(ctx, "failed to commit message", "error", err)
			}
		}
	}
}

// Close closes the consumer.
func (c *Consumer) Close() error {
	return c.reader.Close()
}

// Message represents a message in the system.
type Message struct {
	Key       string
	Value     interface{}
	Timestamp time.Time
	Offset    int64
	Partition int
}

// MessageHandler is a function that processes messages.
type MessageHandler func(ctx context.Context, msg Message) error

// PubSub provides pub/sub messaging patterns.
type PubSub struct {
	logger *slog.Logger
	kafka  *KafkaClient
}

// NewPubSub creates a new pub/sub client.
func NewPubSub(logger *slog.Logger, kafka *KafkaClient) *PubSub {
	return &PubSub{
		logger: logger.With("component", "pubsub"),
		kafka:  kafka,
	}
}

// Publish publishes a message to a topic.
func (ps *PubSub) Publish(ctx context.Context, topic string, message interface{}) error {
	// Ensure topic exists
	if err := ps.kafka.CreateTopic(ctx, topic); err != nil {
		return err
	}

	producer := ps.kafka.Producer(topic)
	defer producer.Close()

	// Use topic name as key for now (could be message-specific)
	return producer.Publish(ctx, topic, message)
}

// Subscribe subscribes to a topic with a consumer group.
func (ps *PubSub) Subscribe(ctx context.Context, topic, groupID string, handler MessageHandler) error {
	// Ensure topic exists
	if err := ps.kafka.CreateTopic(ctx, topic); err != nil {
		return err
	}

	consumer := ps.kafka.Consumer(topic, groupID)
	defer consumer.Close()

	return consumer.Consume(ctx, handler)
}

// DirectMessaging provides direct agent-to-agent messaging.
type DirectMessaging struct {
	logger *slog.Logger
	kafka  *KafkaClient
}

// NewDirectMessaging creates a new direct messaging client.
func NewDirectMessaging(logger *slog.Logger, kafka *KafkaClient) *DirectMessaging {
	return &DirectMessaging{
		logger: logger.With("component", "direct_messaging"),
		kafka:  kafka,
	}
}

// SendMessage sends a direct message to an agent.
func (dm *DirectMessaging) SendMessage(ctx context.Context, fromAgent, toAgent api.AgentID, message interface{}) error {
	// Use agent ID as topic for direct messaging
	topic := fmt.Sprintf("agent.%s", toAgent)

	// Ensure topic exists
	if err := dm.kafka.CreateTopic(ctx, topic); err != nil {
		return err
	}

	producer := dm.kafka.Producer(topic)
	defer producer.Close()

	// Wrap message with metadata
	envelope := MessageEnvelope{
		From:      fromAgent,
		To:        toAgent,
		Payload:   message,
		Timestamp: time.Now(),
	}

	return producer.Publish(ctx, string(fromAgent), envelope)
}

// ReceiveMessages receives messages for an agent.
func (dm *DirectMessaging) ReceiveMessages(ctx context.Context, agentID api.AgentID, handler func(ctx context.Context, envelope MessageEnvelope) error) error {
	topic := fmt.Sprintf("agent.%s", agentID)

	// Ensure topic exists
	if err := dm.kafka.CreateTopic(ctx, topic); err != nil {
		return err
	}

	consumer := dm.kafka.Consumer(topic, string(agentID))
	defer consumer.Close()

	return consumer.Consume(ctx, func(ctx context.Context, msg Message) error {
		// Parse envelope
		data, err := json.Marshal(msg.Value)
		if err != nil {
			return fmt.Errorf("failed to marshal envelope: %w", err)
		}

		var envelope MessageEnvelope
		if err := json.Unmarshal(data, &envelope); err != nil {
			return fmt.Errorf("failed to unmarshal envelope: %w", err)
		}

		return handler(ctx, envelope)
	})
}

// MessageEnvelope wraps messages with metadata.
type MessageEnvelope struct {
	From      api.AgentID `json:"from"`
	To        api.AgentID `json:"to"`
	Payload   interface{} `json:"payload"`
	Timestamp time.Time   `json:"timestamp"`
}
