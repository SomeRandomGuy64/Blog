package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const resendEndpoint = "https://api.resend.com/emails"

// Resend sends through Resend's HTTP API (https://resend.com/docs/api-reference/emails/send-email).
// Plain net/http: the API is one POST, not worth an SDK dependency.
type Resend struct {
	Key  string
	From string // "Name <addr>" on a domain verified in Resend

	endpoint string // tests point this at an httptest server
}

func (r Resend) Send(ctx context.Context, m Message) error {
	body, err := json.Marshal(struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
		HTML    string   `json:"html,omitempty"`
	}{r.From, []string{m.To}, m.Subject, m.Text, m.HTML})
	if err != nil {
		return err
	}
	url := r.endpoint
	if url == "" {
		url = resendEndpoint
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.Key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "fazond")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var out struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	json.Unmarshal(raw, &out) // best effort: errors are reported from the status either way
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := out.Message
		if msg == "" {
			msg = string(raw)
		}
		if r := []rune(msg); len(r) > 300 {
			msg = string(r[:300]) + "…"
		}
		return fmt.Errorf("resend: %s: %s", resp.Status, msg)
	}
	if out.ID == "" {
		return errors.New("resend: 2xx without an email id")
	}
	slog.Info("resend accepted email", "id", out.ID, "to", m.To)
	return nil
}
