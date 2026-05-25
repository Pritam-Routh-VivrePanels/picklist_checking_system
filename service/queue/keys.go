package queue

import "fmt"

const keyPrefix = "picklist"

func jobsKey(salesOrderID string) string {
	return fmt.Sprintf("%s:so:%s:jobs", keyPrefix, salesOrderID)
}

func activeKey(salesOrderID string) string {
	return fmt.Sprintf("%s:so:%s:active", keyPrefix, salesOrderID)
}

func readyKey() string {
	return keyPrefix + ":so:ready"
}
