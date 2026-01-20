package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

func main() {
	host := getEnv("REDIS_HOST", "localhost")
	port := getEnv("REDIS_PORT", "6379")
	maxIterations := getEnvInt("MAX_ITERATIONS", 0)
	sleepDuration := getEnvFloat("SLEEP_DURATION", 1.0)

	client := redis.NewClient(&redis.Options{
		Addr: fmt.Sprintf("%s:%s", host, port),
	})
	ctx := context.Background()

	iteration := 0
	for {
		iteration++
		fmt.Printf("[Go] Iteration %d\n", iteration)

		// Basic operations
		key := fmt.Sprintf("go:key:%d", iteration)
		client.Set(ctx, key, fmt.Sprintf("value-%d", iteration), 0)
		client.Get(ctx, key)

		// List operations
		client.LPush(ctx, "go:list", fmt.Sprintf("item-%d", iteration))
		client.LRange(ctx, "go:list", 0, -1)

		// Hash operations
		client.HSet(ctx, "go:hash", fmt.Sprintf("field-%d", iteration), fmt.Sprintf("value-%d", iteration))
		client.HGetAll(ctx, "go:hash")

		// Pub/sub (publish only)
		client.Publish(ctx, "go:channel", fmt.Sprintf("message-%d", iteration))

		if maxIterations > 0 && iteration >= maxIterations {
			break
		}
		time.Sleep(time.Duration(sleepDuration * float64(time.Second)))
	}

	fmt.Printf("[Go] Completed %d iterations\n", iteration)
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
