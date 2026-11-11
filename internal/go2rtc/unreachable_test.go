package go2rtc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// With no answer at all, go2rtc is unreachable since the first failure; the
// following ones only change the message. Any answer, even a 500, clears the
// state: whoever answered is up.
func TestUnreachableSinceFirstFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // closed port: Do fails without an answer

	c := New(config.Go2RTC{URL: url})
	ctx := context.Background()

	_, _ = c.Version(ctx)
	since, errText := c.Unreachable()
	if since.IsZero() || errText == "" {
		t.Fatalf("transport failure wasn't recorded: since %v, error %q", since, errText)
	}
	time.Sleep(time.Millisecond)
	_, _ = c.Version(ctx)
	if d, _ := c.Unreachable(); !d.Equal(since) {
		t.Errorf("the second failure moved since: %v -> %v", since, d)
	}

	// go2rtc comes back, answering an HTTP error: it's still an answer.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv2.Close()
	c.BaseURL = srv2.URL
	_, _ = c.Version(ctx)
	if d, e := c.Unreachable(); !d.IsZero() || e != "" {
		t.Errorf("answered 500 and stayed unreachable: since %v, error %q", d, e)
	}
}

// dwnvr giving up on the request (recorder stopping, screen closed) says
// nothing about go2rtc.
func TestUnreachableIgnoresCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := New(config.Go2RTC{URL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, err := c.Version(ctx); err == nil {
		t.Fatal("the canceled request should fail")
	}
	if d, e := c.Unreachable(); !d.IsZero() {
		t.Errorf("cancellation marked it unreachable: since %v, error %q", d, e)
	}
}
