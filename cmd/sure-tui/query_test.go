package main

import (
	"testing"
	"time"
)

func TestRelRange(t *testing.T) {
	if _, _, ok := relRange("lunch"); ok {
		t.Fatal("non-keyword should not match")
	}
	want := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	if got := ParseQuery("week").From; got != want {
		t.Fatalf("week from = %q, want %q", got, want)
	}
	// Compact spans: 30d, 2w, 3m, 1y.
	for tok, exp := range map[string]string{
		"30d": time.Now().AddDate(0, 0, -30).Format("2006-01-02"),
		"2w":  time.Now().AddDate(0, 0, -14).Format("2006-01-02"),
		"3m":  time.Now().AddDate(0, -3, 0).Format("2006-01-02"),
		"1y":  time.Now().AddDate(-1, 0, 0).Format("2006-01-02"),
	} {
		if got := ParseQuery(tok).From; got != exp {
			t.Fatalf("%s from = %q, want %q", tok, got, exp)
		}
	}
	// Month name resolves to a full month range, most recent occurrence.
	q := ParseQuery("may")
	if q.From == "" || q.To == "" || q.From[5:7] != "05" || q.To[5:7] != "05" {
		t.Fatalf("may range = %q..%q", q.From, q.To)
	}
	if q.From[8:] != "01" || q.To[8:] != "31" {
		t.Fatalf("may bounds = %q..%q, want 01..31", q.From, q.To)
	}
}

func TestQuery(t *testing.T) {
	txns := []Txn{
		{Date: "2026-05-02", Amount: "100", Classification: "income", Name: "Paycheck",
			Account: Account{Name: "Cash"}, Category: &Category{Name: "Salary"}},
		{Date: "2026-05-10", Amount: "20", Classification: "expense", Name: "Apples",
			Account: Account{Name: "Cash"}, Category: &Category{Name: "Grocery"},
			Tags: []Tag{{Name: "Travel"}}},
		{Date: "2026-06-01", Amount: "50", Classification: "expense", Name: "Hotel",
			Account: Account{Name: "Visa"}, Category: &Category{Name: "Travel"}},
		{Date: "2026-05-15", Amount: "5", Classification: "expense", Name: "xfer",
			Account: Account{Name: "Cash"}, Transfer: &Transfer{}},
	}
	count := func(s string) int { return len(filterTxns(txns, ParseQuery(s))) }

	// income + expense excludes the transfer.
	if n := count("/income, expense"); n != 3 {
		t.Fatalf("income+expense = %d, want 3", n)
	}
	// income alone, no transfers.
	if n := count("/income"); n != 1 {
		t.Fatalf("income = %d, want 1", n)
	}
	// date range is inclusive on both ends.
	if n := count("/from: 2026-05-01, to: 2026-05-31"); n != 3 {
		t.Fatalf("date range = %d, want 3", n)
	}
	// multiple categories OR.
	if n := count("/+Grocery, +Travel"); n != 2 {
		t.Fatalf("cat OR = %d, want 2", n)
	}
	// tag + name AND, matches the tagged Apples row only.
	if n := count("/@Travel, apple"); n != 1 {
		t.Fatalf("tag+name = %d, want 1", n)
	}
	// account token with a paren; combined with expense.
	if n := count("/(Visa), expense"); n != 1 {
		t.Fatalf("account+expense = %d, want 1", n)
	}
	// empty query passes everything through.
	if n := count(""); n != 4 {
		t.Fatalf("empty = %d, want 4", n)
	}
}

func TestSelMatch(t *testing.T) {
	t1 := Txn{Account: Account{ID: "a1"}, Category: &Category{ID: "c1"}, Tags: []Tag{{ID: "g1"}}}
	t2 := Txn{Account: Account{ID: "a2"}, Category: &Category{ID: "c2"}}

	m := &model{selAccts: map[string]filterState{}, selCats: map[string]filterState{}, selTags: map[string]filterState{}}
	if !m.selMatch(t1) || !m.selMatch(t2) {
		t.Fatal("no selection should match everything")
	}
	// Include OR within a kind: either account passes.
	m.selAccts["a1"], m.selAccts["a2"] = fInclude, fInclude
	if !m.selMatch(t1) || !m.selMatch(t2) {
		t.Fatal("account include OR failed")
	}
	// AND across kinds: including category c1 drops t2.
	m.selCats["c1"] = fInclude
	if !m.selMatch(t1) || m.selMatch(t2) {
		t.Fatal("category include AND failed")
	}
	// Exclude always rejects, even with no includes in that kind.
	m2 := &model{selAccts: map[string]filterState{"a1": fExclude}, selCats: map[string]filterState{}, selTags: map[string]filterState{}}
	if m2.selMatch(t1) || !m2.selMatch(t2) {
		t.Fatal("account exclude failed")
	}
	// Tag exclude rejects a txn carrying that tag.
	m3 := &model{selAccts: map[string]filterState{}, selCats: map[string]filterState{}, selTags: map[string]filterState{"g1": fExclude}}
	if m3.selMatch(t1) || !m3.selMatch(t2) {
		t.Fatal("tag exclude failed")
	}
	// Tag include requires a matching tag.
	m4 := &model{selAccts: map[string]filterState{}, selCats: map[string]filterState{}, selTags: map[string]filterState{"gX": fInclude}}
	if m4.selMatch(t1) || m4.selMatch(t2) {
		t.Fatal("tag include should require the tag")
	}
}
