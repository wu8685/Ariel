package puck

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var exactTokenPattern = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9._/#-]{2,}`)

func exactTokens(query string) []string {
	seen := map[string]bool{}
	var tokens []string
	for _, token := range exactTokenPattern.FindAllString(query, -1) {
		token = strings.ToLower(token)
		if seen[token] || utf8.RuneCountInString(token) < 3 {
			continue
		}
		seen[token] = true
		tokens = append(tokens, token)
		if len(tokens) == 8 {
			break
		}
	}
	return tokens
}

func rankMemories(entries []Memory, distance map[string]float64, keyword, exact map[string]bool) []Memory {
	type scored struct {
		entry Memory
		score float64
	}
	ranked := make([]scored, 0, len(entries))
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.ID] {
			continue
		}
		seen[entry.ID] = true
		item := scored{entry: entry}
		if value, ok := distance[entry.ID]; ok {
			if value > 1 {
				value = 1
			}
			if value < 0 {
				value = 0
			}
			item.score += 5 * (1 - value)
		}
		if keyword[entry.ID] {
			item.score += 10
		}
		if exact[entry.ID] {
			item.score += 100
		}
		ranked = append(ranked, item)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if !ranked[i].entry.CreatedAt.Equal(ranked[j].entry.CreatedAt) {
			return ranked[i].entry.CreatedAt.After(ranked[j].entry.CreatedAt)
		}
		return ranked[i].entry.ID < ranked[j].entry.ID
	})
	out := make([]Memory, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.entry)
	}
	return out
}
