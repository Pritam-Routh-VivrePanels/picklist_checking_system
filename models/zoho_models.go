package models

import "time"

type TokenJson struct {
	Access_token  string `json:"access_token"`
	Refresh_token string `json:"refresh_token"`
	Expires_in    int    `json:"expires_in"`
	Created_At    int64  `json:"Created_At"`
}

type RefresToken struct {
	Access_token string `json:"access_token"`
	Api_domain   string `json:"api_domain"`
	Token_type   string `json:"token_type"`
	Expires_in   int    `json:"expires_in"`
}

type SalesOrder struct {
	SalesorderID          string
	ItemID                string
	ItemName              string
	Quantity              float64
	Rate                  float64
	ItemSubTotalFormatted string
	Unit                  string
	CreatorOpsID          string
	ReceivedAT            time.Time
	UpdateAT              time.Time
}

type CreatorSubformRow struct {
	ID                  string  `json:"ID,omitempty"`
	ProductUniqueCode   string  `json:"Product_Unique_Code"`
	UsageUnit           string  `json:"Usage_Unit"`
	Rate                float64 `json:"Rate"`
	Amount              float64 `json:"Amount"`
	Qty                 float64 `json:"Qty"`
	TransferredQuantity float64 `json:"Transferred_Quantity"`
}

type CreatorPayload struct {
	Data struct {
		Picklist []CreatorSubformRow `json:"Picklist"`
	} `json:"data"`

	Result struct {
		Message bool     `json:"message"`
		Fields  []string `json:"fields,omitempty"`
	} `json:"result"`

	// SkipWorkflow instructs Creator to skip named workflows when updating.
	SkipWorkflow []string `json:"skip_workflow,omitempty"`
}
