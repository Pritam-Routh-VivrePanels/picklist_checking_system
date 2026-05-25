package service

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"picklist_checking_system/models"

	"github.com/joho/godotenv"
)

func GetToken() string {
	err := godotenv.Load()

	if err != nil {
		log.Fatal(err)
	}

	token_json_path := os.Getenv("ZOHO_TOKEN_JSON")

	json_data, _ := os.ReadFile(token_json_path)

	var token_json models.TokenJson
	json.Unmarshal(json_data, &token_json)

	if IsTokenExpired(token_json) {
		log.Println("Token expired, refreshing...")
		RefresToken()

		// Reload updated token
		json_data, _ = os.ReadFile(token_json_path)
		json.Unmarshal(json_data, &token_json)
	}

	return token_json.Access_token
}

func AutoRefres() {
	go func() {
		for {
			err := godotenv.Load()

			if err != nil {
				log.Fatal(err)
			}

			token_json_path := os.Getenv("ZOHO_TOKEN_JSON")

			json_data, _ := os.ReadFile(token_json_path)

			var token models.TokenJson
			json.Unmarshal(json_data, &token)

			expiryTime := token.Created_At.Add(time.Duration(token.Expires_in) * time.Second)

			// refres 5 min before expirey
			refreshTime := expiryTime.Add(-5 * time.Minute)

			sleepDuration := time.Until(refreshTime)

			if sleepDuration <= 0 {
				sleepDuration = 10 * time.Second
			}

			time.Sleep(sleepDuration)

			log.Println("Auto refreshing token ...")
			RefresToken()
		}
	}()
}

func GenrateToken(Code string) int {
	err := godotenv.Load()

	if err != nil {
		log.Fatal("Error Loading .env file")
	}

	token_json_path := os.Getenv("ZOHO_TOKEN_JSON")

	tokenURL := os.Getenv("ZOHO_OAUTH_TOKEN_URL")
	client_id := os.Getenv("ZOHO_CLIENT_ID")
	client_secret := os.Getenv("ZOHO_CLIENT_SECRET")
	redirect_uri := os.Getenv("ZOHO_REDIRECT_URI")

	if tokenURL == "" {
		log.Fatal("ZOHO_OAUTH_TOKEN_URL is not configured")
	}

	formData := url.Values{
		"code":          {Code},
		"client_id":     {client_id},
		"client_secret": {client_secret},
		"redirect_uri":  {redirect_uri},
		"grant_type":    {"authorization_code"},
	}
	body := strings.NewReader(formData.Encode())

	resp, err := http.Post(tokenURL, "application/x-www-form-urlencoded", body)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("token exchange failed: status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}

	var data models.TokenJson

	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		log.Fatalf("token exchange JSON parse failed: %v response=%s", err, string(bodyBytes))
	}

	data.Created_At = time.Now()

	file, _ := json.MarshalIndent(data, "", "    ")
	_ = os.WriteFile(token_json_path, file, 0644)

	expiry_time := 3600

	return expiry_time
}

func RefresToken() int {
	err := godotenv.Load()

	if err != nil {
		log.Fatal("Error Loading .env file")
	}

	token_json_path := os.Getenv("ZOHO_TOKEN_JSON")

	tokenURL := os.Getenv("ZOHO_OAUTH_TOKEN_URL")
	client_id := os.Getenv("ZOHO_CLIENT_ID")
	client_secret := os.Getenv("ZOHO_CLIENT_SECRET")
	redirect_uri := os.Getenv("ZOHO_REDIRECT_URI")

	if tokenURL == "" {
		log.Fatal("ZOHO_OAUTH_TOKEN_URL is not configured")
	}

	json_data, _ := os.ReadFile(token_json_path)

	var token_json models.TokenJson

	json.Unmarshal(json_data, &token_json)

	formData := url.Values{
		"refresh_token": {token_json.Refresh_token},
		"client_id":     {client_id},
		"client_secret": {client_secret},
		"redirect_uri":  {redirect_uri},
		"grant_type":    {"refresh_token"},
	}
	body := strings.NewReader(formData.Encode())

	resp, err := http.Post(tokenURL, "application/x-www-form-urlencoded", body)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("refresh token exchange failed: status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}

	var data models.RefresToken

	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		log.Fatalf("refresh token JSON parse failed: %v response=%s", err, string(bodyBytes))
	}

	// 🚨 SAFETY CHECK (VERY IMPORTANT)
	if data.Access_token == "" {
		log.Println("ERROR: Empty access token received. Response:", string(bodyBytes))
		return 0 // DO NOT overwrite existing token
	}

	// ✅ Update ALL required fields
	token_json.Access_token = data.Access_token
	token_json.Expires_in = data.Expires_in
	token_json.Created_At = time.Now()

	update_json, err := json.MarshalIndent(token_json, "", "    ")
	if err != nil {
		log.Fatal(err)
	}

	if err := os.WriteFile(token_json_path, update_json, 0644); err != nil {
		log.Fatal(err)
	}

	return token_json.Expires_in
}

func IsTokenExpired(token models.TokenJson) bool {
	expiryTime := token.Created_At.Add(time.Duration(token.Expires_in) * time.Second)

	// Refresh 5 minutes before expiry
	buffer := 5 * time.Minute

	return time.Now().After(expiryTime.Add(-buffer))
}
