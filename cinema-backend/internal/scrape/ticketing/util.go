package ticketing

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// str returns m[k] if it is a string, else "".
func str(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

// truthy is Python's bool(x) for decoded JSON values.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// obj is a JSON object that remembers its key order (Python dicts iterate in source order).
type obj struct {
	keys []string
	m    map[string]any
}

func (o *obj) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &o.m); err != nil {
		return err
	}
	o.keys = o.keys[:0]
	dec := json.NewDecoder(bytes.NewReader(b))
	if _, err := dec.Token(); err != nil { // '{'
		return err
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		o.keys = append(o.keys, t.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return err
		}
	}
	return nil
}

// headRunes is s[:n] in Python (code points, not bytes).
func headRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// upperFirst upper-cases the first code point (p[:1].upper() + p[1:]).
func upperFirst(p string) string {
	r, size := utf8.DecodeRuneInString(p)
	if size == 0 {
		return p
	}
	return strings.ToUpper(string(r)) + p[size:]
}
