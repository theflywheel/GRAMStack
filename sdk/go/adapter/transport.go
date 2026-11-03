package adapter

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"time"
)

// ServiceTLS builds mutually authenticated transport. The caller pins the peer
// service DNS identity in addition to trusting the deployment CA.
func ServiceTLS(ca, certificate, key []byte) (*tls.Config, error) {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("service CA required")
	}
	pair, err := tls.X509KeyPair(certificate, key)
	if err != nil {
		return nil, errors.New("invalid service certificate/key")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ClientCAs: roots, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert}, nil
}
func ServiceClient(ca, certificate, key []byte, peer string) (*http.Client, error) {
	if peer == "" {
		return nil, errors.New("peer service identity required")
	}
	config, err := ServiceTLS(ca, certificate, key)
	if err != nil {
		return nil, err
	}
	config.ServerName = peer
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: config}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}
func CheckService(r *http.Request, name string) error {
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 || name == "" || r.TLS.PeerCertificates[0].VerifyHostname(name) != nil {
		return errors.New("untrusted service caller")
	}
	return nil
}
