package api

import (
	"fmt"
	"net/http/httptest"
	"slices"
	"testing"
)

// addWeekSub posts to a week's sub-categories (replace 0 = a plain add).
func (c *client) addWeekSub(key, cat, name string, replace int64) *httptest.ResponseRecorder {
	body := map[string]any{"category": cat, "name": name}
	if replace != 0 {
		body["replace"] = replace
	}
	return c.do("POST", "/v1/track/weeks/"+key+"/subcats", body)
}

// mustAddWeekSub adds name to a week and returns the sub-category's id.
func (c *client) mustAddWeekSub(key, cat, name string, replace int64) int64 {
	c.e.t.Helper()
	body := map[string]any{"category": cat, "name": name}
	if replace != 0 {
		body["replace"] = replace
	}
	return int64(c.mustStatus(200, "POST", "/v1/track/weeks/"+key+"/subcats", body)["id"].(float64))
}

// weekSubs is a week's "subcats" (the sub-categories added to it).
func (c *client) weekSubs(key string) []int64 {
	c.e.t.Helper()
	raw, ok := c.mustStatus(200, "GET", "/v1/track/weeks/"+key, nil)["subcats"].([]any)
	if !ok {
		c.e.t.Fatalf("week %s: subcats isn't a list", key)
	}
	out := []int64{}
	for _, x := range raw {
		out = append(out, int64(x.(float64)))
	}
	return out
}

// subcatName is a sub-category's name in the config ("" = no such sub-category).
func (c *client) subcatName(id int64) string {
	c.e.t.Helper()
	for _, x := range c.mustStatus(200, "GET", "/v1/track/config", nil)["subcats"].([]any) {
		sc := x.(map[string]any)
		if int64(sc["id"].(float64)) == id {
			return fmt.Sprint(sc["name"], map[bool]string{true: "(archived)", false: ""}[sc["archived"].(bool)])
		}
	}
	return ""
}

func ids(xs ...int64) []int64 { return append([]int64{}, xs...) }

func TestTrackWeekSubcats(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	const lastWeek, older = "2026-10-03", "2026-09-26"

	eq(t, "empty week", fmt.Sprint(a.weekSubs(thisWeek)), "[]")

	// A new name creates a sub-category in this week only.
	anime := a.mustAddWeekSub(thisWeek, "active", " Anime ", 0)
	eq(t, "created", a.subcatName(anime), "Anime")
	eqStrings(t, "this week", fmtIDs(a.weekSubs(thisWeek)), fmtIDs(ids(anime)))
	eq(t, "not last week", fmt.Sprint(a.weekSubs(lastWeek)), "[]")
	eq(t, "not bob's", fmt.Sprint(e.bob.weekSubs(thisWeek)), "[]")

	// The same name (any case) in another week is the same sub-category.
	eq(t, "reused", a.mustAddWeekSub(lastWeek, "active", "ANIME", 0), anime)
	eq(t, "name kept", a.subcatName(anime), "Anime")
	eqStrings(t, "last week", fmtIDs(a.weekSubs(lastWeek)), fmtIDs(ids(anime)))
	// …but not in another category.
	other := a.mustAddWeekSub(thisWeek, "passive", "Anime", 0)
	if other == anime {
		t.Fatal("passive Anime is the active one")
	}

	// An archived one is restored.
	pod := a.subcat("passive", "Podcasts")
	a.mustStatus(200, "POST", fmt.Sprint("/v1/track/subcats/", pod), map[string]any{"archived": true})
	eq(t, "restored", a.mustAddWeekSub(thisWeek, "passive", "podcasts", 0), pod)
	eq(t, "unarchived", a.subcatName(pod), "Podcasts")

	// Already in the week: by membership, or by hours alone.
	eq(t, "already added", a.addWeekSub(thisWeek, "active", "anime", 0).Code, 409)
	games := a.subcat("active", "Games") // not added to any week
	a.saveWeek(older, map[string]any{"hours": []any{hrs("active", games, 3)}})
	eq(t, "hours only: not a member", fmt.Sprint(a.weekSubs(older)), "[]")
	eq(t, "already there by hours", a.addWeekSub(older, "active", "Games", 0).Code, 409)

	// A past week keeps showing its hours for a sub-category it isn't a member of.
	a.saveWeek(older, map[string]any{"hours": []any{hrs("active", games, 3), hrs("active", anime, 1.5)}})
	eq(t, "past week hours", a.catHours("active", older), "Anime:1.5 Games:3")
	eq(t, "still not a member", fmt.Sprint(a.weekSubs(older)), "[]")

	// Validation.
	eq(t, "bad category", a.addWeekSub(thisWeek, "gaming", "X", 0).Code, 400)
	eq(t, "empty name", a.addWeekSub(thisWeek, "active", " ", 0).Code, 400)
	eq(t, "future week", a.addWeekSub("2026-10-17", "active", "X", 0).Code, 400)
	eq(t, "not a Saturday", a.addWeekSub("2026-10-11", "active", "X", 0).Code, 400)
	eq(t, "logged out", e.anon().addWeekSub(thisWeek, "active", "X", 0).Code, 401)

	// At most maxWeekSubcats per category in a week, hours-only ones counted.
	b := e.bob
	fillSubcats(t, e, b.id, "output", maxWeekSubcats)
	if _, err := e.db.Exec(`INSERT INTO track_week_subcats (user_id, week, subcat_id)
		SELECT user_id, ?, id FROM track_subcats WHERE user_id = ? AND category = 'output' AND name != 'S0'`, thisWeek, b.id); err != nil {
		t.Fatal(err)
	}
	b.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("output", b.subcatID("output", "S0"), 1)}})
	eq(t, "week full", b.addWeekSub(thisWeek, "output", "one more", 0).Code, 409)
	eq(t, "week full message", b.last.Body.String(), fmt.Sprintf(`{"error":"at most %d sub-categories per category in a week"}`+"\n", maxWeekSubcats))
	eq(t, "nothing created", b.subcatID("output", "one more"), int64(0))
	eq(t, "another category", b.addWeekSub(thisWeek, "active", "one more", 0).Code, 200)
	eq(t, "another week", b.addWeekSub(lastWeek, "output", "one more", 0).Code, 200)
	// Renaming one in the full week is fine: it frees its own slot.
	s1 := b.subcatID("output", "S1")
	eq(t, "rename in a full week", b.mustAddWeekSub(thisWeek, "output", "S1 renamed", s1), s1)
}

func TestTrackWeekSubcatRename(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	const lastWeek = "2026-10-03"

	// Only this week has it: renamed in place, same id.
	manga := a.mustAddWeekSub(thisWeek, "active", "Manga", 0)
	eq(t, "in place", a.mustAddWeekSub(thisWeek, "active", "Manga (JP)", manga), manga)
	eq(t, "renamed", a.subcatName(manga), "Manga (JP)")
	eq(t, "case only", a.mustAddWeekSub(thisWeek, "active", "manga (jp)", manga), manga)
	eq(t, "case renamed", a.subcatName(manga), "manga (jp)")
	eq(t, "same name", a.mustAddWeekSub(thisWeek, "active", "manga (jp)", manga), manga)
	eqStrings(t, "membership", fmtIDs(a.weekSubs(thisWeek)), fmtIDs(ids(manga)))

	// Another week uses it: this week switches to a (new) sub-category; the other week keeps it.
	anime := a.mustAddWeekSub(thisWeek, "active", "Anime", 0)
	a.mustAddWeekSub(lastWeek, "active", "Anime", 0)
	a.saveWeek(lastWeek, map[string]any{"hours": []any{hrs("active", anime, 2)}})
	anime2 := a.mustAddWeekSub(thisWeek, "active", "Anime 2", anime)
	if anime2 == anime {
		t.Fatal("renamed a sub-category another week uses")
	}
	eq(t, "old name kept", a.subcatName(anime), "Anime")
	eq(t, "new one", a.subcatName(anime2), "Anime 2")
	eqStrings(t, "this week switched", fmtIDs(a.weekSubs(thisWeek)), fmtIDs(ids(manga, anime2)))
	eqStrings(t, "last week untouched", fmtIDs(a.weekSubs(lastWeek)), fmtIDs(ids(anime)))
	eq(t, "last week hours", a.catHours("active", lastWeek), "Anime:2")

	// Switching to a name another week already has reuses that sub-category.
	yt := a.mustAddWeekSub(lastWeek, "active", "YouTube", 0)
	eq(t, "switch to existing", a.mustAddWeekSub(thisWeek, "active", "youtube", anime2), yt)
	eqStrings(t, "this week", fmtIDs(a.weekSubs(thisWeek)), fmtIDs(ids(manga, yt)))

	// A case-only rename of a sub-category another week uses keeps it in this week.
	eq(t, "shared case-only", a.mustAddWeekSub(thisWeek, "active", "Youtube", yt), yt)
	eqStrings(t, "still in this week", fmtIDs(a.weekSubs(thisWeek)), fmtIDs(ids(manga, yt)))
	eq(t, "shared case renamed", a.subcatName(yt), "Youtube")

	// Refusals.
	eq(t, "onto a name already here", a.addWeekSub(thisWeek, "active", "MANGA (JP)", yt).Code, 409)
	eq(t, "not in this week", a.addWeekSub(thisWeek, "active", "X", anime).Code, 404)
	eq(t, "wrong category", a.addWeekSub(thisWeek, "passive", "X", manga).Code, 404)
	eq(t, "someone else's", e.bob.addWeekSub(thisWeek, "active", "X", manga).Code, 404)
	eq(t, "missing", a.addWeekSub(thisWeek, "active", "X", 9999).Code, 404)
	eq(t, "unchanged", a.subcatName(manga), "manga (jp)")
}

func TestTrackWeekSubcatRemove(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	const lastWeek = "2026-10-03"
	remove := func(c *client, key string, id int64) map[string]any {
		t.Helper()
		return c.mustStatus(200, "DELETE", fmt.Sprint("/v1/track/weeks/", key, "/subcats/", id), nil)
	}

	// In no other week, no hours: removed and deleted.
	solo := a.mustAddWeekSub(thisWeek, "active", "Solo", 0)
	eq(t, "solo deleted", remove(a, thisWeek, solo)["deleted"], any(true))
	eq(t, "solo gone", a.subcatName(solo), "")
	eq(t, "solo out of the week", fmt.Sprint(a.weekSubs(thisWeek)), "[]")

	// Another week has hours for it: only this week's membership goes.
	shared := a.mustAddWeekSub(thisWeek, "active", "Shared", 0)
	a.saveWeek(lastWeek, map[string]any{"hours": []any{hrs("active", shared, 1)}})
	eq(t, "shared kept", remove(a, thisWeek, shared)["deleted"], any(false))
	eq(t, "shared exists", a.subcatName(shared), "Shared")
	eq(t, "shared out of the week", fmt.Sprint(a.weekSubs(thisWeek)), "[]")
	eq(t, "other week's hours", a.catHours("active", lastWeek), "Shared:1")

	// Another week has it added (no hours anywhere): kept too.
	both := a.mustAddWeekSub(thisWeek, "passive", "Both", 0)
	a.mustAddWeekSub(lastWeek, "passive", "Both", 0)
	eq(t, "member elsewhere kept", remove(a, thisWeek, both)["deleted"], any(false))
	eqStrings(t, "other week keeps it", fmtIDs(a.weekSubs(lastWeek)), fmtIDs(ids(both)))
	eq(t, "then the last week", remove(a, lastWeek, both)["deleted"], any(true))

	// Hours in this week still show it (and keep it) until they're cleared.
	h := a.mustAddWeekSub(thisWeek, "output", "Diary", 0)
	a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("output", h, 1)}})
	eq(t, "with hours kept", remove(a, thisWeek, h)["deleted"], any(false))
	eq(t, "hours still shown", a.catHours("output", thisWeek), "Diary:1")

	// Someone else's, or a missing one: 404, nothing changes.
	mine := a.mustAddWeekSub(thisWeek, "active", "Mine", 0)
	eq(t, "other user's", e.bob.do("DELETE", fmt.Sprint("/v1/track/weeks/", thisWeek, "/subcats/", mine), nil).Code, 404)
	eq(t, "missing", a.do("DELETE", fmt.Sprint("/v1/track/weeks/", thisWeek, "/subcats/9999"), nil).Code, 404)
	eq(t, "bad id", a.do("DELETE", fmt.Sprint("/v1/track/weeks/", thisWeek, "/subcats/x"), nil).Code, 404)
	eqStrings(t, "mine kept", fmtIDs(a.weekSubs(thisWeek)), fmtIDs(ids(mine)))
	eq(t, "mine exists", a.subcatName(mine), "Mine")
}

// Saving a week deletes a sub-category that had hours there and is afterwards in no week at all:
// what a rename that switched sub-categories leaves behind once the form moves its hours.
func TestTrackWeekSaveDeletesUnused(t *testing.T) {
	e := newEnv(t)
	a := e.alice
	const lastWeek = "2026-10-03"

	// Old has hours this week and is added to last week; a rename here switches to New.
	old := a.mustAddWeekSub(thisWeek, "active", "Old", 0)
	a.mustAddWeekSub(lastWeek, "active", "Old", 0)
	a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", old, 2)}})
	nw := a.mustAddWeekSub(thisWeek, "active", "New", old)
	if nw == old {
		t.Fatal("renamed in place")
	}
	// The hours move with the save; Old is still in last week, so it stays.
	a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nw, 2)}})
	eq(t, "kept while last week has it", a.subcatName(old), "Old")
	eq(t, "moved", a.catHours("active", thisWeek), "New:2")

	// Same again once last week no longer has it: the save deletes it.
	nw2 := a.mustAddWeekSub(thisWeek, "active", "Newer", nw)
	eq(t, "in place: New is only here", nw2, nw)
	a.mustStatus(200, "DELETE", fmt.Sprint("/v1/track/weeks/", lastWeek, "/subcats/", old), nil) // deleted: no hours anywhere
	eq(t, "old deleted by remove", a.subcatName(old), "")

	other := a.mustAddWeekSub(lastWeek, "active", "Other", 0)
	gone := a.mustAddWeekSub(thisWeek, "active", "Gone", 0)
	a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nw, 2), hrs("active", gone, 1)}})
	// Switch Gone → Other (another week's name), then save with the hour moved.
	eq(t, "switch", a.mustAddWeekSub(thisWeek, "active", "other", gone), other)
	eq(t, "gone still exists before the save", a.subcatName(gone), "Gone")
	a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", nw, 2), hrs("active", other, 1)}})
	eq(t, "gone deleted by the save", a.subcatName(gone), "")
	eq(t, "hours", a.catHours("active", thisWeek), "Newer:2 Other:1")

	// A sub-category still added to this week survives losing its hours.
	a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", other, 1)}})
	eq(t, "member kept", a.subcatName(nw), "Newer")
	// …and one that only had hours here (never added) goes when they're cleared.
	stray := a.subcat("active", "Stray")
	a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", stray, 1)}})
	a.saveWeek(thisWeek, map[string]any{"hours": []any{}})
	eq(t, "stray deleted", a.subcatName(stray), "")
	// A rejected save deletes nothing.
	a.saveWeek(thisWeek, map[string]any{"hours": []any{hrs("active", other, 1)}})
	eq(t, "bad save", a.do("POST", "/v1/track/weeks/"+thisWeek, map[string]any{"hours": []any{hrs("active", nil, 200)}}).Code, 400)
	eq(t, "other kept", a.subcatName(other), "Other")
}

// subcatID is the id of the user's sub-category of cat named name (0 = none).
func (c *client) subcatID(cat, name string) int64 {
	c.e.t.Helper()
	for _, x := range c.mustStatus(200, "GET", "/v1/track/config", nil)["subcats"].([]any) {
		sc := x.(map[string]any)
		if sc["category"] == cat && sc["name"] == name {
			return int64(sc["id"].(float64))
		}
	}
	return 0
}

func fmtIDs(xs []int64) []string {
	xs = slices.Clone(xs)
	slices.Sort(xs)
	out := []string{}
	for _, x := range xs {
		out = append(out, fmt.Sprint(x))
	}
	return out
}
