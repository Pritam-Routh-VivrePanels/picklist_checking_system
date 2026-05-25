package main

import (
	"log"
	"net/http"

	"github.com/joho/godotenv"

	database "picklist_checking_system/db"
	"picklist_checking_system/handler"
	"picklist_checking_system/service"
	"picklist_checking_system/service/queue"
)

func main() {

	err := godotenv.Load()

	if err != nil {
		log.Fatal("Error Loading .env file")
	}

	go service.AutoRefres()

	// Building new database Connection
	database.DbConnection()

	queue.Init()
	queue.StartWorkers(queue.WorkerCount())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Server is running 🚀"))
	})

	http.HandleFunc("/zoho/webhook", handler.BookWebhook)
	http.HandleFunc("/zoho/auth/url", handler.GenrateAuthUrl)
	http.HandleFunc("/zoho/auth/generate", handler.GenrateAuthToken)

	log.Println("Server is running 🚀 on :9050")

	http.ListenAndServe(":9050", nil)
}
