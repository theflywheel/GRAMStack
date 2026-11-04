// Runtimefiles materializes Railway secret files before replacing itself with
// the service process. Secrets are never part of an image or command argument.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
)

const maxBundle = 2 << 20

var fileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func decodeBundle(raw string) (map[string][]byte, error) {
	bad := errors.New("invalid runtime file bundle")
	if len(raw) > maxBundle {
		return nil, bad
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, bad
	}
	files := make(map[string][]byte)
	total := 0
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, bad
		}
		name, ok := token.(string)
		if !ok || !fileName.MatchString(name) || len(files) >= 32 {
			return nil, bad
		}
		if _, exists := files[name]; exists {
			return nil, bad
		}
		var encoded *string
		if err := decoder.Decode(&encoded); err != nil || encoded == nil {
			return nil, bad
		}
		value, err := base64.StdEncoding.Strict().DecodeString(*encoded)
		total += len(value)
		if err != nil || total > 1<<20 {
			return nil, bad
		}
		files[name] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, bad
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, bad
	}
	return files, nil
}

func materialize(dir string, files map[string][]byte) error {
	bad := errors.New("runtime files unavailable or changed")
	if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return bad
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return bad
	}
	for name, data := range files {
		path := filepath.Join(dir, name)
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
				return bad
			}
			existing, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(existing, data) {
				return bad
			}
			continue
		}
		if err != nil {
			return bad
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return bad
		}
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("service executable required")
	}
	raw := os.Getenv("GRAMSTACK_FILES_JSON")
	os.Unsetenv("GRAMSTACK_FILES_JSON")
	if raw != "" {
		files, err := decodeBundle(raw)
		if err != nil {
			log.Fatal(err)
		}
		if err = materialize("/run/gramstack", files); err != nil {
			log.Fatal(err)
		}
	}
	binary, err := exec.LookPath(os.Args[1])
	if err != nil {
		log.Fatal("service executable unavailable")
	}
	if err = syscall.Exec(binary, os.Args[1:], os.Environ()); err != nil {
		log.Fatal("service execution failed")
	}
}
