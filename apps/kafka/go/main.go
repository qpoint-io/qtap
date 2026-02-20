package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/segmentio/kafka-go"
)

func main() {
	broker := getEnv("KAFKA_BROKER", "localhost:9092")
	topic := getEnv("KAFKA_TOPIC", "qtap-test")
	groupID := getEnv("KAFKA_GROUP_ID", "go-consumer-group")
	maxIterations := getEnvInt("MAX_ITERATIONS", 0)
	sleepDuration := getEnvFloat("SLEEP_DURATION", 1.0)

	createTopic(broker, topic)

	writer := &kafka.Writer{
		Addr:     kafka.TCP(broker),
		Topic:    topic,
		Balancer: &kafka.LeastBytes{},
	}
	defer writer.Close()

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{broker},
		Topic:   topic,
		GroupID: groupID,
	})
	defer reader.Close()

	iteration := 0
	for {
		iteration++
		fmt.Printf("[Go] Iteration %d\n", iteration)

		msgs := []kafka.Message{
			{
				Key:   []byte(fmt.Sprintf("key-%d-event", iteration)),
				Value: []byte(fmt.Sprintf(`{"iteration": %d, "type": "event", "lang": "go"}`, iteration)),
			},
			{
				Key:   []byte(fmt.Sprintf("key-%d-metric", iteration)),
				Value: []byte(fmt.Sprintf(`{"iteration": %d, "type": "metric", "value": %d}`, iteration, iteration*10)),
			},
		}
		if err := writer.WriteMessages(context.Background(), msgs...); err != nil {
			fmt.Printf("[Go] Failed to produce messages: %v\n", err)
		} else {
			fmt.Printf("[Go] Produced 2 messages to topic %s\n", topic)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		for {
			msg, err := reader.ReadMessage(ctx)
			if err != nil {
				break
			}
			fmt.Printf("[Go] Consumed message: key=%s, value=%s, offset=%d\n",
				string(msg.Key), string(msg.Value), msg.Offset)
		}
		cancel()

		if maxIterations > 0 && iteration >= maxIterations {
			break
		}
		time.Sleep(time.Duration(sleepDuration * float64(time.Second)))
	}

	fmt.Printf("[Go] Completed %d iterations\n", iteration)
}

func createTopic(broker, topic string) {
	conn, err := kafka.Dial("tcp", broker)
	if err != nil {
		fmt.Printf("[Go] Failed to connect to broker: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		fmt.Printf("[Go] Failed to get controller: %v\n", err)
		os.Exit(1)
	}

	controllerConn, err := kafka.Dial("tcp", fmt.Sprintf("%s:%d", controller.Host, controller.Port))
	if err != nil {
		fmt.Printf("[Go] Failed to connect to controller: %v\n", err)
		os.Exit(1)
	}
	defer controllerConn.Close()

	err = controllerConn.CreateTopics(kafka.TopicConfig{
		Topic:             topic,
		NumPartitions:     1,
		ReplicationFactor: 1,
	})
	if err != nil {
		fmt.Printf("[Go] Topic creation note: %v\n", err)
	} else {
		fmt.Printf("[Go] Topic %s created\n", topic)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	return defaultValue
}
