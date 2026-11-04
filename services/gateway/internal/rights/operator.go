package rights

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"time"

	"github.com/gowebpki/jcs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/looplab/fsm"
	"github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
)

var actorID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var caseID = regexp.MustCompile(`^[A-Z2-7]{26}$`)
var errConflict = errors.New("case transition conflict")

type queuedCase struct {
	ID        string    `json:"case_id"`
	Type      string    `json:"case_type"`
	Status    string    `json:"status"`
	Version   int64     `json:"status_version"`
	CreatedAt time.Time `json:"created_at"`
	DueAt     time.Time `json:"due_at"`
}

type update struct {
	Action    string `json:"action"`
	Exception string `json:"exception_code,omitempty"`
}

type acknowledgement struct {
	update
	Status     string    `json:"status"`
	Version    int64     `json:"status_version"`
	RecordedAt time.Time `json:"recorded_at"`
}

func (u update) valid() bool {
	switch u.Action {
	case "acknowledge", "resolve":
		return u.Exception == ""
	case "record_exception":
		return u.Exception == "legal_hold" || u.Exception == "statutory_retention" || u.Exception == "insufficient_information"
	default:
		return false
	}
}

func nextStatus(ctx context.Context, state, action string) (string, error) {
	machine := fsm.NewFSM(state, fsm.Events{
		{Name: "acknowledge", Src: []string{"queued"}, Dst: "acknowledged"},
		{Name: "resolve", Src: []string{"acknowledged"}, Dst: "resolved"},
		{Name: "record_exception", Src: []string{"acknowledged"}, Dst: "exception"},
	}, nil)
	if err := machine.Event(ctx, action); err != nil {
		return "", errConflict
	}
	return machine.Current(), nil
}

func OperatorHandler(pool *pgxpool.Pool, authority *authorization.Service, registries map[string]string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/rights-cases/{role}/{actor}", func(w http.ResponseWriter, r *http.Request) {
		role, actor, ok := operator(r, authority, registries)
		if !ok {
			http.NotFound(w, r)
			return
		}
		rows, err := pool.Query(r.Context(), `SELECT case_id,case_type,status,status_version,created_at,due_at
			FROM rights_cases WHERE responsible_role=$1 AND responsible_id=$2 AND status IN ('queued','acknowledged')
			ORDER BY created_at,case_id LIMIT 25`, role, actor)
		if err != nil {
			http.Error(w, "Case queue temporarily unavailable.", http.StatusServiceUnavailable)
			return
		}
		cases, err := pgx.CollectRows(rows, pgx.RowToStructByPos[queuedCase])
		if err != nil {
			http.Error(w, "Case queue temporarily unavailable.", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, cases)
	})
	mux.HandleFunc("PUT /internal/v1/rights-cases/{role}/{actor}/{case}/{event}", func(w http.ResponseWriter, r *http.Request) {
		role, actor, ok := operator(r, authority, registries)
		id, event := r.PathValue("case"), r.PathValue("event")
		if !ok || !caseID.MatchString(id) || !actorID.MatchString(event) {
			http.NotFound(w, r)
			return
		}
		media, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		raw, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
		var input update
		_, shapeErr := jcs.Transform(raw)
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if mediaErr != nil || media != "application/json" || readErr != nil || shapeErr != nil || decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !input.valid() {
			http.Error(w, "Invalid case update.", http.StatusBadRequest)
			return
		}
		result, err := apply(r.Context(), pool, role, actor, id, event, authorization.PresenterThumbprint(r), input)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			http.NotFound(w, r)
		case errors.Is(err, errConflict):
			http.Error(w, "Case update conflicts with the recorded state.", http.StatusConflict)
		case err != nil:
			http.Error(w, "Case update temporarily unavailable.", http.StatusServiceUnavailable)
		default:
			writeJSON(w, result)
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func operator(r *http.Request, authority *authorization.Service, registries map[string]string) (string, string, bool) {
	role, actor := r.PathValue("role"), r.PathValue("actor")
	if !actorID.MatchString(actor) || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return "", "", false
	}
	switch role {
	case "recipient":
		return role, actor, authority.IsRecipient(r, actor)
	case "registry":
		thumb := authorization.PresenterThumbprint(r)
		return role, actor, thumb != "" && registries[actor] == thumb
	default:
		return "", "", false
	}
}

func apply(ctx context.Context, pool *pgxpool.Pool, role, actor, id, event, thumb string, input update) (acknowledgement, error) {
	var result acknowledgement
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var state string
		var version int64
		err := tx.QueryRow(ctx, `SELECT status,status_version FROM rights_cases
			WHERE case_id=$1 AND responsible_role=$2 AND responsible_id=$3 FOR UPDATE`, id, role, actor).Scan(&state, &version)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `SELECT action,exception_code,status,status_version,recorded_at
			FROM rights_case_events WHERE case_id=$1 AND event_id=$2`, id, event).
			Scan(&result.Action, &result.Exception, &result.Status, &result.Version, &result.RecordedAt)
		if err == nil {
			if result.update != input {
				return errConflict
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		next, err := nextStatus(ctx, state, input.Action)
		if err != nil {
			return err
		}
		result = acknowledgement{update: input, Status: next, Version: version + 1}
		if _, err = tx.Exec(ctx, `UPDATE rights_cases SET status=$2,status_version=$3,exception_code=$4 WHERE case_id=$1`,
			id, result.Status, result.Version, result.Exception); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO rights_case_events
			(case_id,event_id,action,exception_code,status,status_version,actor_thumbprint)
			VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING recorded_at`, id, event, input.Action, input.Exception, next, result.Version, thumb).
			Scan(&result.RecordedAt)
	})
	return result, err
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
