package authorization

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

type PolicyKey struct{ Registry, Consumer, Resource, Purpose string }
type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func keyOf(p Policy) PolicyKey  { return PolicyKey{p.Registry, p.Consumer, p.Resource, p.Purpose} }
func (k PolicyKey) args() []any { return []any{k.Registry, k.Consumer, k.Resource, k.Purpose} }
func (s *Service) signedPolicy(compact string) (Policy, string, error) {
	var p Policy
	if len(compact) > 128*1024 {
		return p, "", ErrBinding
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return p, "", ErrBinding
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return p, "", ErrBinding
	}
	// An unverified registry selects only a configured key set, never a trust URL.
	var lookup struct {
		Registry string `json:"registry"`
	}
	if json.Unmarshal(raw, &lookup) != nil {
		return p, "", ErrBinding
	}
	payload, err := verify(compact, PolicyType, s.config.PolicyKeys[lookup.Registry], &p)
	if err != nil || p.validate() != nil {
		return Policy{}, "", ErrBinding
	}
	return p, cm.Digest(payload), nil
}
func (s *Service) InstallPolicy(ctx context.Context, compact, expected string) error {
	p, digest, err := s.signedPolicy(compact)
	if err != nil {
		return err
	}
	if expected != "" && !idPattern.MatchString(expected) {
		return ErrBinding
	}
	k := keyOf(p)
	return pgx.BeginFunc(ctx, s.config.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "policy:"+p.Registry+":"+p.Consumer+":"+p.Resource+":"+p.Purpose); err != nil {
			return err
		}
		var current, oldDigest string
		err := tx.QueryRow(ctx, `SELECT h.version,v.policy_digest FROM registry_policy_heads h JOIN registry_policy_versions v USING(registry,consumer_id,resource,purpose,version) WHERE h.registry=$1 AND h.consumer_id=$2 AND h.resource=$3 AND h.purpose=$4 FOR UPDATE OF h`, k.args()...).Scan(&current, &oldDigest)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if current == p.Version {
			if oldDigest == digest {
				return nil
			}
			return ErrConflict
		}
		if current != expected {
			return ErrConflict
		}
		var exists bool
		args := append(k.args(), p.Version)
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM registry_policy_versions WHERE registry=$1 AND consumer_id=$2 AND resource=$3 AND purpose=$4 AND version=$5)`, args...).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrConflict
		} // A superseded version can never revive old grants.
		args = append(args, digest, compact)
		if _, err = tx.Exec(ctx, `INSERT INTO registry_policy_versions(registry,consumer_id,resource,purpose,version,policy_digest,jws) VALUES($1,$2,$3,$4,$5,$6,$7)`, args...); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO registry_policy_heads(registry,consumer_id,resource,purpose,version) VALUES($1,$2,$3,$4,$5) ON CONFLICT(registry,consumer_id,resource,purpose) DO UPDATE SET version=excluded.version,enabled=true`, args[:5]...)
		return err
	})
}
func (s *Service) DisablePolicy(ctx context.Context, k PolicyKey, expected string) error {
	if !idPattern.MatchString(expected) {
		return ErrBinding
	}
	args := append(k.args(), expected)
	result, err := s.config.Pool.Exec(ctx, `UPDATE registry_policy_heads SET enabled=false WHERE registry=$1 AND consumer_id=$2 AND resource=$3 AND purpose=$4 AND version=$5`, args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
func (s *Service) policy(ctx context.Context, q queryer, k PolicyKey, locked bool) (Policy, string, error) {
	sql := `SELECT h.version,h.enabled,v.policy_digest,v.jws FROM registry_policy_heads h JOIN registry_policy_versions v USING(registry,consumer_id,resource,purpose,version) WHERE h.registry=$1 AND h.consumer_id=$2 AND h.resource=$3 AND h.purpose=$4`
	if locked {
		sql += " FOR SHARE OF h"
	}
	var version, digest, compact string
	var enabled bool
	if err := q.QueryRow(ctx, sql, k.args()...).Scan(&version, &enabled, &digest, &compact); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Policy{}, "", ErrDenied
		}
		return Policy{}, "", err
	}
	if !enabled {
		return Policy{}, "", ErrDenied
	}
	p, verified, err := s.signedPolicy(compact)
	if err != nil || keyOf(p) != k || p.Version != version || verified != digest {
		return Policy{}, "", ErrDenied
	}
	return p, digest, nil
}
