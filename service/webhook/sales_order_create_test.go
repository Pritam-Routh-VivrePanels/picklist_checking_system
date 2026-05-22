package webhook

import (
	"testing"

	creator_sync "picklist_checking_system/service/creater"
)

func TestBuildCreatorPayload_updateAndNewRows(t *testing.T) {
	salesorder := map[string]interface{}{
		"line_items": []interface{}{
			map[string]interface{}{
				"item_id":  "item-a",
				"name":     "Widget",
				"quantity": 10.0,
				"rate":     5.0,
				"unit":     "pcs",
			},
			map[string]interface{}{
				"item_id":  "item-b",
				"name":     "Gadget",
				"quantity": 2.0,
				"rate":     100.0,
				"unit":     "pcs",
			},
		},
	}

	mappings := map[string]string{
		"item-a": "subform-111",
	}

	existing := []creator_sync.PicklistRow{
		{ID: "subform-111", ProductUniqueCode: "Widget"},
		{ID: "subform-999", ProductUniqueCode: "Manual Extra"},
	}

	payload := BuildCreatorPayload(salesorder, mappings, existing)

	if len(payload.Data.Picklist) != 3 {
		t.Fatalf("expected 3 picklist rows (2 SO + 1 preserved), got %d", len(payload.Data.Picklist))
	}

	if payload.Data.Picklist[0].ID != "subform-111" {
		t.Errorf("row 0: expected ID subform-111, got %q", payload.Data.Picklist[0].ID)
	}
	if payload.Data.Picklist[0].ProductUniqueCode != "Widget" || payload.Data.Picklist[0].Qty != 10 {
		t.Errorf("row 0: unexpected field values %+v", payload.Data.Picklist[0])
	}

	if payload.Data.Picklist[1].ID != "" {
		t.Errorf("row 1 (new item): expected empty ID, got %q", payload.Data.Picklist[1].ID)
	}
	if payload.Data.Picklist[1].ProductUniqueCode != "Gadget" {
		t.Errorf("row 1: expected Gadget, got %q", payload.Data.Picklist[1].ProductUniqueCode)
	}

	preserved := payload.Data.Picklist[2]
	if preserved.ID != "subform-999" {
		t.Errorf("preserved row: expected ID subform-999, got %q", preserved.ID)
	}
	if preserved.ProductUniqueCode != "" {
		t.Errorf("preserved row should be ID-only stub, got ProductUniqueCode=%q", preserved.ProductUniqueCode)
	}
}

func TestBuildCreatorPayload_productCodeFallback(t *testing.T) {
	salesorder := map[string]interface{}{
		"line_items": []interface{}{
			map[string]interface{}{
				"item_id":  "item-x",
				"name":     "Widget",
				"quantity": 1.0,
				"rate":     1.0,
				"unit":     "pcs",
			},
		},
	}

	existing := []creator_sync.PicklistRow{
		{ID: "subform-from-get", ProductUniqueCode: "Widget"},
	}

	payload := BuildCreatorPayload(salesorder, nil, existing)

	if len(payload.Data.Picklist) != 1 {
		t.Fatalf("expected 1 row, got %d", len(payload.Data.Picklist))
	}
	if payload.Data.Picklist[0].ID != "subform-from-get" {
		t.Errorf("expected product-code match ID, got %q", payload.Data.Picklist[0].ID)
	}
}

func TestResolveSubformID_prefersMappingOverProduct(t *testing.T) {
	mappings := map[string]string{"item-1": "mapped-id"}
	existing := []creator_sync.PicklistRow{
		{ID: "other-id", ProductUniqueCode: "Same Name"},
	}
	used := make(map[string]bool)

	id := resolveSubformID("item-1", "Same Name", mappings, existing, used)
	if id != "mapped-id" {
		t.Errorf("expected mapped-id, got %q", id)
	}
}
