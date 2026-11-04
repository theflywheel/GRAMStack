package authn

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func checkDistinctRetrievalCapacity(t *testing.T, pool *pgxpool.Pool, base, private, token string, consumer *http.Client) {
	t.Helper()
	const slots, overflow = 4, 8 // The isolated gateway config sets four slots.
	type authorized struct{ id, grant string }
	requests := make([]authorized, slots+1)
	for i := range requests {
		id := liveRequestFields(t, base, token, "member-12345", []string{"membership_status"})
		_, browser, values, _ := reviewExistingRequest(t, base, id)
		values.Set("decision", "approve")
		values.Set("confirmation", "yes")
		if response, _ := liveResponse(t, browser, decisionRequest(base, id, values)); response.StatusCode != http.StatusOK {
			t.Fatal("capacity fixture approval failed", response.StatusCode)
		}
		requests[i].id = id
	}
	for i := range requests {
		id := requests[i].id
		awaitRequestState(t, base, token, id, "CONSENTED")
		requests[i].grant = liveGrant(t, base, id, token, "capacity-"+id).Grant
	}

	dir := os.Getenv("GRAMSTACK_TEST_SOURCE_READ_GATE")
	block, release := filepath.Join(dir, "block"), filepath.Join(dir, "release")
	if err := os.WriteFile(block, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	var inFlight sync.WaitGroup
	defer func() {
		_ = os.WriteFile(release, []byte("release"), 0600)
		inFlight.Wait()
		for _, name := range []string{block, release, filepath.Join(dir, "entered")} {
			_ = os.Remove(name)
		}
	}()
	start := sourceReads(t)
	results := make(chan int, slots)
	for _, input := range requests[:slots] {
		inFlight.Go(func() {
			response, err := consumer.Do(dataRequest(private, input.id, input.grant).WithContext(t.Context()))
			if err != nil {
				results <- 0
				return
			}
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
			if err != nil {
				results <- 0
				return
			}
			results <- response.StatusCode
		})
	}
	for deadline := time.Now().Add(4 * time.Second); sourceReads(t) < start+slots && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if sourceReads(t) != start+slots {
		t.Fatal("independent authorized requests did not occupy all four slots")
	}
	extra := requests[slots]
	for range overflow {
		response, body := liveResponse(t, consumer, dataRequest(private, extra.id, extra.grant))
		if response.StatusCode != http.StatusTooManyRequests || response.Header.Get("Retry-After") != "1" ||
			!strings.Contains(body, "CAPACITY_UNAVAILABLE") || strings.Contains(body, "membership_status") {
			t.Fatal("saturated gateway did not return metadata-only backpressure", response.StatusCode)
		}
	}
	var claimed bool
	if err := pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM gateway_retrieval_attempts WHERE request_id=$1)`, extra.id).
		Scan(&claimed); err != nil || claimed || sourceReads(t) != start+slots {
		t.Fatal("saturated request consumed authority or reached the source", err)
	}
	if err := os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	for range slots {
		select {
		case status := <-results:
			if status != http.StatusOK {
				t.Fatal("admitted request did not finish successfully", status)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("admitted request did not finish after releasing the source gate")
		}
	}
	if response, _ := liveResponse(t, consumer, dataRequest(private, extra.id, extra.grant)); response.StatusCode != http.StatusOK ||
		sourceReads(t) != start+slots+1 {
		t.Fatal("same grant could not recover after capacity returned", response.StatusCode)
	}
	t.Logf("PASS: %d independent retrievals occupied the gateway, %d overflow calls returned 429 without claiming, and the same grant recovered with one read", slots, overflow)
}
