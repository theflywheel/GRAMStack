package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/migrations"
	"io"
	"os"
)

func migrate(ctx context.Context, args []string, output io.Writer) error {
	if len(args) != 0 {
		return errors.New("migrate takes no positional arguments")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL required")
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer pool.Close()
	if err := migrations.Apply(ctx, pool); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, "migrations applied")
	return err
}
