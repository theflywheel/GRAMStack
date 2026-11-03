// Devcerts writes an isolated demonstration CA and service identities. It does
// not install trust roots or contact a production certificate authority.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func main() {
	dir := flag.String("dir", "", "private output directory")
	flag.Parse()
	if *dir == "" {
		log.Fatal("-dir required")
	}
	if err := os.MkdirAll(*dir, 0700); err != nil {
		log.Fatal(err)
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "GRAMStack isolated demo CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		log.Fatal(err)
	}
	write(*dir, "ca.pem", "CERTIFICATE", der)
	for i, name := range []string{"gateway", "lokos-adapter", "mock-lokos", "unauthorised", "consumer-one"} {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			log.Fatal(err)
		}
		cert := &x509.Certificate{SerialNumber: big.NewInt(int64(i + 2)), Subject: pkix.Name{CommonName: name + ".gramstack.internal"}, DNSNames: []string{name + ".gramstack.internal"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
		der, err := x509.CreateCertificate(rand.Reader, cert, ca, &key.PublicKey, caKey)
		if err != nil {
			log.Fatal(err)
		}
		write(*dir, name+".pem", "CERTIFICATE", der)
		raw, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			log.Fatal(err)
		}
		write(*dir, name+".key", "EC PRIVATE KEY", raw)
	}
	signing, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal("consent key generation failed")
	}
	signingDER, err := x509.MarshalPKCS8PrivateKey(signing)
	if err != nil {
		log.Fatal("consent key encoding failed")
	}
	write(*dir, "consent-signing.key", "PRIVATE KEY", signingDER)
	for _, name := range []string{"grant-signing", "policy-signing", "adapter-evidence"} {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			log.Fatal("signing key generation failed")
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			log.Fatal("signing key encoding failed")
		}
		write(*dir, name+".key", "PRIVATE KEY", der)
		pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			log.Fatal("public key encoding failed")
		}
		write(*dir, name+".pub", "PUBLIC KEY", pub)
	}

}
func write(dir, name, kind string, der []byte) {
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatal("certificate output already exists or is unavailable")
	}
	_, err = file.Write(pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}))
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		log.Fatal("certificate output failed")
	}

}
