package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

const dateFmt = "2006-01-02"

// spanRe matches a compact lookback like "30d", "2w", "3m", "1y".
var spanRe = regexp.MustCompile(`^(\d+)([dwmy])$`)

// spanAlias maps friendly words to the compact span syntax.
var spanAlias = map[string]string{"today": "0d", "week": "1w", "month": "1m", "year": "1y"}

// relRange maps a relative keyword to a date range, or ok=false if it isn't one.
// Spans (e.g. "30d", "2w", "month") start that far back with an open end. Month
// names ("july", "may"…) resolve to the most recent occurrence of that month.
func relRange(s string) (from, to string, ok bool) {
	now := time.Now()
	if mo, isMonth := monthNames[s]; isMonth {
		y := now.Year()
		if mo > now.Month() { // hasn't happened yet this year -> last year
			y--
		}
		start := time.Date(y, mo, 1, 0, 0, 0, 0, time.UTC)
		return start.Format(dateFmt), start.AddDate(0, 1, -1).Format(dateFmt), true
	}
	if a, isAlias := spanAlias[s]; isAlias {
		s = a
	}
	m := spanRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	n, _ := strconv.Atoi(m[1])
	var f time.Time
	switch m[2] {
	case "d":
		f = now.AddDate(0, 0, -n)
	case "w":
		f = now.AddDate(0, 0, -7*n)
	case "m":
		f = now.AddDate(0, -n, 0)
	case "y":
		f = now.AddDate(-n, 0, 0)
	}
	return f.Format(dateFmt), "", true
}

var monthNames = map[string]time.Month{
	"january": 1, "february": 2, "march": 3, "april": 4, "may": 5, "june": 6,
	"july": 7, "august": 8, "september": 9, "october": 10, "november": 11, "december": 12,
}

// Query is the advanced "/" filter, applied client-side over all transactions.
//
//	/from: 2026-05-01, to: 2026-05-29, income, +Grocery, @Travel, (Cash), some name
//
// Semantics:
//   - income / expense: only that side, transfers always excluded. Both flags
//     together => every non-transfer.
//   - +categories / @tags / (accounts): match ANY listed (OR within a kind).
//   - bare words: name must contain every term (AND).
//   - all kinds are ANDed together.
type Query struct {
	From, To                  string
	Income, Expense, Transfer bool
	Categories                []string
	Tags            []string
	Accounts        []string
	Names           []string
	raw             string
}

func (q Query) empty() bool {
	return !q.Income && !q.Expense && !q.Transfer && q.From == "" && q.To == "" &&
		len(q.Categories) == 0 && len(q.Tags) == 0 && len(q.Accounts) == 0 && len(q.Names) == 0
}

// ParseQuery parses the slash DSL. Names match by substring, so nothing is
// resolved to IDs here.
func ParseQuery(raw string) Query {
	q := Query{raw: strings.TrimSpace(raw)}
	s := strings.TrimPrefix(q.raw, "/")

	// Pull out (account) groups first; their names may contain commas.
	for _, m := range acctRe.FindAllStringSubmatch(s, -1) {
		if t := strings.TrimSpace(m[1]); t != "" {
			q.Accounts = append(q.Accounts, t)
		}
	}
	s = acctRe.ReplaceAllString(s, "")

	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch low := strings.ToLower(part); {
		case low == "income":
			q.Income = true
		case low == "expense":
			q.Expense = true
		case low == "transfer" || low == "transfers":
			q.Transfer = true
		case strings.HasPrefix(low, "from:"):
			q.From = strings.TrimSpace(part[5:])
		case strings.HasPrefix(low, "to:"):
			q.To = strings.TrimSpace(part[3:])
		case strings.HasPrefix(part, "+") && len(part) > 1:
			q.Categories = append(q.Categories, part[1:])
		case strings.HasPrefix(part, "@") && len(part) > 1:
			q.Tags = append(q.Tags, part[1:])
		default:
			if from, to, ok := relRange(low); ok {
				q.From = from
				if to != "" {
					q.To = to
				}
			} else {
				q.Names = append(q.Names, part)
			}
		}
	}
	return q
}

func (q Query) match(t Txn) bool {
	if q.From != "" && t.Date < q.From {
		return false
	}
	if q.To != "" && t.Date > q.To {
		return false
	}
	// Transfers are their own thing: only the transfer filter shows them, and
	// income/expense always exclude them.
	switch {
	case q.Transfer:
		if !t.IsTransfer() {
			return false
		}
	case q.Income || q.Expense:
		if t.IsTransfer() {
			return false
		}
		if q.Income && !q.Expense && t.Classification != "income" {
			return false
		}
		if q.Expense && !q.Income && t.Classification == "income" {
			return false
		}
	}
	if len(q.Accounts) > 0 && !containsAny(t.Account.Name, q.Accounts) {
		return false
	}
	if len(q.Categories) > 0 {
		if t.Category == nil || !containsAny(t.Category.Name, q.Categories) {
			return false
		}
	}
	if len(q.Tags) > 0 {
		ok := false
		for _, tg := range t.Tags {
			if containsAny(tg.Name, q.Tags) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, n := range q.Names { // AND
		if !strings.Contains(strings.ToLower(t.Name), strings.ToLower(n)) {
			return false
		}
	}
	return true
}

// containsAny is true if hay (case-insensitively) contains any of subs.
func containsAny(hay string, subs []string) bool {
	h := strings.ToLower(hay)
	for _, s := range subs {
		if strings.Contains(h, strings.ToLower(strings.TrimSpace(s))) {
			return true
		}
	}
	return false
}

func filterTxns(txns []Txn, q Query) []Txn {
	if q.empty() {
		return txns
	}
	out := make([]Txn, 0, len(txns))
	for _, t := range txns {
		if q.match(t) {
			out = append(out, t)
		}
	}
	return out
}
