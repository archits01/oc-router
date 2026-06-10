package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
)

// clientFromContext
//
//
// -
// -
//
//
//	func (r *someRepo) SomeMethod(ctx context.Context) error {
//	    client := clientFromContext(ctx, r.client)
//	    return client.SomeEntity.Create().Save(ctx)
//	}
func clientFromContext(ctx context.Context, defaultClient *dbent.Client) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return defaultClient
}

// translatePersistenceError
//
//
//
//
//
//   - err:
//   - notFound:
//   - conflict:
//
//
//
//	err := translatePersistenceError(dbErr, service.ErrUserNotFound, service.ErrEmailExists)
func translatePersistenceError(err error, notFound, conflict *infraerrors.ApplicationError) error {
	if err == nil {
		return nil
	}

	//
	// Ent
	if notFound != nil && (errors.Is(err, sql.ErrNoRows) || dbent.IsNotFound(err)) {
		return notFound.WithCause(err)
	}

	if conflict != nil && isUniqueConstraintViolation(err) {
		return conflict.WithCause(err)
	}

	return err
}

// isUniqueConstraintViolation
//
//  1. PostgreSQL
//
//
func isUniqueConstraintViolation(err error) bool {
	if err == nil {
		return false
	}

	//
	//
	//
	var pgErr *pq.Error
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}

	//
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate entry")
}
