// Package outbox appends safe metadata events inside the caller's transaction.
package outbox

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

func Append(ctx context.Context, tx pgx.Tx, kind, id string, version int64, event string, payload map[string]any) error {
	eventID := rand.Text()
	payload["event_id"] = eventID
	payload["status_version"] = version
	payload["occurred_at"] = time.Now().UTC()
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox(event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload) VALUES($1,$2,$3,$4,$5,$6)`, eventID, kind, id, version, event, raw)
	return err
}
