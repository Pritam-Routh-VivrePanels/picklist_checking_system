package queue

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	client *redis.Client
	ctx    = context.Background()
)

func Init() {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	db := 0
	if s := os.Getenv("REDIS_DB"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			db = n
		}
	}

	client = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       db,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		log.Fatalf("Redis connection failed (%s): %v", addr, err)
	}
	log.Printf("Connected to Redis at %s (db=%d)", addr, db)
}

func WorkerCount() int {
	n := 50
	if s := os.Getenv("WORKER_COUNT"); s != "" {
		if parsed, err := strconv.Atoi(s); err == nil && parsed > 0 {
			n = parsed
		}
	}
	return n
}
