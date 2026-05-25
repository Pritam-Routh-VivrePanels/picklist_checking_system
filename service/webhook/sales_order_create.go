package webhook

import (
	"log"
	"math"
	"time"

	database "picklist_checking_system/db"
	"picklist_checking_system/models"
	creator_sync "picklist_checking_system/service/creater"
)

func HandleWebhook(payload map[string]interface{}) {
	salesorder, ok := payload["salesorder"].(map[string]interface{})
	if !ok {
		log.Println("Error: top-level 'salesorder' object not found")
		return
	}

	salesOrderID, _ := salesorder["salesorder_id"].(string)
	if salesOrderID == "" {
		log.Println("Warning: salesorder_id not found or empty")
		return
	}

	var creatorOpsID string
	if customFields, ok := salesorder["custom_field_hash"].(map[string]interface{}); ok {
		creatorOpsID, _ = customFields["cf_creator_ops_id"].(string)
	}

	lineItemsRaw, ok := salesorder["line_items"].([]interface{})
	if !ok {
		log.Println("Error: line_items not found or not an array")
		return
	}

	salesOrderExists, err := database.SalesOrderExists(salesOrderID)
	if err != nil {
		log.Printf("Failed to check if sales order %s exists: %v\n", salesOrderID, err)
	}

	now := time.Now()
	for i, itemRaw := range lineItemsRaw {
		item, ok := itemRaw.(map[string]interface{})
		if !ok {
			log.Printf("Skipping item at index %d: invalid format\n", i)
			continue
		}

		entry := models.SalesOrder{
			SalesorderID:          salesOrderID,
			ItemID:                stringVal(item["item_id"]),
			ItemName:              stringVal(item["name"]),
			Quantity:              floatVal(item["quantity"]),
			Rate:                  floatVal(item["rate"]),
			ItemSubTotalFormatted: stringVal(item["item_sub_total_formatted"]),
			Unit:                  stringVal(item["unit"]),
			CreatorOpsID:          creatorOpsID,
			ReceivedAT:            now,
			UpdateAT:              now,
		}

		if err := database.UpsertSalesOrderLine(entry); err != nil {
			log.Printf("Failed to persist line item %s for sales order %s: %v\n", entry.ItemID, salesOrderID, err)
		}
	}

	if creatorOpsID == "" {
		log.Printf("No cf_creator_ops_id for sales order %s; skipping Creator update\n", salesOrderID)
		return
	}

	if salesOrderExists {
		updateExistingCreatorRecord(salesorder, salesOrderID, creatorOpsID)
		return
	}

	updateNewCreatorRecord(salesorder, salesOrderID, creatorOpsID)
}

func updateNewCreatorRecord(salesorder map[string]interface{}, salesOrderID, creatorOpsID string) {
	mappings, err := database.GetSubformMappings(salesOrderID)
	if err != nil {
		log.Printf("Failed to load subform mappings for %s: %v\n", salesOrderID, err)
		mappings = make(map[string]string)
	}

	if err := creator_sync.ClearOpsPicklist(creatorOpsID); err != nil {
		log.Printf("Creator clear Picklist failed for sales order %s record %s: %v\n", salesOrderID, creatorOpsID, err)
		return
	}

	creatorPayload := BuildCreatorPayload(salesorder, mappings, nil, false)
	if err := creator_sync.UpdateOpsRecord(creatorOpsID, creatorPayload); err != nil {
		log.Printf("Creator update failed for sales order %s record %s: %v\n", salesOrderID, creatorOpsID, err)
		return
	}

	fetched, err := creator_sync.GetOpsRecord(creatorOpsID)
	if err != nil {
		log.Printf("Creator fetch after update failed for sales order %s record %s: %v\n", salesOrderID, creatorOpsID, err)
		return
	}

	picklistRows := creator_sync.ExtractPicklistRows(fetched)
	if len(picklistRows) == 0 {
		log.Printf("Creator GET returned no Picklist rows for sales order %s record %s\n", salesOrderID, creatorOpsID)
		return
	}

	if err := persistSubformMappings(picklistRows, salesorder, salesOrderID, creatorOpsID); err != nil {
		log.Printf("Failed to persist subform mappings for %s: %v\n", salesOrderID, err)
	}
}

func updateExistingCreatorRecord(salesorder map[string]interface{}, salesOrderID, creatorOpsID string) {
	fetched, err := creator_sync.GetOpsRecord(creatorOpsID)
	if err != nil {
		log.Printf("Creator GET before update failed for sales order %s record %s: %v\n", salesOrderID, creatorOpsID, err)
		return
	}

	picklistRows := creator_sync.ExtractPicklistRows(fetched)

	mappings, err := database.GetSubformMappings(salesOrderID)
	if err != nil {
		log.Printf("Failed to load subform mappings for %s: %v\n", salesOrderID, err)
		mappings = make(map[string]string)
	}

	preservedByItemID := preservedPicklistByItem(salesorder, mappings, picklistRows)

	if err := creator_sync.ClearOpsPicklist(creatorOpsID); err != nil {
		log.Printf("Creator clear Picklist failed for sales order %s record %s: %v\n", salesOrderID, creatorOpsID, err)
		return
	}

	creatorPayload := BuildCreatorPayload(salesorder, nil, preservedByItemID, true)
	if err := creator_sync.UpdateOpsRecord(creatorOpsID, creatorPayload); err != nil {
		log.Printf("Creator update failed for sales order %s record %s: %v\n", salesOrderID, creatorOpsID, err)
		return
	}

	fetched, err = creator_sync.GetOpsRecord(creatorOpsID)
	if err != nil {
		log.Printf("Creator fetch after update failed for sales order %s record %s: %v\n", salesOrderID, creatorOpsID, err)
		return
	}

	picklistRows = creator_sync.ExtractPicklistRows(fetched)
	if len(picklistRows) == 0 {
		log.Printf("Creator GET returned no Picklist rows for sales order %s record %s\n", salesOrderID, creatorOpsID)
		return
	}

	if err := persistSubformMappings(picklistRows, salesorder, salesOrderID, creatorOpsID); err != nil {
		log.Printf("Failed to persist subform mappings for %s: %v\n", salesOrderID, err)
	}
}

type preservedPicklistValues struct {
	TransferredQuantity float64
	Amount              float64
	Rate                float64
}

func preservedPicklistByItem(
	salesorder map[string]interface{},
	mappings map[string]string,
	picklistRows []creator_sync.PicklistRow,
) map[string]preservedPicklistValues {
	picklistByID := make(map[string]creator_sync.PicklistRow, len(picklistRows))
	for _, row := range picklistRows {
		picklistByID[row.ID] = row
	}

	usedIDs := make(map[string]bool)
	result := make(map[string]preservedPicklistValues)

	lineItemsRaw, ok := salesorder["line_items"].([]interface{})
	if !ok {
		return result
	}

	for _, itemRaw := range lineItemsRaw {
		item, ok := itemRaw.(map[string]interface{})
		if !ok {
			continue
		}

		itemID := stringVal(item["item_id"])
		name := stringVal(item["name"])

		subformID := ""
		if mappedID, exists := mappings[itemID]; exists {
			subformID = mappedID
		}
		if subformID == "" {
			subformID = matchSubformIDByProductCode(picklistRows, name, usedIDs)
		}
		if subformID == "" {
			continue
		}
		usedIDs[subformID] = true

		row, ok := picklistByID[subformID]
		if !ok {
			continue
		}

		result[itemID] = preservedPicklistValues{
			TransferredQuantity: row.TransferredQuantity,
			Amount:              row.Amount,
			Rate:                row.Rate,
		}
	}

	return result
}

func BuildCreatorPayload(
	salesorder map[string]interface{},
	existingMappings map[string]string,
	preservedByItemID map[string]preservedPicklistValues,
	omitSubformIDs bool,
) models.CreatorPayload {
	var payload models.CreatorPayload
	payload.Result.Message = true
	payload.Result.Fields = []string{"Picklist"}

	lineItemsRaw, ok := salesorder["line_items"].([]interface{})
	if !ok {
		return payload
	}

	for _, itemRaw := range lineItemsRaw {
		item, ok := itemRaw.(map[string]interface{})
		if !ok {
			continue
		}

		itemID := stringVal(item["item_id"])
		name := stringVal(item["name"])
		qty := roundQty(floatVal(item["quantity"]))
		rate := roundCurrency(floatVal(item["rate"]))
		unit := stringVal(item["unit"])
		amount := lineItemAmount(item, qty, rate)
		transferred := qty

		if preservedByItemID != nil {
			if p, ok := preservedByItemID[itemID]; ok {
				transferred = roundQty(p.TransferredQuantity)
				amount = roundCurrency(p.Amount)
				if p.Rate != 0 {
					rate = roundCurrency(p.Rate)
				}
			}
		}

		row := models.CreatorSubformRow{
			ProductUniqueCode:   name,
			UsageUnit:           unit,
			Rate:                rate,
			Amount:              amount,
			Qty:                 qty,
			TransferredQuantity: transferred,
		}

		if !omitSubformIDs && existingMappings != nil {
			if creatorSubformID, exists := existingMappings[itemID]; exists {
				row.ID = creatorSubformID
			}
		}

		payload.Data.Picklist = append(payload.Data.Picklist, row)
	}

	return payload
}

func persistSubformMappings(
	picklistRows []creator_sync.PicklistRow,
	salesorder map[string]interface{},
	salesOrderID, creatorOpsID string,
) error {
	lineItems := lineItemsWithIDs(salesorder)
	usedIDs := make(map[string]bool)

	for i, li := range lineItems {
		subformID := ""
		if i < len(picklistRows) && picklistRows[i].ID != "" && !usedIDs[picklistRows[i].ID] {
			subformID = picklistRows[i].ID
		}
		if subformID == "" {
			subformID = matchSubformIDByProductCode(picklistRows, li.name, usedIDs)
		}
		if subformID == "" {
			log.Printf("No subform row ID returned for item %s (%s); picklist rows=%d\n",
				li.itemID, li.name, len(picklistRows))
			continue
		}
		usedIDs[subformID] = true
		log.Printf("Mapped item %s (%s) -> subform ID %s\n", li.itemID, li.name, subformID)
		if err := database.UpsertSubformMapping(salesOrderID, li.itemID, creatorOpsID, subformID); err != nil {
			return err
		}
	}
	return nil
}

type lineItemRef struct {
	itemID string
	name   string
}

func lineItemsWithIDs(salesorder map[string]interface{}) []lineItemRef {
	lineItemsRaw, ok := salesorder["line_items"].([]interface{})
	if !ok {
		return nil
	}
	var refs []lineItemRef
	for _, itemRaw := range lineItemsRaw {
		item, ok := itemRaw.(map[string]interface{})
		if !ok {
			continue
		}
		refs = append(refs, lineItemRef{
			itemID: stringVal(item["item_id"]),
			name:   stringVal(item["name"]),
		})
	}
	return refs
}

func matchSubformIDByProductCode(rows []creator_sync.PicklistRow, productName string, usedIDs map[string]bool) string {
	for _, r := range rows {
		if r.ProductUniqueCode == productName && r.ID != "" && !usedIDs[r.ID] {
			return r.ID
		}
	}
	return ""
}

func lineItemAmount(item map[string]interface{}, qty, rate float64) float64 {
	for _, key := range []string{"item_total", "line_item_total", "item_sub_total"} {
		if v := floatVal(item[key]); v != 0 {
			return roundCurrency(v)
		}
	}
	return roundCurrency(qty * rate)
}

func roundCurrency(f float64) float64 {
	return math.Round(f*100) / 100
}

func roundQty(f float64) float64 {
	return math.Round(f*10000) / 10000
}

func stringVal(v interface{}) string {
	s, _ := v.(string)
	return s
}

func floatVal(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return 0
	}
}
