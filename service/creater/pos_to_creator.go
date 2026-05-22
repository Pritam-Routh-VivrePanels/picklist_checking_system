package creator_sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"picklist_checking_system/service"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

// Global rate limiter to prevent hitting Creator API limits
var (
	requestChan   = make(chan struct{}, 2)                 // Max 2 concurrent requests
	requestTicker = time.NewTicker(100 * time.Millisecond) // Throttle requests
)

func init() {
	// Release a token every 500ms to throttle requests
	go func() {
		for range requestTicker.C {
			select {
			case requestChan <- struct{}{}:
			default:
			}
		}
	}()
}

// parseRetryAfter parses Retry-After header which can be seconds (int) or duration format (e.g., "1m20s", "10s")
func parseRetryAfter(retryAfterStr string) time.Duration {
	retryAfterStr = strings.TrimSpace(retryAfterStr)

	// Try parsing as integer (seconds)
	if sec, err := strconv.Atoi(retryAfterStr); err == nil {
		return time.Duration(sec) * time.Second
	}

	// Try parsing as duration format (e.g., "1m20s", "10s")
	if duration, err := time.ParseDuration(retryAfterStr); err == nil {
		return duration
	}

	// Default fallback
	return 10 * time.Second
}

// Global queue to deduplicate bulk updates for same itemID
var (
	itemUpdateQueue = make(map[string]bool)
	queueMutex      = &sync.Mutex{}
)

// PostToCreator updates records in Zoho Creator by item_id criteria with pagination support
// Maximum 200 records per request; automatically paginates if more records match the criteria
// Uses deduplication queue to prevent multiple concurrent updates for the same itemID
func PostToCreator(updateData map[string]interface{}, itemID string) {
	// Deduplicate: skip if itemID is already being processed
	queueMutex.Lock()
	if itemUpdateQueue[itemID] {
		queueMutex.Unlock()
		log.Printf("BulkUpdate: Skipping duplicate update for itemID=%s (already in progress)\n", itemID)
		return
	}
	itemUpdateQueue[itemID] = true
	queueMutex.Unlock()

	defer func() {
		queueMutex.Lock()
		delete(itemUpdateQueue, itemID)
		queueMutex.Unlock()
	}()

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Dotenv load error", err)
	}

	baseURL := os.Getenv("ZOHO_CLIENT_URL")
	appName := os.Getenv("ZOHO_CREATOR_APP_NAME")
	reportName := os.Getenv("ZOHO_DEALER_REPORT")

	accessToken := service.GetToken() // ✅ Fetch once

	client := &http.Client{}

	// Build API URL for criteria-based bulk update (with query parameter)
	baseURLPath := fmt.Sprintf(
		"%s/creator/v2.1/data/vivrepanelsprivatelimited/%s/report/%s",
		baseURL,
		appName,
		reportName,
	)

	// Add process_until_limit as query parameter
	queryParams := url.Values{}
	queryParams.Add("process_until_limit", "true")
	fullURL := baseURLPath + "?" + queryParams.Encode()

	log.Printf("BulkUpdate: Starting bulk update for itemID=%s with URL=%s\n", itemID, fullURL)

	criteria := fmt.Sprintf("Item_ID==\"%s\"", itemID)
	pageCount := 0
	moreRecords := true

	for moreRecords {
		pageCount++

		var stockData map[string]interface{}

		if dataMap, ok := updateData["data"].(map[string]interface{}); ok {
			stockData = dataMap
		} else {
			log.Printf("BulkUpdate: ERROR - could not extract 'data' from updateData for itemID=%s\n", itemID)
			return
		}

		// Build request payload with criteria for bulk update (process_until_limit is now a query parameter)
		requestPayload := map[string]interface{}{
			"criteria": criteria,
			"data":     stockData,
		}

		jsonData, err := json.Marshal(requestPayload)
		if err != nil {
			log.Printf("BulkUpdate: JSON marshal error for itemID=%s page=%d: %v\n", itemID, pageCount, err)
			return
		}

		prettyPayload, err := json.MarshalIndent(requestPayload, "", "  ")
		if err != nil {
			log.Println("BulkUpdate: JSON indent error:", err)
			prettyPayload = jsonData
		}

		log.Printf("BulkUpdate: itemID=%s page=%d criteria=%s\nPayload:\n%s\n", itemID, pageCount, criteria, string(prettyPayload))

		var resp *http.Response
		var body []byte
		maxRetries := 15
		var lastErr error

		for attempt := 1; attempt <= maxRetries; attempt++ {
			// Wait for rate limiter token
			<-requestChan

			req, err := http.NewRequest("PATCH", fullURL, bytes.NewBuffer(jsonData))
			if err != nil {
				log.Printf("BulkUpdate: Request creation error for itemID=%s page=%d: %v\n", itemID, pageCount, err)
				lastErr = err
				break
			}
			req.Header.Set("Authorization", "Zoho-oauthtoken "+accessToken)
			req.Header.Set("Content-Type", "application/json")

			resp, err = client.Do(req)
			if err != nil {
				log.Printf("BulkUpdate: Request error for itemID=%s page=%d attempt=%d: %v\n", itemID, pageCount, attempt, err)
				lastErr = err
				// Retry network errors
				if attempt < maxRetries {
					time.Sleep(time.Duration(attempt) * time.Second) // Exponential-ish backoff
				}
				continue
			}

			body, _ = io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout {
				retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))

				log.Printf("BulkUpdate: Rate limit for itemID=%s page=%d status=%s attempt=%d/%d, retry-after=%v body=%s\n",
					itemID, pageCount, resp.Status, attempt, maxRetries, retryAfter, string(body))

				if attempt == maxRetries {
					lastErr = fmt.Errorf("max retries exceeded for rate limit")
					break
				}
				time.Sleep(retryAfter)
				continue
			}

			// Success
			log.Printf("BulkUpdate: Response for itemID=%s page=%d status=%s body=%s\n", itemID, pageCount, resp.Status, string(body))
			lastErr = nil
			break
		}

		if resp == nil {
			log.Printf("BulkUpdate: No response for itemID=%s page=%d after retries, lastErr=%v\n", itemID, pageCount, lastErr)
			break
		}

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
			log.Printf("BulkUpdate: Request failed for itemID=%s page=%d: status=%s body=%s\n", itemID, pageCount, resp.Status, string(body))
			break
		}

		// Parse response to check for more_records
		var respData map[string]interface{}
		err = json.Unmarshal(body, &respData)
		if err != nil {
			log.Printf("BulkUpdate: Failed to parse response for itemID=%s page=%d: %v\n", itemID, pageCount, err)
			break
		}

		// Check if there are more records to process
		moreRecords = false
		if moreRecordsVal, exists := respData["more_records"]; exists {
			if moreRecordsBool, ok := moreRecordsVal.(bool); ok && moreRecordsBool {
				moreRecords = true
				log.Printf("BulkUpdate: MORE RECORDS DETECTED for itemID=%s - paginating (page=%d)\n", itemID, pageCount)
			}
		}
	}

	log.Printf("BulkUpdate: Completed for itemID=%s (total pages processed: %d)\n", itemID, pageCount)
}
