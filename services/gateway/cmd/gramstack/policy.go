package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"

	"github.com/gowebpki/jcs"
	az "github.com/theflywheel/GRAMStack/services/gateway/internal/authorization"
)

// signPolicy is an offline operator utility. Runtime trusts only the configured
// source public keys; the registry's signing key is never needed by the gateway.
func signPolicy(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("sign-policy", flag.ContinueOnError)
	flags.SetOutput(out)
	input := flags.String("input", "", "registry-approved policy JSON")
	keyPath := flags.String("key", "", "source-controlled P-256 private key PEM")
	kid := flags.String("key-id", "", "approved source key ID")
	output := flags.String("output", "", "new signed policy output file")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *input == "" || *output == "" || *keyPath == "" || *kid == "" {
		return errors.New("input, key, key-id and output required")
	}
	raw, err := readBounded(*input, 128*1024)
	if err != nil {
		return err
	}
	canonical, err := jcs.Transform(raw)
	if err != nil {
		return errors.New("invalid policy JSON")
	}
	var policy az.Policy
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&policy) != nil || decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid policy fields")
	}
	key, err := signingKey(*keyPath)
	if err != nil {
		return err
	}
	signed, err := az.SignPolicy(ctx, *kid, key, policy)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("policy output already exists or is unavailable")
	}
	_, err = io.WriteString(file, signed+"\n")
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
