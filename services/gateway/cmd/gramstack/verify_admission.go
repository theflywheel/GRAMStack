package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/admission"
)

var auditRequestID = regexp.MustCompile(`^[A-Z2-7]{26}$`)

func verifyAdmission(ctx context.Context, args []string, output io.Writer) error {
	if len(args) != 1 || !auditRequestID.MatchString(args[0]) || os.Getenv("DATABASE_URL") == "" {
		return errors.New("verify-admission requires DATABASE_URL and one request ID")
	}
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("invalid audit database configuration")
	}
	defer pool.Close()
	result, err := admission.Verify(ctx, pool, args[0])
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}
