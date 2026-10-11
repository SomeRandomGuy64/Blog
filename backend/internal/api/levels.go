package api

import (
	"net/http"
	"sort"
)

const maxLevel = 100

// levelNeed[i] is the cumulative XP needed for level i+1: 5·(L−1)², so 0, 5, 20, 45, 80, …
// 49005. Each step is 10 XP bigger than the one before.
var levelNeed = func() (n [maxLevel]int64) {
	for l := int64(1); l <= maxLevel; l++ {
		n[l-1] = 5 * (l - 1) * (l - 1)
	}
	return n
}()

// levelOf returns the level for xp, the XP that level started at, and the XP for the next one
// (nil at the top level).
func levelOf(xp int64) (level int, levelXP int64, nextXP *int64) {
	level = sort.Search(maxLevel, func(i int) bool { return levelNeed[i] > xp }) // levels 1..level qualify
	level = max(level, 1)
	levelXP = levelNeed[level-1]
	if level < maxLevel {
		next := levelNeed[level]
		nextXP = &next
	}
	return level, levelXP, nextXP
}

func (s *Server) levels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=86400")
	writeJSON(w, http.StatusOK, map[string]any{"levels": levelNeed[:]})
}
