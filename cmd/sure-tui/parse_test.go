package main

import "testing"

func TestParse(t *testing.T) {
	accts := []Account{{ID: "a1", Name: "Anor, *0635"}, {ID: "a2", Name: "Cash"}}
	cats := []Category{{ID: "c1", Name: "Grocery"}}
	tags := []Tag{{ID: "t1", Name: "tag"}}

	got, err := Parse("2026-06-29 (Anor, *0635) +20000.00 The grocery +grocery @tag", accts, cats, tags)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "a1" || got.Date != "2026-06-29" || got.Amount != "20000.00" ||
		got.Nature != "income" || got.CategoryID != "c1" || got.Name != "The grocery" ||
		len(got.TagIDs) != 1 || got.TagIDs[0] != "t1" {
		t.Fatalf("unexpected parse: %+v", got)
	}

	// Minus => expense, default date, no sign keeps expense.
	exp, err := Parse("(Cash) -5 coffee", accts, cats, tags)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Nature != "expense" || exp.Amount != "5" || exp.AccountID != "a2" {
		t.Fatalf("expense parse: %+v", exp)
	}

	// Missing account is an error.
	if _, err := Parse("+10 lunch", accts, cats, tags); err == nil {
		t.Fatal("expected error for missing account")
	}

	// Category names with spaces must resolve, with the name kept intact.
	sp := []Category{{ID: "c9", Name: "Grocery Shopping"}}
	g, err := Parse("(Cash) -5 weekly run +Grocery Shopping", accts, sp, tags)
	if err != nil {
		t.Fatal(err)
	}
	if g.Name != "weekly run" || g.CategoryID != "c9" {
		t.Fatalf("spaced category: %+v", g)
	}

	// Exact account name wins over a longer one it's a substring of: "Bank" must
	// resolve to "Bank", not be shadowed by "Bank Card".
	two := []Account{{ID: "b1", Name: "Bank Card"}, {ID: "b2", Name: "Bank"}}
	bk, err := Parse("(Bank) -5 x", two, nil, nil)
	if err != nil || bk.AccountID != "b2" {
		t.Fatalf("exact account match: %+v err=%v", bk, err)
	}

	// Mistyped category/tag must error, not silently vanish.
	if _, err := Parse("(Cash) -5 coffee +grocey", accts, cats, tags); err == nil {
		t.Fatal("expected error for unknown category")
	}
	if _, err := Parse("(Cash) -5 coffee @nope", accts, cats, tags); err == nil {
		t.Fatal("expected error for unknown tag")
	}
}

func TestComma(t *testing.T) {
	cases := map[float64]string{0: "0.00", 12.5: "12.50", 1234.5: "1,234.50", 1234567.89: "1,234,567.89", -1234.5: "-1,234.50"}
	for in, want := range cases {
		if got := comma(in); got != want {
			t.Fatalf("comma(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestBalanceDelta(t *testing.T) {
	// income raises, expense lowers, by magnitude regardless of AmountCents sign.
	inc := Txn{AmountCents: -5000, Classification: "income"}
	exp := Txn{AmountCents: 5000, Classification: "expense"}
	if balanceDeltaCents(inc) != 5000 || balanceDeltaCents(exp) != -5000 {
		t.Fatalf("delta: inc=%d exp=%d", balanceDeltaCents(inc), balanceDeltaCents(exp))
	}

	// Instant create then edit: balance nets the change, not double-counts.
	m := &model{accounts: []Account{{ID: "a1", BalanceCents: 10000}}}
	m.applySaved(Txn{ID: "t1", Account: Account{ID: "a1"}, AmountCents: centsFromAmount("30"), Classification: "expense"}, true)
	if m.accounts[0].BalanceCents != 10000-3000 {
		t.Fatalf("after create: %d", m.accounts[0].BalanceCents)
	}
	m.applySaved(Txn{ID: "t1", Account: Account{ID: "a1"}, AmountCents: centsFromAmount("50"), Classification: "expense"}, false)
	if m.accounts[0].BalanceCents != 10000-5000 {
		t.Fatalf("after edit: %d", m.accounts[0].BalanceCents)
	}
	m.removeTxn("t1")
	if m.accounts[0].BalanceCents != 10000 {
		t.Fatalf("after delete: %d", m.accounts[0].BalanceCents)
	}
}
