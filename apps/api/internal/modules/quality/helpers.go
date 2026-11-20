package quality

import (
	"strconv"

	"github.com/jackc/pgx/v5"
)

func strconvItoa(n int) string { return strconv.Itoa(n) }

func collect(rows pgx.Rows) ([]map[string]any, error) {
	items, err := pgx.CollectRows(rows, pgx.RowToMap)
	if items == nil {
		items = []map[string]any{}
	}
	return items, err
}
