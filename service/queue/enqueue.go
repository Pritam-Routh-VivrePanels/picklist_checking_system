package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
)

// enqueueScript atomically appends a job and schedules processing when no worker
// currently owns this sales order (active lock absent).
var enqueueScript = redis.NewScript(`
redis.call('RPUSH', KEYS[1], ARGV[2])
if redis.call('SET', KEYS[2], '1', 'NX', 'EX', 7200) then
  redis.call('LPUSH', KEYS[3], ARGV[1])
end
return redis.call('LLEN', KEYS[1])
`)

// finishScript releases the active lock or re-queues the sales order if more jobs
// arrived while the worker was finishing the previous one.
var finishScript = redis.NewScript(`
local len = redis.call('LLEN', KEYS[1])
if len > 0 then
  redis.call('LPUSH', KEYS[3], ARGV[1])
  return len
end
redis.call('DEL', KEYS[2])
return 0
`)

func extractSalesOrderID(payload map[string]interface{}) (string, error) {
	salesorder, ok := payload["salesorder"].(map[string]interface{})
	if !ok {
		return "", errors.New("top-level 'salesorder' object not found")
	}
	id, _ := salesorder["salesorder_id"].(string)
	if id == "" {
		return "", errors.New("salesorder_id not found or empty")
	}
	return id, nil
}

// Enqueue stores the webhook payload for the sales order. Jobs for the same
// sales order run strictly one after another; different sales orders run in
// parallel across workers.
func Enqueue(payload map[string]interface{}) error {
	if client == nil {
		return fmt.Errorf("redis queue not initialized")
	}

	salesOrderID, err := extractSalesOrderID(payload)
	if err != nil {
		return err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	depth, err := enqueueScript.Run(ctx, client,
		[]string{jobsKey(salesOrderID), activeKey(salesOrderID), readyKey()},
		salesOrderID, string(body),
	).Int64()
	if err != nil {
		return fmt.Errorf("redis enqueue: %w", err)
	}

	log.Printf("Queued sales order %s (queue depth %d)", salesOrderID, depth)
	return nil
}
