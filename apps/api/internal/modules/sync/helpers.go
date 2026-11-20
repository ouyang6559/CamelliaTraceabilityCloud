package sync

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

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }
