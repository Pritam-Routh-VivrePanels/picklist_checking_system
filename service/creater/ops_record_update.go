package creator_sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"picklist_checking_system/models"
	"picklist_checking_system/service"
	"time"

	"github.com/joho/godotenv"
)

const opsSubformField = "Picklist"

func opsRecordURL(recordID string) (string, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("Dotenv load warning:", err)
	}

	baseURL := os.Getenv("ZOHO_CLIENT_URL")
	appName := os.Getenv("ZOHO_CREATOR_APP_NAME")
	reportName := os.Getenv("ZOHO_OPS_REPORT")

	if baseURL == "" || appName == "" || reportName == "" {
		return "", fmt.Errorf("missing ZOHO_CLIENT_URL, ZOHO_CREATOR_APP_NAME, or ZOHO_OPS_REPORT")
	}

	// v2.1 Update/Get Record by ID uses the report link name (same record ID as the parent form).
	url := fmt.Sprintf(
		"%s/creator/v2.1/data/vivrepanelsprivatelimited/%s/report/%s/%s",
		baseURL, appName, reportName, recordID,
	)
	return url, nil
}

func creatorRequestHeaders(extra map[string]string) map[string]string {
	headers := make(map[string]string)
	for k, v := range extra {
		headers[k] = v
	}
	if env := os.Getenv("ZOHO_CREATOR_ENVIRONMENT"); env != "" {
		headers["environment"] = env
	}
	return headers
}

type creatorAPIError struct {
	HTTPStatus int
	Code       int
	Message    string
	Body       string
}

func (e *creatorAPIError) Error() string {
	return fmt.Sprintf("creator API error: status=%d code=%d message=%s", e.HTTPStatus, e.Code, e.Message)
}

func parseCreatorAPIError(statusCode int, respBody []byte) *creatorAPIError {
	err := &creatorAPIError{HTTPStatus: statusCode, Body: string(respBody)}
	var respData map[string]interface{}
	if json.Unmarshal(respBody, &respData) != nil {
		err.Message = string(respBody)
		return err
	}
	switch c := respData["code"].(type) {
	case float64:
		err.Code = int(c)
	case json.Number:
		if n, e := c.Int64(); e == nil {
			err.Code = int(n)
		}
	}
	if msg, ok := respData["message"].(string); ok {
		err.Message = msg
	}
	return err
}

func logCreatorAPIError(method string, apiErr *creatorAPIError) {
	if apiErr == nil {
		return
	}
	log.Printf("creatorHTTPRequest %s: Zoho code=%d message=%q status=%d\n", method, apiErr.Code, apiErr.Message, apiErr.HTTPStatus)
	switch apiErr.Code {
	case 2930:
		log.Printf("creatorHTTPRequest: UPLOAD_RULE_NOT_CONFIGURED — configure upload rules on the parent form file-upload field in Creator, and ensure ZOHO_SCOPE includes ZohoCreator.report.UPDATE\n")
	case 2894:
		log.Printf("creatorHTTPRequest: report not found — verify ZOHO_OPS_REPORT matches the report link name in Creator\n")
	case 3100:
		log.Printf("creatorHTTPRequest: no data / record not found — verify cf_creator_ops_id and record visibility in the report\n")
	}
}

func creatorHTTPRequest(method, url string, body []byte, extraHeaders map[string]string) (map[string]interface{}, error) {
	accessToken := service.GetToken()
	client := &http.Client{Timeout: 60 * time.Second}

	maxRetries := 15
	var lastErr error
	var respBody []byte
	var statusCode int

	for attempt := 1; attempt <= maxRetries; attempt++ {
		<-requestChan

		var bodyReader io.Reader
		if len(body) > 0 {
			bodyReader = bytes.NewBuffer(body)
		}

		req, err := http.NewRequest(method, url, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Zoho-oauthtoken "+accessToken)
		req.Header.Set("Accept", "application/json")
		if len(body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range creatorRequestHeaders(extraHeaders) {
			req.Header.Set(k, v)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			log.Printf("creatorHTTPRequest %s: request error attempt %d: %v\n", method, attempt, err)
			if attempt < maxRetries {
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			continue
		}

		respBody, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		statusCode = resp.StatusCode

		if statusCode == http.StatusTooManyRequests || statusCode == http.StatusServiceUnavailable || statusCode == http.StatusGatewayTimeout {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			log.Printf("creatorHTTPRequest %s: rate limited status=%d attempt=%d retry-after=%v body=%s\n",
				method, statusCode, attempt, retryAfter, string(respBody))
			if attempt == maxRetries {
				lastErr = fmt.Errorf("max retries exceeded for rate limit")
				break
			}
			time.Sleep(retryAfter)
			continue
		}

		log.Printf("creatorHTTPRequest %s: status=%d body=%s\n", method, statusCode, string(respBody))
		lastErr = nil
		break
	}

	if lastErr != nil {
		return nil, lastErr
	}

	var respData map[string]interface{}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &respData); err != nil {
			if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
				apiErr := parseCreatorAPIError(statusCode, respBody)
				logCreatorAPIError(method, apiErr)
				return nil, apiErr
			}
			return nil, fmt.Errorf("parse response: %w", err)
		}
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		apiErr := parseCreatorAPIError(statusCode, respBody)
		if apiErr.Code == 0 {
			if code, ok := respData["code"].(float64); ok {
				apiErr.Code = int(code)
			}
			if msg, ok := respData["message"].(string); ok {
				apiErr.Message = msg
			}
		}
		logCreatorAPIError(method, apiErr)
		return respData, apiErr
	}

	if code, ok := respData["code"].(float64); ok && int(code) != 3000 {
		apiErr := &creatorAPIError{
			HTTPStatus: statusCode,
			Code:       int(code),
			Body:       string(respBody),
		}
		if msg, ok := respData["message"].(string); ok {
			apiErr.Message = msg
		}
		logCreatorAPIError(method, apiErr)
		return respData, apiErr
	}

	return respData, nil
}

// UpdateOpsRecord updates a Zoho Creator ops record by ID (v2.1 Update Record by ID).
func UpdateOpsRecord(recordID string, payload models.CreatorPayload) error {
	url, err := opsRecordURL(recordID)
	if err != nil {
		return err
	}

	if payload.Result.Fields == nil {
		payload.Result.Fields = []string{opsSubformField}
	}
	if payload.SkipWorkflow == nil {
		payload.SkipWorkflow = []string{"form_workflow"}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	log.Printf("UpdateOpsRecord: PATCH %s\nPayload: %s\n", url, string(jsonData))

	_, err = creatorHTTPRequest(http.MethodPatch, url, jsonData, nil)
	return err
}

// GetOpsRecord fetches an ops record by ID including the Picklist subform (v2.1 Get Record by ID).
// Uses field_config=all then detail_view — custom+fields does not return subform data on this app.
func GetOpsRecord(recordID string) (map[string]interface{}, error) {
	url, err := opsRecordURL(recordID)
	if err != nil {
		return nil, err
	}

	var lastResp map[string]interface{}
	for _, fieldConfig := range []string{"all", "detail_view", "quick_view"} {
		log.Printf("GetOpsRecord: GET %s (field_config=%s)\n", url, fieldConfig)

		resp, err := creatorHTTPRequest(http.MethodGet, url, nil, map[string]string{
			"field_config": fieldConfig,
		})
		if err != nil {
			return nil, err
		}
		lastResp = resp

		if HasPicklistData(resp) {
			log.Printf("GetOpsRecord: Picklist found using field_config=%s\n", fieldConfig)
			return resp, nil
		}
		log.Printf("GetOpsRecord: no Picklist in response for field_config=%s (data keys: %v)\n",
			fieldConfig, dataKeys(resp))
	}

	return lastResp, nil
}

// HasPicklistData reports whether the Creator response contains a non-empty Picklist subform array.
func HasPicklistData(resp map[string]interface{}) bool {
	return len(ExtractPicklistRows(resp)) > 0
}

// PicklistRow is a parsed subform row from a Creator GET response.
type PicklistRow struct {
	ID                string
	ProductUniqueCode string
}

// ExtractPicklistRows parses Picklist subform rows from a Get Record response.
func ExtractPicklistRows(resp map[string]interface{}) []PicklistRow {
	if resp == nil {
		return nil
	}
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return nil
	}

	raw := picklistArrayFromData(data)
	if len(raw) == 0 {
		return nil
	}

	var rows []PicklistRow
	for _, r := range raw {
		row, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		id := fieldToString(row["ID"])
		if id == "" {
			continue
		}
		productCode := fieldToString(row["Product_Unique_Code"])
		if productCode == "" {
			productCode = picklistProductCodeFromRow(row)
		}
		rows = append(rows, PicklistRow{
			ID:                id,
			ProductUniqueCode: productCode,
		})
	}
	return rows
}

func picklistArrayFromData(data map[string]interface{}) []interface{} {
	if raw, ok := data[opsSubformField].([]interface{}); ok && len(raw) > 0 {
		return raw
	}

	// Fallback: locate any subform-like array (objects with ID + Product_Unique_Code).
	for key, val := range data {
		arr, ok := val.([]interface{})
		if !ok || len(arr) == 0 {
			continue
		}
		first, ok := arr[0].(map[string]interface{})
		if !ok {
			continue
		}
		if fieldToString(first["ID"]) != "" && (first["Product_Unique_Code"] != nil || first["Product_Unique_Code.zc_display_value"] != nil) {
			log.Printf("ExtractPicklistRows: using subform array at key %q\n", key)
			return arr
		}
	}
	return nil
}

func picklistProductCodeFromRow(row map[string]interface{}) string {
	if v, ok := row["Product_Unique_Code.zc_display_value"].(string); ok && v != "" {
		return v
	}
	if nested, ok := row["Product_Unique_Code"].(map[string]interface{}); ok {
		if v, ok := nested["zc_display_value"].(string); ok {
			return v
		}
		if v := fieldToString(nested["value"]); v != "" {
			return v
		}
	}
	return ""
}

func fieldToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	case json.Number:
		return t.String()
	default:
		return ""
	}
}

func dataKeys(resp map[string]interface{}) []string {
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys
}
