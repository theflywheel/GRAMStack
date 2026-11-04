package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundleTrustBoundary(t *testing.T) {
	for _, input := range []string{
		`[]`, `null`, `{"../key":""}`, `{"/key":""}`, `{"key":1}`, `{"key":null}`, `{"key":"?"}`,
		`{"key":"","key":""}`, `{} {}`, `{"key":""`, strings.Repeat(" ", maxBundle+1),
		`{"key":"` + base64.StdEncoding.EncodeToString(make([]byte, (1<<20)+1)) + `"}`,
	} {
		if _, err := decodeBundle(input); err == nil {
			t.Fatalf("unsafe bundle accepted: %.60s", input)
		}
	}
	many := make(map[string]string)
	for i := 0; i < 33; i++ {
		many[fmt.Sprintf("file%d", i)] = ""
	}
	raw, err := json.Marshal(many)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeBundle(string(raw)); err == nil {
		t.Fatal("file count limit ignored")
	}
	files, err := decodeBundle(`{"config.json":"e30=","key.pem":"c2VjcmV0"}`)
	if err != nil || string(files["config.json"]) != "{}" || string(files["key.pem"]) != "secret" {
		t.Fatal(files, err)
	}
}

func TestPrivateRestartSafeFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runtime")
	files := map[string][]byte{"key.pem": []byte("secret")}
	if err := materialize(dir, files); err != nil {
		t.Fatal(err)
	}
	if err := materialize(dir, files); err != nil {
		t.Fatal("identical restart failed", err)
	}
	info, err := os.Stat(filepath.Join(dir, "key.pem"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	if err := materialize(dir, map[string][]byte{"key.pem": []byte("changed")}); err == nil {
		t.Fatal("silently rotated persistent key")
	}
	if err := os.Chmod(filepath.Join(dir, "key.pem"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := materialize(dir, files); err == nil {
		t.Fatal("publicly readable secret accepted")
	}
}

func TestSymlinksAndDirectoryPermissionsRejected(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := materialize(link, map[string][]byte{}); err == nil {
		t.Fatal("directory symlink accepted")
	}
	if err := os.Symlink(filepath.Join(parent, "outside"), filepath.Join(target, "key")); err != nil {
		t.Fatal(err)
	}
	if err := materialize(target, map[string][]byte{"key": []byte("secret")}); err == nil {
		t.Fatal("file symlink accepted")
	}
	if _, err := os.Stat(filepath.Join(parent, "outside")); !os.IsNotExist(err) {
		t.Fatal("wrote outside private directory")
	}
	if err := os.Chmod(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := materialize(target, map[string][]byte{}); err == nil {
		t.Fatal("unsafe directory accepted")
	}
}
