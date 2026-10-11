package mail

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestBuild(t *testing.T) {
	from, _ := mail.ParseAddress("Fazon Nexus <login@fazon.nexus>")
	to, _ := mail.ParseAddress("bob@example.com")
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	link := "https://fazon.nexus/account/verify/#t=" + strings.Repeat("A", 43) + "&next=%2Farticles%2Fx%2F"
	raw, err := Build(from, to, Message{To: "bob@example.com", Subject: "Sign in — ログイン",
		Text: "Hi\n\n" + link + "\n", HTML: `<p><a href="` + link + `">Sign in</a></p>`}, now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ReplaceAll(string(raw), "\r\n", ""), "\n") {
		t.Fatal("bare LF in message")
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	subj, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subj != "Sign in — ログイン" {
		t.Fatalf("subject %q", subj)
	}
	if d, err := msg.Header.Date(); err != nil || !d.Equal(now) {
		t.Fatalf("date %v %v", d, err)
	}
	if id := msg.Header.Get("Message-ID"); !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@fazon.nexus>") {
		t.Fatalf("message-id %q", id)
	}
	if got := msg.Header.Get("From"); got != `"Fazon Nexus" <login@fazon.nexus>` {
		t.Fatalf("from %q", got)
	}
	mt, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if mt != "multipart/alternative" {
		t.Fatalf("content-type %q", mt)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var types []string
	for {
		p, err := mr.NextPart() // decodes quoted-printable
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(p)
		if !strings.Contains(string(body), link) {
			t.Fatalf("%s part lost the link: %q", p.Header.Get("Content-Type"), body)
		}
		types = append(types, p.Header.Get("Content-Type"))
	}
	if strings.Join(types, ",") != "text/plain; charset=utf-8,text/html; charset=utf-8" {
		t.Fatalf("parts %q", types)
	}
}

func TestResend(t *testing.T) {
	var got struct {
		From, Subject, Text, HTML string
		To                        []string
	}
	var auth, ctype string
	status, reply := 200, `{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, ctype = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	rs := Resend{Key: "re_test", From: "Fazon Nexus <login@fazon.nexus>", endpoint: srv.URL}
	m := Message{To: "bob@example.com", Subject: "Sign in", Text: "link", HTML: "<p>link</p>"}

	if err := rs.Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer re_test" || ctype != "application/json" {
		t.Fatalf("headers %q %q", auth, ctype)
	}
	if got.From != rs.From || len(got.To) != 1 || got.To[0] != m.To || got.Subject != m.Subject || got.Text != m.Text || got.HTML != m.HTML {
		t.Fatalf("payload %+v", got)
	}

	status, reply = 422, `{"statusCode":422,"name":"validation_error","message":"The fazon.nexus domain is not verified. `+strings.Repeat("x", 400)+`"}`
	err := rs.Send(context.Background(), m)
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "domain is not verified") ||
		strings.Contains(err.Error(), "statusCode") || len(err.Error()) > 400 {
		t.Fatalf("error %v", err)
	}

	status, reply = 200, `{}`
	if err := rs.Send(context.Background(), m); err == nil {
		t.Fatal("2xx without id accepted")
	}

	// Times out instead of hanging a goroutine forever.
	block := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer slow.Close()
	defer close(block)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := (Resend{Key: "k", From: rs.From, endpoint: slow.URL}).Send(ctx, m); err == nil {
		t.Fatal("no timeout")
	}
}
