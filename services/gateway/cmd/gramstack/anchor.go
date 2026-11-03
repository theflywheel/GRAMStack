package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"

	"github.com/theflywheel/GRAMStack/services/gateway/internal/evidence/dedi"
)

// anchor explicitly provisions or publishes through the same pinned client as
// the durable evidence worker.
func anchor(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("anchor", flag.ContinueOnError)
	event := flags.String("event-id", "", "opaque evidence event ID")
	digest := flags.String("digest", "", "protected SHA-256 evidence commitment")
	provision := flags.Bool("provision", false, "explicitly create the configured namespace and registry if absent")
	loopback := flags.Bool("development-loopback", false, "allow literal loopback HTTP for an isolated test node")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected anchor arguments")
	}
	if *event == "" || *digest == "" {
		return errors.New("anchor requires -event-id and -digest")
	}
	client, err := configuredDeDiClient(*loopback)
	if err != nil {
		return err
	}
	if *provision {
		if err := client.Provision(ctx); err != nil {
			return err
		}
	}
	proof, err := client.Publish(ctx, *event, *digest)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(proof)
}

func configuredDeDiClient(loopback bool) (*dedi.Client, error) {
	raw, err := readBounded(os.Getenv("DEDI_PUBLISHER_KEY_FILE"), 4096)
	if err != nil {
		return nil, errors.New("cannot read DeDi publisher key file")
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("invalid DeDi publisher key encoding")
	}
	return dedi.New(dedi.Config{
		BaseURL: os.Getenv("DEDI_NODE_URL"), Namespace: os.Getenv("DEDI_NAMESPACE"), Registry: os.Getenv("DEDI_REGISTRY"),
		PublisherKeyID: os.Getenv("DEDI_PUBLISHER_KEY_ID"), PublisherKey: ed25519.PrivateKey(key),
		AdminUser: os.Getenv("DEDI_ADMIN_USER"), AdminPassword: os.Getenv("DEDI_ADMIN_PASSWORD"),
		CheckpointOrigin: os.Getenv("DEDI_CHECKPOINT_ORIGIN"), CheckpointVerifier: os.Getenv("DEDI_CHECKPOINT_VERIFIER"), AllowInsecureLoopback: loopback,
	})
}
