package evidence

import (
	"encoding/json"
	"fmt"
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

func one(rows pgx.Rows) (map[string]any, error) {
	return pgx.CollectOneRow(rows, pgx.RowToMap)
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func itoa(n int) string { return strconv.Itoa(n) }

func anyToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}
