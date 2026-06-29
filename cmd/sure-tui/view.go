package main

import (
	"fmt"
	"math"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var paneStyle = lipgloss.NewStyle().Padding(0, 1)

// currencySymbol maps the few currency codes we see to a symbol; unknown codes
// fall back to the code itself.
// ponytail: hardcoded map, add codes as they show up.
func currencySymbol(code string) string {
	return code
}

// plural pluralizes an English noun (handles the "y"->"ies" case we hit: Category).
func plural(s string) string {
	if strings.HasSuffix(s, "y") {
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
}

// comma formats f to 2 decimals with thousands separators: 1234567.5 -> "1,234,567.50".
func comma(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	intp, frac, _ := strings.Cut(s, ".")
	neg := strings.HasPrefix(intp, "-")
	intp = strings.TrimPrefix(intp, "-")
	var b strings.Builder
	for i, c := range intp {
		if i > 0 && (len(intp)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String() + "." + frac
	if neg {
		out = "-" + out
	}
	return out
}

// cell truncates s to display width w and right-pads it to exactly w.
func cell(s string, w int) string {
	if lipgloss.Width(s) > w {
		r := []rune(s)
		for lipgloss.Width(string(r)) > w-1 && len(r) > 0 {
			r = r[:len(r)-1]
		}
		s = string(r) + "…"
	}
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func (m model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("loading…")
	}
	bodyH := m.height - 3 // filter bar + status + help

	body := m.classicBody(bodyH)
	if m.cfg.UI == "bordered" {
		body = m.borderedBody(bodyH)
	}

	help := m.st.dim.Render("  tab focus · j/k move · / filter · n new · d delete · u ui · r reload · c config · ? help · q quit")

	// Filter bar: editable input while focused, otherwise a summary of the active
	// typed query plus the sidebar ID selections (display-only, so spaces/() are
	// harmless here).
	var filterBar string
	if m.focus == focusFilter {
		filterBar = "  " + m.filterInput.View()
	} else if s := m.filterSummary(); s != "" {
		filterBar = "  " + s
	} else {
		filterBar = "  " + m.st.dim.Render("/ to filter")
	}

	status := m.status
	if m.loading {
		span := "all-time"
		if m.rangeDays > 0 {
			span = fmt.Sprintf("%dd", m.rangeDays)
		}
		status = m.st.header.Render("⟳ downloading " + span + "…")
	}
	out := lipgloss.JoinVertical(lipgloss.Left, filterBar, body, "  "+status, help)

	// Centered modal overlays.
	switch {
	case m.focus == focusEntry:
		out = m.overlay(out)
	case m.panel == panelHelp:
		out = m.center(out, m.helpBox())
	case m.panel == panelConfig:
		out = m.center(out, m.configBox())
	case m.panel == panelDelete:
		out = m.center(out, m.deleteBox())
	}

	v := tea.NewView(out)
	v.AltScreen = true
	return v
}

// classicBody is the default layout: two padded panes split by a │ separator.
func (m model) classicBody(bodyH int) string {
	leftW := 30
	rightW := max(m.width-leftW-5, 20)
	left := paneStyle.Width(leftW).Height(bodyH).Render(m.leftView(leftW - 2)) // -padding
	right := paneStyle.Width(rightW).Height(bodyH).Render(m.rightView(rightW, bodyH))
	sep := m.st.sep.Render(strings.TrimRight(strings.Repeat("│\n", bodyH), "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
}

// borderedBody is the spotify-tui-style layout: each pane in a rounded box, the
// focused one highlighted. Border adds 2 to each box's width/height, so content
// sizes are reduced to keep the same total footprint as the classic layout.
func (m model) borderedBody(bodyH int) string {
	const leftBoxW = 30
	innerH := max(bodyH-2, 1) // border eats the top+bottom rows
	lst, rst := m.st.paneBlur, m.st.paneBlur
	switch m.focus {
	case focusLeft:
		lst = m.st.paneFocus
	case focusRight:
		rst = m.st.paneFocus
	}
	// Width/Height on a bordered style are the *total* box size, so the panes
	// sum to m.width. rightView still gets border-subtracted dims (matching the
	// content width/height it expects in the classic layout).
	rightBoxW := max(m.width-leftBoxW, 22)
	left := lst.Width(leftBoxW).Height(bodyH).Render(m.leftView(leftBoxW - 4)) // -border-padding
	right := rst.Width(rightBoxW).Height(bodyH).Render(m.rightView(rightBoxW-2, innerH))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

func (m model) overlay(base string) string {
	title := " New transaction "
	if m.editingID != "" {
		title = " Edit transaction "
	}
	w := min(m.width-20, 70)
	m.input.SetWidth(w - 4)
	content := m.input.View()
	// Show frozen candidates (cycling with tab) when present, else a live preview.
	sugg, active := m.sugg, m.suggIdx
	if sugg == nil {
		sugg, active = Suggest(m.input.Value(), m.accounts, m.categories, m.tags), -1
	}
	if len(sugg) > 0 {
		parts := make([]string, len(sugg))
		for i, s := range sugg {
			if i == active {
				parts[i] = m.st.sel.Render(s)
			} else {
				parts[i] = m.st.dim.Render(s)
			}
		}
		content += "\n" + m.st.dim.Render("tab ") + strings.Join(parts, "  ")
	}
	// Live parse feedback: enter only saves once this is OK, so the error stays
	// visible (instead of behind the modal) until it's fixed or esc cancels.
	if strings.TrimSpace(m.input.Value()) != "" {
		if req, err := Parse(m.input.Value(), m.accounts, m.categories, m.tags); err != nil {
			content += "\n" + m.st.expense.Render("✗ "+err.Error())
		} else {
			content += "\n" + m.st.income.Render("✓ ready") + "\n" + m.interpret(req)
		}
	}
	content += "\n" + m.st.dim.Render("enter: save · esc: cancel")
	box := m.st.modal.Width(w).Render(m.st.header.Render(title) + "\n" + content)
	return m.center(base, box)
}

// interpret renders how the parsed entry is understood, so the user can confirm
// the account/amount/currency/category/tags before saving.
func (m model) interpret(req TxnReq) string {
	amtSt := m.st.expense
	sign := "-"
	if req.Nature == "income" {
		amtSt, sign = m.st.income, "+"
	}
	acct, _ := m.account(req.AccountID)
	rows := [][2]string{
		{"date", req.Date},
		{"account", acct.Name},
		{"amount", ""}, // styled below
		{"name", req.Name},
	}
	if catID := req.CategoryID; catID != "" {
		for _, c := range m.categories {
			if c.ID == catID {
				rows = append(rows, [2]string{"category", c.Name})
			}
		}
	}
	if len(req.TagIDs) > 0 {
		var names []string
		for _, id := range req.TagIDs {
			for _, t := range m.tags {
				if t.ID == id {
					names = append(names, t.Name)
				}
			}
		}
		rows = append(rows, [2]string{"tags", strings.Join(names, ", ")})
	}
	var b strings.Builder
	for _, r := range rows {
		val := m.st.text.Render(r[1])
		if r[0] == "amount" {
			val = amtSt.Render(sign+req.Amount) + " " + m.st.dim.Render(req.Currency)
		}
		fmt.Fprintf(&b, "%s %s\n", m.st.dim.Render(cell(r[0], 8)), val)
	}
	return strings.TrimRight(b.String(), "\n")
}

// center composites box centered over base.
func (m model) center(base, box string) string {
	x := (m.width - lipgloss.Width(box)) / 2
	y := (m.height - lipgloss.Height(box)) / 2
	c := lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	)
	return c.Render()
}

// helpBox renders the key reference shown by "?".
func (m model) helpBox() string {
	rows := [][2]string{
		{"j / k  ↑ / ↓", "move cursor"},
		{"[ ] / h l", "jump half-page"},
		{"ctrl+u / ctrl+d", "half-page up / down"},
		{"tab", "switch pane"},
		{"enter / e", "edit txn · ●include filter"},
		{"x", "✕exclude filter"},
		{"/", "filter"},
		{"esc", "clear filter"},
		{"n / a / i", "new transaction"},
		{"d", "delete transaction"},
		{"u", "switch ui layout"},
		{"r", "reload"},
		{"c", "config"},
		{"?", "this help"},
		{"q / ctrl+c", "quit"},
	}
	var b strings.Builder
	b.WriteString(m.st.header.Render(" Keys "))
	b.WriteString("\n\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%s  %s\n", m.st.text.Render(cell(r[0], 16)), m.st.dim.Render(r[1]))
	}
	b.WriteString("\n" + m.st.dim.Render("any key to close"))
	return m.st.modal.Render(b.String())
}

// configBox shows the resolved config and where it lives.
func (m model) configBox() string {
	key := m.st.dim.Render("(unset)")
	if m.cfg.APIKey != "" {
		key = maskKey(m.cfg.APIKey)
	}
	rows := [][2]string{
		{"file", m.cfg.path},
		{"api_url", m.cfg.APIURL},
		{"api_key", key},
		{"range", m.cfg.Range},
		{"instant", fmt.Sprintf("%t", m.cfg.InstantUpdate)},
		{"ui", m.cfg.UI},
		{"max txns", fmt.Sprintf("%d", m.cfg.MaxTransactions)},
	}
	var b strings.Builder
	b.WriteString(m.st.header.Render(" Config "))
	b.WriteString("\n\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%s  %s\n", m.st.dim.Render(cell(r[0], 8)), m.st.text.Render(r[1]))
	}
	b.WriteString("\n" + m.st.dim.Render("edit the file above · any key to close"))
	return m.st.modal.Render(b.String())
}

// deleteBox confirms deletion of the selected transaction.
func (m model) deleteBox() string {
	body := m.st.header.Render(" Delete transaction ") + "\n\n" +
		m.st.text.Render("Delete ") + m.st.expense.Render(m.delName) + m.st.text.Render("?") +
		"\n\n" + m.st.dim.Render("y: delete · any other key: cancel")
	return m.st.modal.Render(body)
}

// maskKey shows only the last 4 chars of a secret.
func maskKey(s string) string {
	if len(s) <= 4 {
		return "••••"
	}
	return "••••" + s[len(s)-4:]
}

// filterSummary is a styled line of the active filters for the bar: includes in
// green, excludes in red with a leading "-".
func (m model) filterSummary() string {
	var parts []string
	for _, it := range m.leftItems {
		st := m.selState(it)
		if st == fNone {
			continue
		}
		tok := "(" + it.name + ")"
		switch it.kind {
		case "category":
			tok = "+" + it.name
		case "tag":
			tok = "@" + it.name
		}
		if st == fExclude {
			parts = append(parts, m.st.expense.Render("-"+tok))
		} else {
			parts = append(parts, m.st.income.Render(tok))
		}
	}
	if m.query.raw != "" {
		parts = append(parts, m.st.header.Render(m.query.raw))
	}
	return strings.Join(parts, "  ")
}

// account looks up a loaded account by id.
func (m model) account(id string) (Account, bool) {
	for _, a := range m.accounts {
		if a.ID == id {
			return a, true
		}
	}
	return Account{}, false
}

func (m model) leftView(_ int) string {
	type sums struct{ inc, exp float64 }
	byCur := map[string]*sums{}
	var curs []string
	for _, t := range m.txns {
		if t.IsTransfer() {
			// ponytail: transfers net to zero, left out of income/expense totals.
			continue
		}
		s := byCur[t.Currency]
		if s == nil {
			s = &sums{}
			byCur[t.Currency] = s
			curs = append(curs, t.Currency)
		}
		if t.Classification == "income" {
			s.inc += math.Abs(t.Amountf())
		} else {
			s.exp += math.Abs(t.Amountf())
		}
	}
	sort.Strings(curs)
	var b strings.Builder
	for _, cur := range curs {
		s := byCur[cur]
		fmt.Fprintf(&b, "%s\n  %s\n  %s\n", m.st.header.Render(currencySymbol(cur)+":"),
			m.st.income.Render("+"+comma(s.inc)), m.st.expense.Render("-"+comma(s.exp)))
	}
	fmt.Fprintf(&b, "%s\n\n", m.st.dim.Render(fmt.Sprintf("%d transactions", len(m.txns))))

	last := ""
	for i, it := range m.leftItems {
		if it.kind != last {
			b.WriteString(m.st.header.Render(plural(strings.ToUpper(it.kind[:1]) + it.kind[1:])))
			b.WriteByte('\n')
			last = it.kind
		}
		name := m.st.colored(it.color).Render(it.name) // accounts have no color -> text
		mark, ch := "  ", "  " // ch: plain marker for the highlighted cursor row
		switch m.selState(it) {
		case fInclude:
			mark, ch = m.st.income.Render("●")+" ", "● "
		case fExclude:
			mark, ch = m.st.expense.Render("✕")+" ", "✕ "
		}
		line := mark + name
		if m.focus == focusLeft && i == m.leftCursor {
			line = m.st.sel.Render(ch + it.name)
		}
		b.WriteString(line)
		b.WriteString("\n")
		// Accounts: balance on its own (indented) line so it never wraps.
		if a, ok := m.account(it.id); ok {
			st := m.st.income
			if a.BalanceCents < 0 {
				st = m.st.expense
			}
			bal := st.Render(comma(float64(a.BalanceCents)/100)) + " " + m.st.dim.Render(a.Currency)
			b.WriteString("    " + bal + "\n")
		}
	}
	return b.String()
}

func (m model) rightView(w, h int) string {
	if len(m.txns) == 0 {
		return m.st.dim.Render("no transactions")
	}
	inc, exp := dayStats(m.txns)
	// Column widths from the data; the 2-char cursor prefix is added per row, so
	// inner rows are built to rowW = content width - 2.
	nameW, acctW := 0, 0
	for _, t := range m.txns {
		nameW = max(nameW, lipgloss.Width(t.Name))
		acctW = max(acctW, lipgloss.Width(t.Account.Name))
	}
	nameW, acctW = min(nameW, 40), min(acctW, 14)
	rowW := max(
		// pane padding + cursor prefix
		w-2-2, 10,
	)

	var lines []string
	selLine := 0
	lastDate := ""
	for i, t := range m.txns {
		if t.Date != lastDate {
			if lastDate != "" {
				lines = append(lines, "")
			}
			// Date on the left, the day's +income -expense on the right.
			right := ""
			if v := inc[t.Date]; v != 0 {
				right += m.st.income.Render("+" + comma(v))
			}
			if v := exp[t.Date]; v != 0 {
				if right != "" {
					right += " "
				}
				right += m.st.expense.Render("-" + comma(v))
			}
			gap := max(rowW-lipgloss.Width(t.Date)-lipgloss.Width(right), 1)
			lines = append(lines, m.st.header.Render(t.Date)+strings.Repeat(" ", gap)+right)
			lastDate = t.Date
		}
		// amount: API signs incomes negative; show our convention (+income/-expense).
		// transfers keep the API's own sign (− leaving, + arriving) in a neutral color.
		var amt string
		switch {
		case t.IsTransfer():
			amt = m.st.transfer.Render(strings.TrimLeft(t.Amount, "+-"))
		case t.Classification == "income":
			amt = m.st.income.Render("+" + strings.TrimLeft(t.Amount, "+-"))
		default:
			amt = m.st.expense.Render("-" + strings.TrimLeft(t.Amount, "+-"))
		}

		cat := ""
		switch {
		case t.IsTransfer():
			// arrow shows flow: "→" money leaving this account, "←" arriving.
			arrow := "←"
			if strings.HasPrefix(t.Amount, "-") {
				arrow = "→"
			}
			other := "transfer"
			if t.Transfer.OtherAccount != nil {
				other = arrow + " " + t.Transfer.OtherAccount.Name
			}
			cat = m.st.transfer.Render(other)
		case t.Category != nil:
			cat = m.st.colored(t.Category.Color).Render("+" + t.Category.Name)
		}
		left := cell(t.Name, nameW) + " " + m.st.dim.Render(cell(t.Account.Name, acctW)) + " " + cat
		gap := max(rowW-lipgloss.Width(left)-lipgloss.Width(amt), 1)
		row := left + strings.Repeat(" ", gap) + amt

		prefix := "  "
		if i == m.rightCursor {
			selLine = len(lines)
			prefix = "▸ "
		}
		row = prefix + row
		if i == m.rightCursor && m.focus == focusRight {
			row = m.st.sel.Render(row)
		}
		lines = append(lines, row)
	}
	// Window the lines so the selected row stays roughly centered: scrolling up
	// then shows context above and below instead of pinning the cursor to the
	// bottom edge.
	if h < 1 {
		h = 1
	}
	start := clamp(selLine-h/2, 0, max(len(lines)-h, 0))
	end := min(start+h, len(lines))
	return strings.Join(lines[start:end], "\n")
}
