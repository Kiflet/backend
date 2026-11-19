// Package database holds what every module's store shares: the connection
// a query runs on, which may be a transaction another module began, and
// paging.
package database

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Conn is a pool, a connection or a transaction: what sqlc's generated
// stores run their queries on (each store's DBTX has these methods). A
// module that writes within another's transaction is handed the
// transaction as a Conn.
type Conn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Page converts a 1-based page into LIMIT and OFFSET, with 25 rows a page
// unless asked otherwise, and 200 at most.
func Page(page, perPage int) (limit, offset int32) {
	if perPage <= 0 {
		perPage = 25
	}
	if perPage > 200 {
		perPage = 200
	}
	if page < 1 {
		page = 1
	}
	return int32(perPage), int32((page - 1) * perPage)
}
