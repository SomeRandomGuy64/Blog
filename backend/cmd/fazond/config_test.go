package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"fazon.nexus/backend/internal/mail"
)

func parse(t *testing.T, env map[string]string, args ...string) (*config, error) {
	t.Helper()
	for _, k := range []string{"FAZOND_MAIL", "FAZOND_MAIL_FROM", "FAZOND_RESEND_KEY", "FAZOND_RESEND_KEY_FILE", "FAZOND_SMTP_HOST",
		"FAZOND_SMTP_USER", "FAZOND_SMTP_PASS", "FAZOND_SMTP_PASS_FILE", "FAZOND_COOKIE_SECURE", "FAZOND_LISTEN", "FAZOND_DATA"} {
		t.Setenv(k, "") // isolate from the caller's environment and earlier cases
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	c, finish := configFlags(fs)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c, finish()
}

func TestConfig(t *testing.T) {
	c, err := parse(t, nil)
	if err != nil || c.listen != "127.0.0.1:8091" || c.dataDir != "/var/lib/fazond" || !c.cookieSecure || len(c.origins) != 3 {
		t.Fatalf("defaults %+v %v", c, err)
	}
	if _, ok := c.mailer().(mail.Log); !ok {
		t.Fatal("default mailer is not log")
	}

	keyFile := filepath.Join(t.TempDir(), "resend_key")
	os.WriteFile(keyFile, []byte("re_fake_test_key\n"), 0o600)
	c, err = parse(t, map[string]string{"FAZOND_MAIL": "resend", "FAZOND_RESEND_KEY_FILE": keyFile})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := c.mailer().(mail.Resend)
	if !ok || r.Key != "re_fake_test_key" || r.From != defaultResendFrom {
		t.Fatalf("resend mailer %+v", c.mailer())
	}
	c, _ = parse(t, map[string]string{"FAZOND_MAIL": "resend", "FAZOND_RESEND_KEY": "k", "FAZOND_MAIL_FROM": "Me <me@fazon.nexus>"})
	if c.mailer().(mail.Resend).From != "Me <me@fazon.nexus>" {
		t.Fatal("FAZOND_MAIL_FROM not honoured")
	}

	for name, env := range map[string]map[string]string{
		"resend without key": {"FAZOND_MAIL": "resend", "FAZOND_RESEND_KEY": ""},
		"smtp without host":  {"FAZOND_MAIL": "smtp", "FAZOND_MAIL_FROM": "a@b.c"},
		"smtp without from":  {"FAZOND_MAIL": "smtp", "FAZOND_SMTP_HOST": "smtp.gmail.com"},
		"smtp user w/o pass": {"FAZOND_MAIL": "smtp", "FAZOND_SMTP_HOST": "h", "FAZOND_MAIL_FROM": "a@b.c", "FAZOND_SMTP_USER": "u"},
		"unknown mode":       {"FAZOND_MAIL": "carrier-pigeon"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(t, env); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	c, err = parse(t, map[string]string{"FAZOND_MAIL": "smtp", "FAZOND_SMTP_HOST": "smtp.gmail.com", "FAZOND_MAIL_FROM": "Fazon Nexus <x@gmail.com>",
		"FAZOND_SMTP_USER": "x@gmail.com", "FAZOND_SMTP_PASS": "pw", "FAZOND_COOKIE_SECURE": "false"}, "-listen", ":9999")
	if err != nil || c.smtp.Port != 587 || c.smtp.Pass != "pw" || c.cookieSecure || c.listen != ":9999" {
		t.Fatalf("smtp %+v %v", c, err)
	}
}
