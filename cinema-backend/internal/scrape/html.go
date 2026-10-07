package scrape

import (
	"bytes"
	"mime"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Doc parses an HTML body.
func Doc(body []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(bytes.NewReader(body))
}

// Doc parses the response as HTML, honouring a Latin-1 Content-Type charset the way Python
// requests does (byte = code point, so e.g. \x96 stays U+0096); anything else is read as UTF-8.
func (r *Resp) Doc() (*goquery.Document, error) {
	body := r.Body
	_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch strings.ToLower(params["charset"]) {
	case "iso-8859-1", "latin-1", "latin1", "iso8859-1":
		rs := make([]rune, len(body))
		for i, b := range body {
			rs[i] = rune(b)
		}
		body = []byte(string(rs))
	}
	return Doc(body)
}

// Text mimics BeautifulSoup's get_text(sep, strip=strip): the selection's text nodes
// (excluding <script>/<style>/<template> and comments) joined by sep; with strip, each node is
// trimmed and empty ones dropped.
func Text(sel *goquery.Selection, sep string, strip bool) string {
	var parts []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			t := n.Data
			if strip {
				t = strings.TrimSpace(t)
				if t == "" {
					return
				}
			}
			parts = append(parts, t)
			return
		case html.ElementNode:
			switch n.Data {
			case "script", "style", "template":
				return
			}
		case html.CommentNode:
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range sel.Nodes {
		walk(n)
	}
	return strings.Join(parts, sep)
}

// TextS is get_text(" ", strip=True), the common case.
func TextS(sel *goquery.Selection) string { return Text(sel, " ", true) }

// Attr returns an attribute or "".
func Attr(sel *goquery.Selection, name string) string {
	v, _ := sel.Attr(name)
	return v
}

// Squash collapses whitespace runs to single spaces and trims (re.sub(r"\s+", " ", t).strip()).
func Squash(s string) string { return strings.Join(strings.Fields(s), " ") }
