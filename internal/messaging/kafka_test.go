package messaging_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/dnakitare/aether/internal/messaging"
	"github.com/dnakitare/aether/pkg/api"
)

func TestKafkaConfig(t *testing.T) {
	config := messaging.DefaultKafkaConfig()

	if len(config.Brokers) == 0 {
		t.Error("Expected default brokers to be set")
	}

	if config.TopicPrefix == "" {
		t.Error("Expected topic prefix to be set")
	}

	if config.NumPartitions == 0 {
		t.Error("Expected num partitions to be set")
	}
}

func TestKafkaClient(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kafka integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := messaging.DefaultKafkaConfig()
	client, err := messaging.NewKafkaClient(logger, config)
	if err != nil {
		t.Skipf("Kafka not available: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// Test topic creation
	err = client.CreateTopic(ctx, "test-topic")
	if err != nil {
		t.Logf("Topic creation result: %v", err)
	}
}

func TestPubSub(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kafka integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := messaging.DefaultKafkaConfig()
	client, err := messaging.NewKafkaClient(logger, config)
	if err != nil {
		t.Skipf("Kafka not available: %v", err)
	}
	defer client.Close()

	pubsub := messaging.NewPubSub(logger, client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test publish
	testMessage := map[string]string{
		"type": "test",
		"data": "hello world",
	}

	err = pubsub.Publish(ctx, "test-events", testMessage)
	if err != nil {
		t.Fatalf("Failed to publish message: %v", err)
	}
}

func TestDirectMessaging(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kafka integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := messaging.DefaultKafkaConfig()
	client, err := messaging.NewKafkaClient(logger, config)
	if err != nil {
		t.Skipf("Kafka not available: %v", err)
	}
	defer client.Close()

	dm := messaging.NewDirectMessaging(logger, client)

	ctx := context.Background()

	// Test sending message
	fromAgent := api.AgentID("agent-1")
	toAgent := api.AgentID("agent-2")
	message := map[string]string{
		"command": "execute",
		"payload": "test",
	}

	err = dm.SendMessage(ctx, fromAgent, toAgent, message)
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}
}

func TestMessageEnvelope(t *testing.T) {
	envelope := messaging.MessageEnvelope{
		From:      api.AgentID("sender"),
		To:        api.AgentID("receiver"),
		Payload:   "test payload",
		Timestamp: time.Now(),
	}

	if envelope.From == "" {
		t.Error("Expected From to be set")
	}

	if envelope.To == "" {
		t.Error("Expected To to be set")
	}

	if envelope.Payload == nil {
		t.Error("Expected Payload to be set")
	}
}
