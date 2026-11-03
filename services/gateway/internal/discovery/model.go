// Package discovery checks public adapter metadata against operator-approved
// descriptors. Advertised hosts, capabilities or keys never establish authority.
package discovery

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const Version = "0.1.0"
const VersionHeader = "Gramstack-Adapter-Discovery-Version"
const ManifestPath = "/.well-known/gramstack-adapter"
const KeysPath = "/.well-known/gramstack-adapter/jwks.json"

var ErrDescriptor = errors.New("approved adapter descriptor invalid")
var ErrMetadata = errors.New("adapter metadata does not match approved contract")
var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

type Resource struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Fields  []string `json:"fields"`
}
type Manifest struct {
	AdapterID           string     `json:"adapter_id"`
	Registry            string     `json:"registry"`
	ContractVersion     string     `json:"contract_version"`
	Audience            string     `json:"audience"`
	Authentication      string     `json:"authentication"`
	SigningKeyIDs       []string   `json:"signing_key_ids"`
	VerificationMethods []string   `json:"verification_methods"`
	Capabilities        []string   `json:"capabilities"`
	Resources           []Resource `json:"resources"`
}
type PublicJWK struct {
	KeyType   string `json:"kty"`
	Curve     string `json:"crv"`
	X         string `json:"x"`
	Y         string `json:"y"`
	ID        string `json:"kid"`
	Algorithm string `json:"alg"`
	Use       string `json:"use"`
}
type Descriptor struct {
	Version, Origin, AdapterID, Registry, Audience, VerificationMethod string
	Resources                                                          []Resource
	Keys                                                               map[string]*ecdsa.PublicKey
}
type Approved struct{ descriptor Descriptor }

func validIDs(ids []string, max int) bool {
	if len(ids) == 0 || len(ids) > max {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !identifier.MatchString(id) || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func validResources(resources []Resource) bool {
	if len(resources) == 0 || len(resources) > 32 {
		return false
	}
	seen := map[string]bool{}
	for _, r := range resources {
		if !identifier.MatchString(r.Name) || !identifier.MatchString(r.Version) || !validIDs(r.Fields, 128) || seen[r.Name] {
			return false
		}
		seen[r.Name] = true
	}
	return true
}
func Approve(d Descriptor, allowLoopback bool) (*Approved, error) {
	u, err := url.Parse(d.Origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, ErrDescriptor
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(allowLoopback && u.Scheme == "http" && ip != nil && ip.IsLoopback()) {
		return nil, ErrDescriptor
	}
	if d.Version != Version || !identifier.MatchString(d.AdapterID) || !identifier.MatchString(d.Registry) || !identifier.MatchString(d.Audience) || !identifier.MatchString(d.VerificationMethod) || !validResources(d.Resources) || len(d.Keys) == 0 || len(d.Keys) > 16 {
		return nil, ErrDescriptor
	}
	keys := map[string]*ecdsa.PublicKey{}
	for id, key := range d.Keys {
		if !identifier.MatchString(id) || key == nil || key.Curve != elliptic.P256() || key.X == nil || key.Y == nil {
			return nil, ErrDescriptor
		}
		raw, err := x509.MarshalPKIXPublicKey(key)
		if err != nil {
			return nil, ErrDescriptor
		}
		parsed, err := x509.ParsePKIXPublicKey(raw)
		if err != nil {
			return nil, ErrDescriptor
		}
		keys[id] = parsed.(*ecdsa.PublicKey)
	}
	d.Keys = keys
	resources := make([]Resource, 0, len(d.Resources))
	for _, r := range d.Resources {
		r.Fields = slices.Clone(r.Fields)
		resources = append(resources, r)
	}
	d.Resources = resources
	d.Origin = strings.TrimSuffix(d.Origin, "/")
	return &Approved{d}, nil
}
func (a *Approved) Matches(registry, id, audience string) bool {
	return a != nil && a.descriptor.Registry == registry && a.descriptor.AdapterID == id && a.descriptor.Audience == audience
}
func (a *Approved) Allows(resource string, fields []string) bool {
	if a == nil || !validIDs(fields, 128) {
		return false
	}
	for _, r := range a.descriptor.Resources {
		if r.Name == resource {
			for _, f := range fields {
				if !slices.Contains(r.Fields, f) {
					return false
				}
			}
			return true
		}
	}
	return false
}
