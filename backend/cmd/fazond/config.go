package main

import (
	"errors"
	"flag"
	"fmt"
	netmail "net/mail"
	"os"
	"strconv"
	"strings"

	"fazon.nexus/backend/internal/mail"
)

type config struct {
	listen       string
	dataDir      string
	site         string
	origins      []string
	cookieSecure bool
	adminEmail   string
	adminName    string
	mailMode     string
	mailFrom     string
	smtp         mail.SMTP
	resendKey    string
}

const defaultResendFrom = "Fazon Nexus <login@fazon.nexus>"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return n
	}
	return def
}

func envBool(key string, def bool) bool {
	if b, err := strconv.ParseBool(os.Getenv(key)); err == nil {
		return b
	}
	return def
}

// configFlags registers the shared config on fs; every flag defaults to its FAZOND_* env var.
// Call finish after fs.Parse.
func configFlags(fs *flag.FlagSet) (c *config, finish func() error) {
	c = &config{}
	var origins, passFile, resendKeyFile string
	fs.StringVar(&c.listen, "listen", env("FAZOND_LISTEN", "127.0.0.1:8091"), "listen address ($FAZOND_LISTEN)")
	fs.StringVar(&c.dataDir, "data", env("FAZOND_DATA", "/var/lib/fazond"), "data directory: fazon.db, backups/ ($FAZOND_DATA)")
	fs.StringVar(&c.site, "site", env("FAZOND_SITE", "https://fazon.nexus"), "public site base URL: magic links, and where /comment-pages.json is fetched from ($FAZOND_SITE)")
	fs.StringVar(&origins, "origins", env("FAZOND_ORIGINS", "https://fazon.nexus"),
		"comma-separated allowed origins; host:* = any port, e.g. http://localhost:1313,http://127.0.0.1:* for dev ($FAZOND_ORIGINS)")
	fs.BoolVar(&c.cookieSecure, "cookie-secure", envBool("FAZOND_COOKIE_SECURE", true), "Secure session cookie; false only for http dev ($FAZOND_COOKIE_SECURE)")
	fs.StringVar(&c.adminEmail, "admin-email", env("FAZOND_ADMIN_EMAIL", "m.faizan.rasul@gmail.com"), "admin account seeded at startup, and the author of every article: article likes are its points ($FAZOND_ADMIN_EMAIL)")
	fs.StringVar(&c.adminName, "admin-name", env("FAZOND_ADMIN_NAME", "Faizan"), "admin display name, used only when creating it ($FAZOND_ADMIN_NAME)")
	fs.StringVar(&c.mailMode, "mail", env("FAZOND_MAIL", "log"), "resend, smtp, or log = print magic links to the log ($FAZOND_MAIL)")
	fs.StringVar(&resendKeyFile, "resend-key-file", env("FAZOND_RESEND_KEY_FILE", ""), "file holding the Resend API key ($FAZOND_RESEND_KEY_FILE; or set $FAZOND_RESEND_KEY)")
	fs.StringVar(&c.smtp.Host, "smtp-host", env("FAZOND_SMTP_HOST", ""), "SMTP submission host, e.g. smtp.gmail.com ($FAZOND_SMTP_HOST)")
	fs.IntVar(&c.smtp.Port, "smtp-port", envInt("FAZOND_SMTP_PORT", 587), "SMTP port; STARTTLS is required ($FAZOND_SMTP_PORT)")
	fs.StringVar(&c.smtp.User, "smtp-user", env("FAZOND_SMTP_USER", ""), "SMTP username ($FAZOND_SMTP_USER)")
	fs.StringVar(&passFile, "smtp-pass-file", env("FAZOND_SMTP_PASS_FILE", ""), "file holding the SMTP password ($FAZOND_SMTP_PASS_FILE; or set $FAZOND_SMTP_PASS)")
	fs.StringVar(&c.mailFrom, "mail-from", env("FAZOND_MAIL_FROM", ""),
		`From address, e.g. "Fazon Nexus <you@gmail.com>"; resend defaults to "`+defaultResendFrom+`" ($FAZOND_MAIL_FROM)`)
	finish = func() error {
		c.site = strings.TrimSuffix(c.site, "/")
		for _, o := range strings.Split(origins, ",") {
			if o = strings.TrimSpace(o); o != "" {
				c.origins = append(c.origins, strings.TrimSuffix(o, "/"))
			}
		}
		var err error
		if c.smtp.Pass, err = secret("FAZOND_SMTP_PASS", passFile); err != nil {
			return err
		}
		if c.resendKey, err = secret("FAZOND_RESEND_KEY", resendKeyFile); err != nil {
			return err
		}
		c.smtp.From = c.mailFrom
		switch c.mailMode {
		case "log":
		case "resend":
			if c.resendKey == "" {
				return errors.New("FAZOND_MAIL=resend needs FAZOND_RESEND_KEY or FAZOND_RESEND_KEY_FILE")
			}
			if c.mailFrom == "" {
				c.mailFrom = defaultResendFrom
			}
			if _, err := netmail.ParseAddress(c.mailFrom); err != nil {
				return fmt.Errorf("bad FAZOND_MAIL_FROM %q: %w", c.mailFrom, err)
			}
		case "smtp":
			if _, err := netmail.ParseAddress(c.smtp.From); err != nil {
				return fmt.Errorf("bad FAZOND_MAIL_FROM %q: %w", c.smtp.From, err)
			}
			if c.smtp.Host == "" {
				return errors.New("FAZOND_MAIL=smtp needs FAZOND_SMTP_HOST")
			}
			if c.smtp.User != "" && c.smtp.Pass == "" {
				return errors.New("FAZOND_SMTP_USER is set but no FAZOND_SMTP_PASS / FAZOND_SMTP_PASS_FILE")
			}
		default:
			return fmt.Errorf("FAZOND_MAIL must be resend, smtp or log, not %q", c.mailMode)
		}
		return nil
	}
	return c, finish
}

// secret reads a credential from the named env var, or from file when one is given (trimmed).
func secret(envKey, file string) (string, error) {
	if file == "" {
		return strings.TrimSpace(os.Getenv(envKey)), nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read %s file: %w", strings.TrimPrefix(envKey, "FAZOND_"), err)
	}
	return strings.TrimSpace(string(b)), nil
}

func (c *config) mailer() mail.Mailer {
	switch c.mailMode {
	case "resend":
		return mail.Resend{Key: c.resendKey, From: c.mailFrom}
	case "smtp":
		return c.smtp
	}
	return mail.Log{}
}
