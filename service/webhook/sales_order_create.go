package webhook

import (
	"log"
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

	now := time.Now().UTC()
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

	mappings, err := database.GetSubformMappings(salesOrderID)
	if err != nil {
		log.Printf("Failed to load subform mappings for %s: %v\n", salesOrderID, err)
		mappings = make(map[string]string)
	}

	fetched, err := creator_sync.GetOpsRecord(creatorOpsID)
	if err != nil {
		log.Printf("Creator fetch before update failed for sales order %s record %s: %v\n", salesOrderID, creatorOpsID, err)
		return
	}

	existingPicklist := creator_sync.ExtractPicklistRows(fetched)
	creatorPayload := BuildCreatorPayload(salesorder, mappings, existingPicklist)

	if err := creator_sync.UpdateOpsRecord(creatorOpsID, creatorPayload); err != nil {
		log.Printf("Creator update failed for sales order %s record %s: %v\n", salesOrderID, creatorOpsID, err)
		return
	}

	fetched, err = creator_sync.GetOpsRecord(creatorOpsID)
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

// BuildCreatorPayload merges Books line items with existing Creator Picklist rows.
// Resolution per line: DB mapping by item_id → product code match on GET rows → new row (no ID).
// Picklist rows present in Creator but not in the sales order are preserved (ID-only stubs).
func BuildCreatorPayload(
	salesorder map[string]interface{},
	existingMappings map[string]string,
	existingPicklist []creator_sync.PicklistRow,
) models.CreatorPayload {
	var payload models.CreatorPayload
	payload.Result.Message = true
	payload.Result.Fields = []string{"Picklist"}

	lineItemsRaw, ok := salesorder["line_items"].([]interface{})
	if !ok {
		return payload
	}

	usedSubformIDs := make(map[string]bool)

	for _, itemRaw := range lineItemsRaw {
		item, ok := itemRaw.(map[string]interface{})
		if !ok {
			continue
		}

		itemID := stringVal(item["item_id"])
		name := stringVal(item["name"])
		qty := floatVal(item["quantity"])
		rate := floatVal(item["rate"])
		unit := stringVal(item["unit"])
		amount := qty * rate

		row := models.CreatorSubformRow{
			ProductUniqueCode:   name,
			UsageUnit:           unit,
			Rate:                rate,
			Amount:              amount,
			Qty:                 qty,
			TransferredQuantity: qty,
		}

		subformID := resolveSubformID(itemID, name, existingMappings, existingPicklist, usedSubformIDs)
		if subformID != "" {
			row.ID = subformID
			usedSubformIDs[subformID] = true
		}

		payload.Data.Picklist = append(payload.Data.Picklist, row)
	}

	for _, existing := range existingPicklist {
		if existing.ID == "" || usedSubformIDs[existing.ID] {
			continue
		}
		payload.Data.Picklist = append(payload.Data.Picklist, models.CreatorSubformRow{ID: existing.ID})
	}

	return payload
}

func resolveSubformID(
	itemID, productName string,
	existingMappings map[string]string,
	existingPicklist []creator_sync.PicklistRow,
	usedIDs map[string]bool,
) string {
	if id := existingMappings[itemID]; id != "" && !usedIDs[id] {
		return id
	}
	return matchSubformIDByProductCode(existingPicklist, productName, usedIDs)
}

func persistSubformMappings(
	picklistRows []creator_sync.PicklistRow,
	salesorder map[string]interface{},
	salesOrderID, creatorOpsID string,
) error {
	lineItems := lineItemsWithIDs(salesorder)
	usedIDs := make(map[string]bool)

	for _, li := range lineItems {
		subformID := matchSubformIDByProductCode(picklistRows, li.name, usedIDs)
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

func stringVal(v interface{}) string {
	s, _ := v.(string)
	return s
}

func floatVal(v interface{}) float64 {
	f, _ := v.(float64)
	return f
}
