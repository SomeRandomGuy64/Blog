package scrape

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// DefaultRequestTimeout matches the Python session's per-request timeout.
const DefaultRequestTimeout = 60 * time.Second

// Many listing sites sit behind Cloudflare/Akamai bot rules that fingerprint TLS and HTTP/2,
// so every request goes out as Chrome 146 (same as curl_cffi impersonate="chrome") with
// Chrome's default navigation headers, in Chrome's order.
var chromeHeaders = [][2]string{
	{"sec-ch-ua", `"Chromium";v="146", "Not-A.Brand";v="24", "Google Chrome";v="146"`},
	{"sec-ch-ua-mobile", "?0"},
	{"sec-ch-ua-platform", `"macOS"`},
	{"upgrade-insecure-requests", "1"},
	{"user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"},
	{"accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
	{"sec-fetch-site", "none"},
	{"sec-fetch-mode", "navigate"},
	{"sec-fetch-user", "?1"},
	{"sec-fetch-dest", "document"},
	{"accept-encoding", "gzip, deflate, br, zstd"},
	{"accept-language", "en-US,en;q=0.9"},
	{"priority", "u=0, i"},
}

// Session is one cinema's scrape: its own HTTP client + cookie jar, plus a memo shared by
// every cinema in the same run (e.g. Picturehouse's 5 MB runtimes feed is fetched once).
type Session struct {
	ctx  context.Context
	hc   tlsclient.HttpClient
	memo *Memo
}

// NewSession builds an impersonating client. memo may be nil.
func NewSession(ctx context.Context, memo *Memo) (*Session, error) {
	opts := []tlsclient.HttpClientOption{
		tlsclient.WithClientProfile(profiles.Chrome_146),
		tlsclient.WithCookieJar(tlsclient.NewCookieJar()),
		tlsclient.WithTimeoutSeconds(180),
		tlsclient.WithRandomTLSExtensionOrder(), // Chrome shuffles extensions since v110
	}
	// tls-client ignores the proxy environment variables, so honour HTTPS_PROXY explicitly.
	if p := cmp.Or(os.Getenv("HTTPS_PROXY"), os.Getenv("https_proxy")); p != "" {
		opts = append(opts, tlsclient.WithProxyUrl(p))
	}
	hc, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), opts...)
	if err != nil {
		return nil, err
	}
	if memo == nil {
		memo = NewMemo()
	}
	return &Session{ctx: ctx, hc: hc, memo: memo}, nil
}

// Context is the cinema's context (cancelled on its timeout).
func (s *Session) Context() context.Context { return s.ctx }

// Close releases idle connections.
func (s *Session) Close() { s.hc.CloseIdleConnections() }

// Memo returns the run-wide value for key, computing it once with fn (errors are cached too).
func (s *Session) Memo(key string, fn func() (any, error)) (any, error) { return s.memo.Get(key, fn) }

// Opt configures one request.
type Opt func(*reqOpts)

type reqOpts struct {
	query   url.Values
	headers [][2]string
	body    []byte
	ctype   string
	timeout time.Duration
}

// Query appends URL query parameters.
func Query(v url.Values) Opt { return func(o *reqOpts) { o.query = v } }

// Header sets (or overrides) one request header.
func Header(k, v string) Opt {
	return func(o *reqOpts) { o.headers = append(o.headers, [2]string{strings.ToLower(k), v}) }
}

// Form sends an application/x-www-form-urlencoded body.
func Form(v url.Values) Opt {
	return func(o *reqOpts) { o.body, o.ctype = []byte(v.Encode()), "application/x-www-form-urlencoded" }
}

// JSONBody sends v as compact JSON.
func JSONBody(v any) Opt {
	return func(o *reqOpts) {
		b, _ := json.Marshal(v)
		o.body, o.ctype = b, "application/json"
	}
}

// Timeout overrides the per-request timeout.
func Timeout(d time.Duration) Opt { return func(o *reqOpts) { o.timeout = d } }

// Resp is a fully-read response.
type Resp struct {
	Status int
	URL    string
	Header http.Header
	Body   []byte
}

func (r *Resp) Text() string { return string(r.Body) }

// OK returns an error for a non-2xx status (Python's raise_for_status).
func (r *Resp) OK() error {
	if r.Status < 200 || r.Status > 299 {
		return &HTTPError{Status: r.Status, URL: r.URL}
	}
	return nil
}

// JSON decodes the body into v.
func (r *Resp) JSON(v any) error {
	if err := json.Unmarshal(r.Body, v); err != nil {
		return fmt.Errorf("decode %s: %w", r.URL, err)
	}
	return nil
}

// HTTPError is a non-2xx response.
type HTTPError struct {
	Status int
	URL    string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d for %s", e.Status, e.URL) }

// Do performs a request without checking the status.
func (s *Session) Do(method, rawURL string, opts ...Opt) (*Resp, error) {
	o := reqOpts{timeout: DefaultRequestTimeout}
	for _, f := range opts {
		f(&o)
	}
	if len(o.query) > 0 {
		sep := "?"
		if strings.Contains(rawURL, "?") {
			sep = "&"
		}
		rawURL += sep + o.query.Encode()
	}
	ctx, cancel := context.WithTimeout(s.ctx, o.timeout)
	defer cancel()
	var body io.Reader
	if o.body != nil {
		body = bytes.NewReader(o.body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	hdr, order := http.Header{}, []string{}
	set := func(k, v string) {
		if _, ok := hdr[k]; !ok {
			order = append(order, k)
		}
		hdr[k] = []string{v}
	}
	for _, kv := range chromeHeaders {
		set(kv[0], kv[1])
	}
	if o.ctype != "" {
		set("content-type", o.ctype)
	}
	for _, kv := range o.headers {
		set(kv[0], kv[1])
	}
	hdr[http.HeaderOrderKey] = order
	req.Header = hdr
	res, err := s.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, err)
	}
	return &Resp{Status: res.StatusCode, URL: rawURL, Header: res.Header, Body: b}, nil
}

// Get performs a GET without checking the status.
func (s *Session) Get(u string, opts ...Opt) (*Resp, error) { return s.Do("GET", u, opts...) }

// Post performs a POST without checking the status.
func (s *Session) Post(u string, opts ...Opt) (*Resp, error) { return s.Do("POST", u, opts...) }

// GetOK is Get + raise_for_status.
func (s *Session) GetOK(u string, opts ...Opt) (*Resp, error) { return ok(s.Do("GET", u, opts...)) }

// PostOK is Post + raise_for_status.
func (s *Session) PostOK(u string, opts ...Opt) (*Resp, error) { return ok(s.Do("POST", u, opts...)) }

// GetJSON GETs, checks the status and decodes into v.
func (s *Session) GetJSON(u string, v any, opts ...Opt) error {
	r, err := s.GetOK(u, opts...)
	if err != nil {
		return err
	}
	return r.JSON(v)
}

// PostJSON POSTs, checks the status and decodes into v.
func (s *Session) PostJSON(u string, v any, opts ...Opt) error {
	r, err := s.PostOK(u, opts...)
	if err != nil {
		return err
	}
	return r.JSON(v)
}

func ok(r *Resp, err error) (*Resp, error) {
	if err != nil {
		return nil, err
	}
	return r, r.OK()
}

// Memo is a run-wide compute-once cache.
type Memo struct {
	mu sync.Mutex
	m  map[string]*memoEntry
}

type memoEntry struct {
	once sync.Once
	v    any
	err  error
}

func NewMemo() *Memo { return &Memo{m: map[string]*memoEntry{}} }

func (m *Memo) Get(key string, fn func() (any, error)) (any, error) {
	m.mu.Lock()
	e := m.m[key]
	if e == nil {
		e = &memoEntry{}
		m.m[key] = e
	}
	m.mu.Unlock()
	e.once.Do(func() { e.v, e.err = fn() })
	return e.v, e.err
}
