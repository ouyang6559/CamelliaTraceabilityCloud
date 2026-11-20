package ai

import (
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5"
)

func collect(rows pgx.Rows) ([]map[string]any, error) {
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if items == nil {
		items = []map[string]any{}
	}
	return items, err
}

func firstRow(rows pgx.Rows) (map[string]any, error) {
	item, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	return item, err
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

var _ = strconv.Itoa
