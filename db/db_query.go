package database

import (
	"database/sql"
	"log"
	"time"

	"picklist_checking_system/models"
)

func UpsertSalesOrderLine(entry models.SalesOrder) error {
	query := `
	IF EXISTS (SELECT 1 FROM dbo.Sales_Order WHERE salesorder_id = @p1 AND item_id = @p2)
		UPDATE dbo.Sales_Order SET
			name = @p3,
			quantity = @p4,
			rate = @p5,
			item_sub_total_formatted = @p6,
			unit = @p7,
			creator_ops_id = @p8,
			updated_at = SYSUTCDATETIME()
		WHERE salesorder_id = @p1 AND item_id = @p2;
	ELSE
		INSERT INTO dbo.Sales_Order (
			salesorder_id, item_id, name, quantity, rate,
			item_sub_total_formatted, unit, creator_ops_id
		) VALUES (
			@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8
		);
	`

	_, err := db.Exec(query,
		entry.SalesorderID,
		entry.ItemID,
		entry.ItemName,
		entry.Quantity,
		entry.Rate,
		entry.ItemSubTotalFormatted,
		entry.Unit,
		entry.CreatorOpsID,
	)
	if err != nil {
		log.Println("UpsertSalesOrderLine error:", err)
	}
	return err
}

func SalesOrderExists(salesOrderID string) (bool, error) {
	var exists int
	err := db.QueryRow(
		`SELECT 1 FROM dbo.Sales_Order WHERE salesorder_id = @p1`,
		salesOrderID,
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		log.Println("SalesOrderExists error:", err)
		return false, err
	}
	return true, nil
}

func GetSubformMappings(salesOrderID string) (map[string]string, error) {
	query := `
	SELECT zoho_item_id, creator_subform_row_id
	FROM dbo.sales_order_subform_mapping
	WHERE sales_order_id = @p1 AND creator_subform_row_id IS NOT NULL AND creator_subform_row_id <> ''
	`

	rows, err := db.Query(query, salesOrderID)
	if err != nil {
		log.Println("GetSubformMappings error:", err)
		return nil, err
	}
	defer rows.Close()

	mappings := make(map[string]string)
	for rows.Next() {
		var itemID, subformRowID string
		if err := rows.Scan(&itemID, &subformRowID); err != nil {
			log.Println("GetSubformMappings scan error:", err)
			continue
		}
		mappings[itemID] = subformRowID
	}
	return mappings, rows.Err()
}

func UpsertSubformMapping(salesOrderID, zohoItemID, parentRecordID, subformRowID string) error {
	query := `
	IF EXISTS (SELECT 1 FROM dbo.sales_order_subform_mapping WHERE sales_order_id = @p1 AND zoho_item_id = @p2)
		UPDATE dbo.sales_order_subform_mapping SET
			creator_parent_record_id = @p3,
			creator_subform_row_id = @p4,
			updated_at = SYSUTCDATETIME()
		WHERE sales_order_id = @p1 AND zoho_item_id = @p2;
	ELSE
		INSERT INTO dbo.sales_order_subform_mapping (
			sales_order_id, zoho_item_id, creator_parent_record_id, creator_subform_row_id
		) VALUES (@p1, @p2, @p3, @p4);
	`

	_, err := db.Exec(query, salesOrderID, zohoItemID, parentRecordID, subformRowID)
	if err != nil {
		log.Println("UpsertSubformMapping error:", err)
	}
	return err
}

// SaveWebhookLog is deprecated; use UpsertSalesOrderLine.
func SaveWebhookLog(logEntry models.SalesOrder) error {
	if logEntry.ReceivedAT.IsZero() {
		logEntry.ReceivedAT = time.Now()
	}
	return UpsertSalesOrderLine(logEntry)
}

// EnsureDB returns the underlying sql.DB for health checks (optional).
func EnsureDB() *sql.DB {
	return db
}
