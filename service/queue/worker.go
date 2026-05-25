package queue

import (
	"encoding/json"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"picklist_checking_system/service/webhook"
)

// StartWorkers runs n goroutines that pull sales order IDs from the ready list.
// Each worker drains every queued payload for that order before taking another ID,
// so the same sales order never processes two webhooks at once.
func StartWorkers(n int) {
	for i := 0; i < n; i++ {
		go workerLoop(i + 1)
	}
	log.Printf("Started %d sales-order workers", n)
}

func workerLoop(workerID int) {
	for {
		result, err := client.BRPop(ctx, 0, readyKey()).Result()
		if err != nil {
			log.Printf("Worker %d: BRPop error: %v; retrying in 2s", workerID, err)
			time.Sleep(2 * time.Second)
			continue
		}
		if len(result) < 2 {
			continue
		}
		salesOrderID := result[1]
		processSalesOrder(workerID, salesOrderID)
	}
}

func processSalesOrder(workerID int, salesOrderID string) {
	jKey := jobsKey(salesOrderID)
	log.Printf("Worker %d: processing sales order %s", workerID, salesOrderID)

	for {
		raw, err := client.LPop(ctx, jKey).Result()
		if err == redis.Nil {
			break
		}
		if err != nil {
			log.Printf("Worker %d: LPop %s failed: %v", workerID, salesOrderID, err)
			break
		}

		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			log.Printf("Worker %d: invalid payload for %s: %v", workerID, salesOrderID, err)
			continue
		}

		webhook.HandleWebhook(payload)
	}

	remaining, err := finishScript.Run(ctx, client,
		[]string{jKey, activeKey(salesOrderID), readyKey()},
		salesOrderID,
	).Int64()
	if err != nil {
		log.Printf("Worker %d: finish script for %s failed: %v", workerID, salesOrderID, err)
		return
	}
	if remaining > 0 {
		log.Printf("Worker %d: re-queued sales order %s (%d job(s) pending)", workerID, salesOrderID, remaining)
	}
}
