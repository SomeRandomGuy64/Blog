package feedback

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestQuotaAndLimits(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 18, 23, 59, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }

	if _, _, err := s.Add("", "  \n\t "); !errors.Is(err, ErrEmpty) {
		t.Errorf("blank: %v", err)
	}
	if _, _, err := s.Add("", strings.Repeat("あ", MaxChars+1)); !errors.Is(err, ErrTooLong) {
		t.Errorf("1001 chars: %v", err)
	}
	if _, _, err := s.Add(strings.Repeat("x", MaxName+1), "hi"); !errors.Is(err, ErrName) {
		t.Errorf("long name: %v", err)
	}
	if _, _, err := s.Add("", strings.Repeat("あ", MaxChars)); err != nil {
		t.Errorf("exactly 1000 multibyte chars: %v", err)
	}
	for i := 1; i < Limit; i++ {
		if _, _, err := s.Add("Ann\x00", "line one\r\nline two\x07"); err != nil {
			t.Fatal(err)
		}
	}
	_, st, err := s.Add("", "one too many")
	if !errors.Is(err, ErrQuota) || st.Remaining != 0 || st.Used != Limit {
		t.Fatalf("6th post: %v %+v", err, st)
	}
	if want := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC); !st.ResetsAt.Equal(want) {
		t.Errorf("resets_at %v, want %v", st.ResetsAt, want)
	}
	if it := st.Items[0]; it.Name != "Ann" || it.Text != "line one\nline two" {
		t.Errorf("cleaning: %+v", it)
	}

	// Deleting doesn't give the slot back.
	if ok, err := s.Delete(st.Items[0].ID); !ok || err != nil {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if _, _, err := s.Add("", "still full"); !errors.Is(err, ErrQuota) {
		t.Errorf("after delete: %v", err)
	}

	// Persists across reopen, and the quota resets at 00:00 UTC.
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	s2.Now = func() time.Time { return now }
	st = s2.Status()
	if len(st.Items) != Limit-1 || st.Used != 0 || st.Remaining != Limit {
		t.Fatalf("next day: %+v", st)
	}
	if _, st, err = s2.Add("", "new day"); err != nil || st.Items[0].Text != "new day" || st.Remaining != Limit-1 {
		t.Fatalf("next day post: %v %+v", err, st)
	}
}

func TestReplies(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	it, _, err := s.Add("", "a question")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.AddReply("nope", "Owner", "hi"); ok || err != nil {
		t.Errorf("reply to missing comment: %v %v", ok, err)
	}
	if _, _, err := s.AddReply(it.ID, "Owner", " \n "); !errors.Is(err, ErrEmpty) {
		t.Errorf("blank reply: %v", err)
	}
	if _, _, err := s.AddReply(it.ID, "Owner", strings.Repeat("あ", MaxReply+1)); !errors.Is(err, ErrReply) {
		t.Errorf("long reply: %v", err)
	}
	r1, ok, err := s.AddReply(it.ID, "Owner", "first\r\nanswer")
	if !ok || err != nil {
		t.Fatal(ok, err)
	}
	r2, _, _ := s.AddReply(it.ID, "Owner", "second")
	st := s.Status()
	if st.Used != 1 {
		t.Errorf("replies used the quota: %+v", st)
	}
	if got := st.Items[0].Replies; len(got) != 2 || got[0].Text != "first\nanswer" || got[1].ID != r2.ID {
		t.Fatalf("replies: %+v", got)
	}
	if ok, err := s.DeleteReply(it.ID, r1.ID); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, _ := s.DeleteReply(it.ID, r1.ID); ok {
		t.Error("deleted twice")
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Status().Items[0].Replies; len(got) != 1 || got[0].ID != r2.ID {
		t.Errorf("after reopen: %+v", got)
	}
}
