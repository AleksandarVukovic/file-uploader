package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const rollbackTimeout = 5 * time.Second

type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type TxBegin interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type txKey struct{}

func txFromCtx(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}

type Transactor struct {
	db TxBegin
}

func NewTransactor(db TxBegin) *Transactor {
	return &Transactor{db: db}
}

func (t *Transactor) WithinTx(ctx context.Context, txFunc func(txCtx context.Context) error) error {
	if _, ok := txFromCtx(ctx); ok {
		return txFunc(ctx)
	}

	tx, err := t.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			rollback(ctx, tx)
		}
	}()

	if err := txFunc(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

func rollback(ctx context.Context, tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	_ = tx.Rollback(ctx)
}

type Executor struct {
	db DBTX
}

func NewExecutor(db DBTX) *Executor {
	return &Executor{db: db}
}

func (e *Executor) conn(ctx context.Context) DBTX {
	if tx, ok := txFromCtx(ctx); ok {
		return tx
	}
	return e.db
}

func (e *Executor) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return e.conn(ctx).Exec(ctx, sql, args...)
}

func (e *Executor) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return e.conn(ctx).Query(ctx, sql, args...)
}

func (e *Executor) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return e.conn(ctx).QueryRow(ctx, sql, args...)
}
