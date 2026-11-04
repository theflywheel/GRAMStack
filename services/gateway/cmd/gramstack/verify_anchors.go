package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/dedi"
)

// verifyAnchors reads existing evidence and emits counts only. It performs no
// migrations, network calls to DeDi, or credential-bearing publication.
func verifyAnchors(ctx context.Context, args []string, output io.Writer) error {
	if len(args) != 0 || os.Getenv("DATABASE_URL") == "" {
		return errors.New("verify-anchors requires DATABASE_URL and no arguments")
	}
	verifier, err := dedi.NewVerifier(dedi.VerificationConfig{
		Namespace: os.Getenv("DEDI_NAMESPACE"), Registry: os.Getenv("DEDI_REGISTRY"),
		PublisherKeyID: os.Getenv("DEDI_PUBLISHER_KEY_ID"), CheckpointOrigin: os.Getenv("DEDI_CHECKPOINT_ORIGIN"),
		CheckpointVerifier: os.Getenv("DEDI_CHECKPOINT_VERIFIER"),
	})
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("invalid evidence database configuration")
	}
	defer pool.Close()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return errors.New("evidence database unavailable")
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
 SELECT r.receipt_id, r.evidence_digest, a.status, a.anchor_reference,
        (SELECT count(*) FROM outbox e WHERE e.aggregate_type='anchor'
         AND e.aggregate_id=r.receipt_id AND e.event_type='evidence.anchored')
 FROM evidence_receipts r LEFT JOIN evidence_anchor_state a USING(receipt_id)
 LIMIT 1001`)
	if err != nil {
		return errors.New("cannot read evidence projections")
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var id, digest string
		var status *string
		var raw []byte
		var events int
		if err := rows.Scan(&id, &digest, &status, &raw, &events); err != nil {
			return errors.New("cannot read retained evidence")
		}
		var proof dedi.Anchor
		if status == nil || *status != "anchored" || events != 1 ||
			json.Unmarshal(raw, &proof) != nil || verifier.VerifyAnchor(id, digest, proof) != nil {
			return errors.New("evidence is pending or failed offline verification")
		}
		count++
		if count > 1000 {
			return errors.New("demo evidence verification is limited to 1000 receipts")
		}
	}
	if rows.Err() != nil {
		return errors.New("cannot finish evidence verification")
	}
	if count == 0 {
		return errors.New("no evidence receipts available")
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("cannot finish the evidence snapshot")
	}
	return json.NewEncoder(output).Encode(struct {
		Verified int    `json:"verified_commitments"`
		Mode     string `json:"mode"`
	}{count, "offline"})
}
