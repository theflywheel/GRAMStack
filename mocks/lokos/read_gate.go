package lokosmock

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// readGate is operator-only fixture instrumentation, never an HTTP control API.
func (s *Source) readGate(ctx context.Context) error {
	dir := s.config.ReadGateDirectory
	if dir == "" {
		return nil
	}
	file, err := os.OpenFile(filepath.Join(dir, "reads.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString("read\n")
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	present := func(name string) (bool, error) {
		_, err := os.Stat(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return err == nil, err
	}
	offline, err := present("offline")
	if err != nil {
		return err
	}
	if offline {
		return errors.New("fixture source offline")
	}
	blocked, err := present("block")
	if err != nil || !blocked {
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "entered"), []byte("entered"), 0600); err != nil {
		return err
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		released, err := present("release")
		if err != nil {
			return err
		}
		if released {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (s *Source) disconnect(w http.ResponseWriter) bool {
	if s.config.ReadGateDirectory == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(s.config.ReadGateDirectory, "disconnect")); err != nil {
		return false
	}
	if h, ok := w.(http.Hijacker); ok {
		conn, _, err := h.Hijack()
		if err == nil {
			conn.Close()
			return true
		}
	}
	http.Error(w, "Fictional source unavailable", 503)
	return true
}
