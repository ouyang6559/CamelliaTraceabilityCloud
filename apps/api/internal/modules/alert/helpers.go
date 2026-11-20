package alert

import (
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

func collect(rows pgx.Rows) ([]map[string]any, error) {
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if items == nil {
		items = []map[string]any{}
	}
	return items, err
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
