package database

import "log"

func enableAutoCreateTables() {
	createSalesOrder := `IF OBJECT_ID(N'dbo.Sales_Order', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.Sales_Order (
        id BIGINT IDENTITY(1,1) PRIMARY KEY,
        salesorder_id NVARCHAR(255) NOT NULL,
        item_id NVARCHAR(255) NOT NULL,
        name NVARCHAR(500),
        quantity DECIMAL(18,4),
        rate DECIMAL(18,4),
        item_sub_total_formatted NVARCHAR(100),
        unit NVARCHAR(100),
        creator_ops_id NVARCHAR(255),
        received_at DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
        updated_at DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
        CONSTRAINT UQ_Sales_Order UNIQUE (salesorder_id, item_id)
    );
END`

	createSalesorderSubformMapping := `IF OBJECT_ID(N'dbo.sales_order_subform_mapping', N'U') IS NULL
BEGIN
    CREATE TABLE dbo.sales_order_subform_mapping (
        id BIGINT IDENTITY(1,1) PRIMARY KEY,
        sales_order_id NVARCHAR(255) NOT NULL,
        zoho_item_id NVARCHAR(255) NOT NULL,
        creator_parent_record_id NVARCHAR(255),
        creator_subform_row_id NVARCHAR(255),
        created_at DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
        updated_at DATETIME2 NOT NULL DEFAULT SYSUTCDATETIME(),
        CONSTRAINT UQ_sales_order_subform_mapping UNIQUE (sales_order_id, zoho_item_id)
    );
END`

	execSQL(createSalesOrder)
	execSQL(createSalesorderSubformMapping)
}

func execSQL(query string) {
	_, err := db.Exec(query)
	if err != nil {
		log.Fatal("Error creating table: ", err)
	}
}
