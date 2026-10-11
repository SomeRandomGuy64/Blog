// Package mail sends the magic-link emails: over SMTP with STARTTLS (port 587 — Hetzner blocks
// outbound 25 and 465), or into the log for development.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Message is one email with a plain-text body and an optional HTML alternative.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Mailer delivers a Message.
type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// Log writes messages to the log instead of sending them (FAZOND_MAIL=log).
type Log struct{}

func (Log) Send(_ context.Context, m Message) error {
	slog.Info("mail (not sent: FAZOND_MAIL=log)", "to", m.To, "subject", m.Subject, "text", m.Text)
	return nil
}

// SMTP sends through a submission server that supports STARTTLS, e.g. smtp.gmail.com:587
// with an app password.
type SMTP struct {
	Host string
	Port int
	User string
	Pass string
	From string // "Name <addr>" or "addr"
}

func (s SMTP) Send(ctx context.Context, m Message) error {
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return fmt.Errorf("bad From %q: %w", s.From, err)
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return fmt.Errorf("bad To %q: %w", m.To, err)
	}
	raw, err := Build(from, to, m, time.Now())
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(s.Host, fmt.Sprint(s.Port))
	conn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	// net/smtp has no context support: bound the whole conversation with a deadline instead.
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return errors.New("smtp server does not offer STARTTLS")
	}
	if err := c.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
		return fmt.Errorf("starttls: %w", err)
	}
	if s.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.User, s.Pass, s.Host)); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(to.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// Build renders an RFC 5322 message: multipart/alternative when m.HTML is set, otherwise a
// single text/plain part. Bodies are quoted-printable UTF-8 with CRLF line endings.
func Build(from, to *mail.Address, m Message, now time.Time) ([]byte, error) {
	var b bytes.Buffer
	hdr := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	domain := "localhost"
	if _, d, ok := strings.Cut(from.Address, "@"); ok {
		domain = d
	}
	id := make([]byte, 16)
	rand.Read(id)
	hdr("From", from.String())
	hdr("To", to.String())
	hdr("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	hdr("Date", now.Format(time.RFC1123Z))
	hdr("Message-ID", "<"+hex.EncodeToString(id)+"@"+domain+">")
	hdr("MIME-Version", "1.0")
	hdr("Auto-Submitted", "auto-generated")
	if m.HTML == "" {
		hdr("Content-Type", "text/plain; charset=utf-8")
		hdr("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		if err := writeQP(&b, m.Text); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	boundary := "fz-" + hex.EncodeToString(id[:8])
	hdr("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	for _, p := range []struct{ ctype, body string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		b.WriteString("--" + boundary + "\r\n")
		hdr("Content-Type", p.ctype+"; charset=utf-8")
		hdr("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		if err := writeQP(&b, p.body); err != nil {
			return nil, err
		}
		b.WriteString("\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes(), nil
}

func writeQP(b *bytes.Buffer, s string) error {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	w := quotedprintable.NewWriter(b)
	if _, err := w.Write([]byte(s)); err != nil {
		return err
	}
	return w.Close()
}
