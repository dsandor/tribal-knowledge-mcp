package web

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/dsandor/memory/internal/auth"
	"github.com/dsandor/memory/internal/storage"
)

type topEntry struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Domain     string  `json:"domain"`
	Score      float64 `json:"score"`
	UsageCount int     `json:"usage_count"`
	Rating     float64 `json:"rating"`
}

type domainStat struct {
	Domain     string  `json:"domain"`
	EntryCount int     `json:"entry_count"`
	AvgRating  float64 `json:"avg_rating"`
	TotalUsage int     `json:"total_usage"`
}

type heatmapPoint struct {
	Week   string `json:"week"`
	Domain string `json:"domain"`
	Usage  int    `json:"usage"`
}

func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	ctx := r.Context()

	entries, err := s.store.ListEntries(ctx, storage.ListFilter{Limit: 200, TeamID: tc.TeamID, Status: "approved"})
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list entries: %v", err))
		return
	}

	type scored struct {
		e     storage.KnowledgeEntry
		score float64
	}
	var scored_ []scored
	domainMap := map[string]*domainStat{}
	for _, e := range entries {
		sc := e.Rating * float64(e.UsageCount)
		scored_ = append(scored_, scored{e, sc})
		ds := domainMap[e.Domain]
		if ds == nil {
			ds = &domainStat{Domain: e.Domain}
			domainMap[e.Domain] = ds
		}
		ds.EntryCount++
		ds.TotalUsage += e.UsageCount
		ds.AvgRating += e.Rating
	}
	sort.Slice(scored_, func(i, j int) bool { return scored_[i].score > scored_[j].score })

	top := make([]topEntry, 0, 10)
	for i, sc := range scored_ {
		if i >= 10 {
			break
		}
		top = append(top, topEntry{ID: sc.e.ID, Title: sc.e.Title, Domain: sc.e.Domain, Score: sc.score, UsageCount: sc.e.UsageCount, Rating: sc.e.Rating})
	}

	domains := make([]domainStat, 0, len(domainMap))
	for _, ds := range domainMap {
		if ds.EntryCount > 0 {
			ds.AvgRating = ds.AvgRating / float64(ds.EntryCount)
		}
		domains = append(domains, *ds)
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i].TotalUsage > domains[j].TotalUsage })

	actLog, _ := s.store.QueryActivity(ctx, tc.TeamID, 1000)
	type heatKey struct{ week, domain string }
	heatMap := map[heatKey]int{}
	entryDomains := map[string]string{}
	for _, e := range entries {
		entryDomains[e.ID] = e.Domain
	}
	for _, a := range actLog {
		if a.Action != "knowledge.get" && a.Action != "prompt.enhance" {
			continue
		}
		year, week := a.CreatedAt.ISOWeek()
		weekStr := fmt.Sprintf("%d-W%02d", year, week)
		domain := entryDomains[a.EntityID]
		heatMap[heatKey{weekStr, domain}]++
	}
	heatSlice := make([]heatmapPoint, 0, len(heatMap))
	for k, v := range heatMap {
		heatSlice = append(heatSlice, heatmapPoint{Week: k.week, Domain: k.domain, Usage: v})
	}
	sort.Slice(heatSlice, func(i, j int) bool { return heatSlice[i].Week > heatSlice[j].Week })

	writeJSON(w, map[string]any{
		"top_entries": top,
		"by_domain":   domains,
		"heatmap":     heatSlice,
	})
}

type gapEntry struct {
	Domain     string `json:"domain"`
	EntryCount int    `json:"entry_count"`
	Threshold  int    `json:"threshold"`
	Severity   string `json:"severity"`
}

func (s *Server) handleGaps(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	ctx := r.Context()

	settings, err := s.store.GetTeamSettings(ctx, tc.TeamID)
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("get settings: %v", err))
		return
	}
	threshold := settings.PipelineMinEntries
	if threshold == 0 {
		threshold = 10
	}

	entries, err := s.store.ListEntries(ctx, storage.ListFilter{Limit: 500, TeamID: tc.TeamID, Status: "approved"})
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list entries: %v", err))
		return
	}

	domainCounts := map[string]int{}
	for _, e := range entries {
		if e.Domain != "" {
			domainCounts[e.Domain]++
		}
	}
	for _, d := range settings.Domains {
		if _, ok := domainCounts[d]; !ok {
			domainCounts[d] = 0
		}
	}

	var gaps []gapEntry
	for domain, count := range domainCounts {
		if count >= threshold {
			continue
		}
		pct := float64(count) / float64(threshold)
		severity := "high"
		if pct >= 0.5 {
			severity = "medium"
		}
		if pct >= 0.8 {
			severity = "low"
		}
		gaps = append(gaps, gapEntry{Domain: domain, EntryCount: count, Threshold: threshold, Severity: severity})
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].EntryCount < gaps[j].EntryCount })
	if gaps == nil {
		gaps = []gapEntry{}
	}
	writeJSON(w, map[string]any{"gaps": gaps})
}

type leaderEntry struct {
	Author        string  `json:"author"`
	EntryCount    int     `json:"entry_count"`
	ApprovedCount int     `json:"approved_count"`
	TotalUsage    int     `json:"total_usage"`
	AvgRating     float64 `json:"avg_rating"`
	Score         float64 `json:"score"`
}

func (s *Server) handleContributions(w http.ResponseWriter, r *http.Request) {
	tc := auth.GetTeamContext(r.Context())
	ctx := r.Context()

	entries, err := s.store.ListEntries(ctx, storage.ListFilter{Limit: 500, TeamID: tc.TeamID})
	if err != nil {
		writeError(w, 500, "internal_error", fmt.Sprintf("list entries: %v", err))
		return
	}

	type authorStats struct {
		entryCount, approvedCount, totalUsage int
		ratingSum                             float64
		ratingCount                           int
	}
	byAuthor := map[string]*authorStats{}
	for _, e := range entries {
		if e.Author == "" {
			continue
		}
		a := byAuthor[e.Author]
		if a == nil {
			a = &authorStats{}
			byAuthor[e.Author] = a
		}
		a.entryCount++
		if e.Status == "approved" {
			a.approvedCount++
		}
		a.totalUsage += e.UsageCount
		if e.Rating > 0 {
			a.ratingSum += e.Rating
			a.ratingCount++
		}
	}

	leaderboard := make([]leaderEntry, 0, len(byAuthor))
	for author, a := range byAuthor {
		avgRating := 0.0
		if a.ratingCount > 0 {
			avgRating = a.ratingSum / float64(a.ratingCount)
		}
		score := float64(a.approvedCount*2) + avgRating*float64(a.totalUsage)
		leaderboard = append(leaderboard, leaderEntry{
			Author:        author,
			EntryCount:    a.entryCount,
			ApprovedCount: a.approvedCount,
			TotalUsage:    a.totalUsage,
			AvgRating:     avgRating,
			Score:         score,
		})
	}
	sort.Slice(leaderboard, func(i, j int) bool { return leaderboard[i].Score > leaderboard[j].Score })
	if leaderboard == nil {
		leaderboard = []leaderEntry{}
	}
	writeJSON(w, map[string]any{"leaderboard": leaderboard})
}
