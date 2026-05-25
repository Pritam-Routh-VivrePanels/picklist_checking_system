package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"picklist_checking_system/service/queue"
)

func BookWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	defer r.Body.Close()

	var booksPayload map[string]interface{}

	err := json.NewDecoder(r.Body).Decode(&booksPayload)
	if err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := queue.Enqueue(booksPayload); err != nil {
		log.Printf("Failed to enqueue webhook: %v", err)
		http.Error(w, "Failed to queue webhook", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "Webhook accepted and queued",
		"response": 202,
	})
	
	if err != nil {
		http.Error(w, "Response encoding failed", http.StatusInternalServerError)
	}
}
