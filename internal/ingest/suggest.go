package ingest

import (
	"sort"
	"strings"
)

// Suggestions returns up to n symbols that look like the query.
func Suggestions(docs []*Document, query string, n int) []string {
	symbols := make([]string, 0, len(docs))
	for _, d := range docs {
		symbols = append(symbols, d.Symbol)
	}
	return SuggestStrings(symbols, query, n)
}

// SuggestStrings is the symbol-only variant used by the index layer.
func SuggestStrings(symbols []string, query string, n int) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}

	type scored struct {
		symbol string
		score  int
	}
	var matches []scored

	for _, symbol := range symbols {
		sym := strings.ToLower(symbol)
		switch {
		case sym == q:
			continue
		case strings.HasPrefix(sym, q), strings.HasSuffix(sym, q):
			matches = append(matches, scored{symbol, 0})
		case strings.Contains(sym, q):
			matches = append(matches, scored{symbol, 1})
		default:
			if dist := closestDistance(q, sym); dist <= 2 {
				matches = append(matches, scored{symbol, 2 + dist})
			}
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score < matches[j].score
		}
		return len(matches[i].symbol) < len(matches[j].symbol)
	})

	out := make([]string, 0, n)
	for _, m := range matches {
		out = append(out, m.symbol)
		if len(out) == n {
			break
		}
	}
	return out
}

func closestDistance(query, symbol string) int {
	q := query
	if i := strings.LastIndex(q, "."); i >= 0 {
		q = q[i+1:]
	}
	best := levenshtein(q, symbol)
	for _, seg := range strings.Split(symbol, ".") {
		if d := levenshtein(q, seg); d < best {
			best = d
		}
	}
	return best
}

func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}
