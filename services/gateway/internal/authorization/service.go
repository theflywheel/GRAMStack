package authorization

import (
	"crypto/ecdsa"
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	cm "github.com/theflywheel/GRAMStack/services/gateway/internal/consentmanager"
)

type Adapter struct{ Audience, PeerName string }
type Config struct {
	Pool          *pgxpool.Pool
	Subjects      cm.SubjectResolver
	Terms         cm.TermsResolver
	Consents      *cm.Verifier
	Signer        Signer
	GrantKeys     map[string]*ecdsa.PublicKey
	PolicyKeys    map[string]map[string]*ecdsa.PublicKey
	Adapters      map[string]Adapter
	Presenters    map[string]string
	Issuer        string
	AllowLoopback bool
}
type Service struct{ config Config }

func New(c Config) (*Service, error) {
	u, err := url.Parse(c.Issuer)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, ErrBinding
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(c.AllowLoopback && u.Scheme == "http" && ip != nil && ip.IsLoopback()) {
		return nil, ErrBinding
	}
	if c.Pool == nil || c.Subjects == nil || c.Terms == nil || c.Consents == nil || c.Signer == nil || len(c.Adapters) == 0 || len(c.Presenters) == 0 {
		return nil, errors.New("authorization dependencies required")
	}
	c.Issuer = strings.TrimSuffix(c.Issuer, "/")
	c.GrantKeys, err = copyKeys(c.GrantKeys)
	if err != nil {
		return nil, err
	}
	policies := map[string]map[string]*ecdsa.PublicKey{}
	for registry, keys := range c.PolicyKeys {
		if !idPattern.MatchString(registry) {
			return nil, ErrBinding
		}
		policies[registry], err = copyKeys(keys)
		if err != nil {
			return nil, err
		}
	}
	c.PolicyKeys = policies
	adapters := map[string]Adapter{}
	audiences := map[string]bool{}
	peers := map[string]bool{}
	for registry, a := range c.Adapters {
		if !idPattern.MatchString(registry) || !idPattern.MatchString(a.Audience) || a.PeerName == "" || audiences[a.Audience] || peers[a.PeerName] || len(policies[registry]) == 0 {
			return nil, ErrBinding
		}
		adapters[registry] = a
		audiences[a.Audience] = true
		peers[a.PeerName] = true
	}
	c.Adapters = adapters
	presenters := map[string]string{}
	thumbs := map[string]bool{}
	for client, thumb := range c.Presenters {
		if !idPattern.MatchString(client) || !thumbPattern.MatchString(thumb) || thumbs[thumb] {
			return nil, ErrBinding
		}
		presenters[client] = thumb
		thumbs[thumb] = true
	}
	c.Presenters = presenters
	return &Service{c}, nil
}
func (s *Service) VerifyGrant(compact string, nowUnix int64) (Grant, error) {
	var g Grant
	if _, err := verify(compact, GrantType, s.config.GrantKeys, &g); err != nil {
		return g, err
	}
	if g.validate() != nil || g.Issuer != s.config.Issuer {
		return Grant{}, ErrBinding
	}
	if g.IssuedAt > nowUnix || g.ExpiresAt <= nowUnix {
		return Grant{}, ErrExpired
	}
	return g, nil
}
