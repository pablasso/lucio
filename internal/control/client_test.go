package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pablasso/lucio/internal/delivery"
)

func TestOriginValidation(t *testing.T) {
	for _, u := range []string{"", "localhost:80", "ftp://example.test", "http://u:p@example.test", "http://example.test/api", "http://example.test?q=1", "http://example.test#x"} {
		if _, err := NewClient(u); err == nil {
			t.Fatal(u)
		}
	}
	if _, err := NewClient(DefaultURL); err != nil {
		t.Fatal(err)
	}
}
func TestClientUpdateIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Lucio-Client") != "1" {
			t.Error("missing IPC header")
		}
		if r.Method == "POST" {
			if r.URL.Query().Get("update") != "full" {
				t.Error("policy")
			}
			json.NewEncoder(w).Encode(Update{ID: "instance-connection-packet", State: UpdatePending, Sent: SentImage{PacketID: 1, ConnectionID: 2}})
			return
		}
		json.NewEncoder(w).Encode(Update{ID: "unrelated", State: UpdateACKed})
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL)
	u, err := c.Submit(context.Background(), "hello.png", []byte{1}, delivery.PolicyFull)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Wait(context.Background(), u.ID); err == nil {
		t.Fatal("accepted unrelated update")
	}
}
func TestClientErrorAndCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		json.NewEncoder(w).Encode(ErrorResponse{Code: "busy", Error: "one update at a time"})
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL)
	_, err := c.Submit(context.Background(), "test", nil, delivery.PolicyFull)
	var e *HTTPError
	if !errors.As(err, &e) || e.Code != "busy" || e.Status != 409 {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond)
	if _, err := c.Health(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
