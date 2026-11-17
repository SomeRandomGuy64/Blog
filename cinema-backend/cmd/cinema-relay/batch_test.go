package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"fazon.nexus/cinema/internal/scrape"
)

func TestBatchesStayUnderLimit(t *testing.T) {
	var rs []scrape.Result
	for i := range 300 {
		rs = append(rs, scrape.Result{Name: fmt.Sprintf("Cinema %03d", i), Error: scrape.ErrString(string(make([]byte, 1000)))})
	}
	one, _ := json.Marshal(rs[0])
	limit := 20 * (len(one) + 1)
	bs := batches(rs, limit)
	total := 0
	for i, b := range bs {
		total += len(b)
		size := 0
		for _, r := range b {
			j, _ := json.Marshal(r)
			size += len(j) + 1
		}
		if size > limit {
			t.Fatalf("batch %d is %d bytes, limit %d", i, size, limit)
		}
	}
	if total != len(rs) || len(bs) != 15 {
		t.Fatalf("got %d batches holding %d results; want 15 holding %d", len(bs), total, len(rs))
	}
	if rs[299].Name != bs[14][len(bs[14])-1].Name {
		t.Fatal("order not kept")
	}
}

func TestOversizedResultGetsOwnBatch(t *testing.T) {
	rs := []scrape.Result{{Name: "a"}, {Name: "big", Error: scrape.ErrString(string(make([]byte, 5000)))}, {Name: "c"}}
	if bs := batches(rs, 100); len(bs) != 3 {
		t.Fatalf("got %d batches, want 3", len(bs))
	}
	if bs := batches(nil, 100); len(bs) != 0 {
		t.Fatalf("empty input gave %d batches", len(bs))
	}
}
