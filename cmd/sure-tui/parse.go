package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	dateRe   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	amountRe = regexp.MustCompile(`^[+-]?\d+(\.\d+)?$`)
	acctRe   = regexp.MustCompile(`\(([^)]*)\)`)
	// amtCurRe matches an amount with a 3-letter currency suffix: "-100USD".
	amtCurRe = regexp.MustCompile(`^([+-]?\d+(?:\.\d+)?)([A-Za-z]{3})$`)
	// curRe matches a standalone uppercase currency code: "USD".
	// ponytail: an all-caps 3-letter name word (e.g. "ATM") would be read as a
	// currency; quote-free DSL, accept it — use the amount suffix to be explicit.
	curRe = regexp.MustCompile(`^[A-Z]{3}$`)
)

// Parse turns the entry DSL into a TxnReq, resolving names to IDs.
//
//	2026-06-29 (Anor, *0635) +20000.00 The grocery +grocery @tag
//
// + amount => income, - or none => expense. Account is required.
func Parse(line string, accts []Account, cats []Category, tags []Tag) (TxnReq, error) {
	var req TxnReq
	req.Date = time.Now().Format("2006-01-02")
	req.Nature = "expense"

	// Account: first (...) group, removed from the line before word parsing.
	var acctCur string
	if m := acctRe.FindStringSubmatch(line); m != nil {
		a := findByName(accountNames(accts), m[1])
		if a < 0 {
			return req, fmt.Errorf("no account matching %q", m[1])
		}
		req.AccountID = accts[a].ID
		acctCur = accts[a].Currency
		line = acctRe.ReplaceAllString(line, "")
	} else {
		return req, fmt.Errorf("account required, e.g. (Checking)")
	}

	// Pull out date and amount first (order-independent), leaving name and
	// markers. The rest is segmented on +/@ markers; everything up to the next
	// marker is one value, so category/tag names may contain spaces.
	var rest []string
	setAmount := func(tok string) {
		if strings.HasPrefix(tok, "+") {
			req.Nature = "income"
		}
		req.Amount = strings.TrimLeft(tok, "+-")
	}
	for _, tok := range strings.Fields(line) {
		switch {
		case dateRe.MatchString(tok):
			req.Date = tok
		case amountRe.MatchString(tok):
			setAmount(tok)
		case amtCurRe.MatchString(tok): // amount with currency suffix, e.g. -100USD
			m := amtCurRe.FindStringSubmatch(tok)
			setAmount(m[1])
			req.Currency = strings.ToUpper(m[2])
		case curRe.MatchString(tok): // standalone currency code, e.g. USD
			req.Currency = tok
		default:
			rest = append(rest, tok)
		}
	}
	if req.Currency == "" {
		req.Currency = acctCur // default to the account's currency
	}

	// Leading words (before any marker) are the transaction name.
	var nameParts []string
	i := 0
	for ; i < len(rest); i++ {
		if isMarker(rest[i]) {
			break
		}
		nameParts = append(nameParts, rest[i])
	}
	req.Name = strings.Join(nameParts, " ")

	// Remaining words form +category / @tag segments (multi-word allowed).
	marker, seg := byte(0), []string(nil)
	flush := func() error {
		if marker == 0 {
			return nil
		}
		val := strings.Join(seg, " ")
		if marker == '+' {
			j := findByName(categoryNames(cats), val)
			if j < 0 {
				return fmt.Errorf("no category matching %q", val)
			}
			req.CategoryID = cats[j].ID
		} else {
			j := findByName(tagNames(tags), val)
			if j < 0 {
				return fmt.Errorf("no tag matching %q", val)
			}
			req.TagIDs = append(req.TagIDs, tags[j].ID)
		}
		return nil
	}
	for ; i < len(rest); i++ {
		if isMarker(rest[i]) {
			if err := flush(); err != nil {
				return req, err
			}
			marker, seg = rest[i][0], []string{rest[i][1:]}
		} else {
			seg = append(seg, rest[i])
		}
	}
	if err := flush(); err != nil {
		return req, err
	}
	if req.Amount == "" {
		return req, fmt.Errorf("amount required")
	}
	if req.Name == "" {
		return req, fmt.Errorf("name required")
	}
	return req, nil
}

// isMarker reports whether tok begins a +category or @tag segment.
func isMarker(tok string) bool {
	return len(tok) > 1 && (tok[0] == '+' || tok[0] == '@')
}

// Suggest returns name completions for the trailing token being typed.
func Suggest(line string, accts []Account, cats []Category, tags []Tag) []string {
	// Open "(" with no closing paren yet => account.
	if i := strings.LastIndex(line, "("); i >= 0 && !strings.Contains(line[i:], ")") {
		return matches(accountNames(accts), line[i+1:])
	}
	fields := strings.Fields(line)
	if len(fields) == 0 || strings.HasSuffix(line, " ") {
		return nil
	}
	tok := fields[len(fields)-1]
	switch {
	// A bare "+"/"@" lists everything; "+" alone isn't an amount yet (amountRe
	// needs a digit), so categories still win until a number is typed.
	case strings.HasPrefix(tok, "+") && !amountRe.MatchString(tok):
		return prefixed("+", matches(categoryNames(cats), tok[1:]))
	case strings.HasPrefix(tok, "@"):
		return prefixed("@", matches(tagNames(tags), tok[1:]))
	}
	return nil
}

func accountNames(a []Account) []string {
	n := make([]string, len(a))
	for i, x := range a {
		n[i] = x.Name
	}
	return n
}

func categoryNames(c []Category) []string {
	n := make([]string, len(c))
	for i, x := range c {
		n[i] = x.Name
	}
	return n
}

func tagNames(t []Tag) []string {
	n := make([]string, len(t))
	for i, x := range t {
		n[i] = x.Name
	}
	return n
}

// findByName resolves a typed name to an index: an exact case-insensitive match
// wins outright, otherwise the first substring match. Without the exact-match
// preference a name that is a substring of a longer one (e.g. "Bank" vs "Bank
// Card") could never be selected.
func findByName(names []string, q string) int {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return -1
	}
	sub := -1
	for i, n := range names {
		switch ln := strings.ToLower(n); {
		case ln == q:
			return i
		case sub < 0 && strings.Contains(ln, q):
			sub = i
		}
	}
	return sub
}

func matches(names []string, q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []string
	for _, n := range names {
		if q == "" || strings.Contains(strings.ToLower(n), q) {
			out = append(out, n)
		}
		if len(out) >= 6 {
			break
		}
	}
	return out
}

func prefixed(p string, ss []string) []string {
	for i := range ss {
		ss[i] = p + ss[i]
	}
	return ss
}
