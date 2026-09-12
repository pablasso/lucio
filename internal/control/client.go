// Package control implements private, same-version daemon IPC. It is not a
// public REST API or Go SDK. CLI commands are the supported operator interface.
package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pablasso/lucio/internal/delivery"
	"github.com/pablasso/lucio/internal/telemetry"
)

const DefaultAddress = "127.0.0.1:18080"
const DefaultURL = "http://" + DefaultAddress

const (
	UpdateSending = "sending"
	UpdatePending = "pending"
	UpdateACKed   = "acked"
	UpdateFailed  = "failed"
)

type Snapshot struct {
	OK           bool                    `json:"ok"`
	RunID        string                  `json:"run_id"`
	Connected    bool                    `json:"connected"`
	HasDevice    bool                    `json:"has_device"`
	ImageLoaded  bool                    `json:"image_loaded"`
	NextPacketID uint32                  `json:"next_packet_id"`
	Remote       string                  `json:"remote,omitempty"`
	ConnectedAt  time.Time               `json:"connected_at,omitempty"`
	ConnectionID uint64                  `json:"connection_id,omitempty"`
	DeviceID     string                  `json:"device_id,omitempty"`
	LastStatus   *Status                 `json:"last_status"`
	LastSent     *SentImage              `json:"last_sent"`
	LastAck      *ImageACK               `json:"last_ack"`
	DisplayState DisplayState            `json:"display_state"`
	Telemetry    telemetry.RuntimeStatus `json:"telemetry"`
	Image        *Image                  `json:"image,omitempty"`
}

type Update struct {
	ID    string    `json:"id"`
	State string    `json:"state"`
	Sent  SentImage `json:"sent"`
	ACK   *ImageACK `json:"ack,omitempty"`
	Error string    `json:"error,omitempty"`
}

func (u Update) Terminal() bool { return u.State == UpdateACKed || u.State == UpdateFailed }

// HTTPError preserves a daemon error code so clients can distinguish busy,
// disconnected, invalid input, and expired results without parsing prose.
type HTTPError struct {
	Status  int
	Code    string
	Message string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("daemon: %s: %s (HTTP %d)", e.Code, e.Message, e.Status)
}

type ErrorResponse struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

type Client struct {
	base *url.URL
	http *http.Client
}

func NewClient(raw string) (*Client, error) {
	u, err := ParseURL(raw)
	if err != nil {
		return nil, err
	}
	return &Client{base: u, http: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func ParseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("server must be an http(s) origin without credentials, path, query, or fragment")
	}
	return u, nil
}
func (c *Client) request(ctx context.Context, method, path string, query url.Values, body []byte, out any) error {
	u := *c.base
	u.Path = path
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "lucio")
	// This non-simple header prevents cross-origin browser form/image requests.
	req.Header.Set("X-Lucio-Client", "1")
	if body != nil {
		req.Header.Set("Content-Type", "image/png")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("contact daemon: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("daemon response exceeds 1 MiB")
	}
	if resp.StatusCode != http.StatusOK {
		var e ErrorResponse
		if json.Unmarshal(data, &e) != nil {
			e.Error = strings.TrimSpace(string(data))
		}
		return &HTTPError{Status: resp.StatusCode, Code: e.Code, Message: e.Error}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode daemon response: %w", err)
	}
	return nil
}
func (c *Client) Health(ctx context.Context) (*Snapshot, error) {
	var s Snapshot
	err := c.request(ctx, "GET", "/healthz", nil, nil, &s)
	if err != nil {
		return nil, err
	}
	if !s.OK || s.RunID == "" {
		return nil, fmt.Errorf("daemon response is missing instance metadata; use matching client/server versions")
	}
	return &s, nil
}
func (c *Client) Submit(ctx context.Context, name string, png []byte, policy delivery.Policy) (Update, error) {
	if _, err := delivery.ParsePolicy(string(policy)); err != nil {
		return Update{}, err
	}
	var u Update
	err := c.request(ctx, "POST", "/api/image", url.Values{"name": {name}, "update": {string(policy)}}, png, &u)
	if err == nil && (u.ID == "" || u.Sent.PacketID == 0) {
		err = fmt.Errorf("daemon response is missing update identity")
	}
	return u, err
}
func (c *Client) Wait(ctx context.Context, id string) (Update, error) {
	if id == "" || strings.ContainsAny(id, "/\\?#") {
		return Update{}, fmt.Errorf("invalid update ID")
	}
	var u Update
	err := c.request(ctx, "GET", "/api/updates/"+id, url.Values{"wait": {"1"}}, nil, &u)
	if err != nil {
		return u, err
	}
	if u.ID != id || !u.Terminal() {
		return u, fmt.Errorf("daemon returned an unrelated or incomplete update")
	}
	if u.State != UpdateACKed {
		return u, fmt.Errorf("update %s failed: %s", id, u.Error)
	}
	if u.ACK == nil || !u.ACK.MatchedLast || u.ACK.ConnectionID != u.Sent.ConnectionID || u.ACK.PacketID != u.Sent.PacketID {
		return u, fmt.Errorf("daemon returned an invalid acknowledgment")
	}
	return u, nil
}
