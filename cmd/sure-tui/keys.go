package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// centsFromAmount converts a clean decimal amount string ("100.50") to minor
// units. The entry DSL guarantees a parseable number, so a parse failure yields 0.
func centsFromAmount(s string) int {
	f, _ := strconv.ParseFloat(s, 64)
	return int(math.Round(f * 100))
}

func (m model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.panel == panelDelete {
		m.panel = panelNone
		if s := msg.String(); s == "y" || s == "enter" {
			id := m.delID
			return m, func() tea.Msg {
				if err := m.api.Delete(id); err != nil {
					return errMsg{err}
				}
				return deletedMsg{id}
			}
		}
		return m, nil // any other key cancels
	}
	if m.panel != panelNone {
		m.panel = panelNone // any key dismisses an open overlay
		return m, nil
	}
	if m.focus == focusEntry {
		return m.onEntryKey(msg)
	}
	if m.focus == focusFilter {
		return m.onFilterKey(msg)
	}
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.panel = panelHelp
		return m, nil
	case "c":
		m.panel = panelConfig
		return m, nil
	case "tab":
		if m.focus == focusLeft {
			m.focus = focusRight
		} else {
			m.focus = focusLeft
		}
		return m, nil
	case "/":
		m.focus = focusFilter
		m.filterInput.CursorEnd()
		m.filterInput.Focus()
		return m, nil
	case "r":
		m.loading = true
		return m, tea.Batch(m.fetchTxns(), m.fetchAccounts())
	case "esc":
		m.filterInput.SetValue("")
		clear(m.selAccts)
		clear(m.selCats)
		clear(m.selTags)
		return m, m.applyQuery()
	case "n", "a", "i":
		m.focus = focusEntry
		m.editingID = ""
		m.input.Reset()
		m.sugg = nil
		m.input.Focus()
		return m, nil
	case "up", "k":
		if m.focus == focusLeft && m.leftCursor > 0 {
			m.leftCursor--
		} else if m.focus == focusRight && m.rightCursor > 0 {
			m.rightCursor--
		}
		return m, nil
	case "down", "j":
		if m.focus == focusLeft && m.leftCursor < len(m.leftItems)-1 {
			m.leftCursor++
		} else if m.focus == focusRight && m.rightCursor < len(m.txns)-1 {
			m.rightCursor++
		}
		return m, nil
	case "ctrl+u":
		m.moveCursor(-max(m.height/2, 1))
		return m, nil
	case "ctrl+d":
		m.moveCursor(max(m.height/2, 1))
		return m, nil
	case "]", "right", "l":
		m.moveCursor(max(m.height/2, 1))
		return m, nil
	case "[", "left", "h":
		m.moveCursor(-max(m.height/2, 1))
		return m, nil
	case "x":
		if m.focus == focusLeft {
			m.toggleLeft(fExclude)
			return m, nil
		}
		return m, nil
	case "enter":
		if m.focus == focusLeft {
			m.toggleLeft(fInclude)
			return m, nil
		}
		m.editSelected()
		return m, nil
	case "e":
		m.editSelected()
		return m, nil
	case "d":
		if m.focus == focusRight && m.rightCursor < len(m.txns) {
			t := m.txns[m.rightCursor]
			m.delID, m.delName = t.ID, t.Name
			m.panel = panelDelete
		}
		return m, nil
	case "u":
		// Live-switch the pane layout and persist it.
		if m.cfg.UI == "bordered" {
			m.cfg.UI = "classic"
		} else {
			m.cfg.UI = "bordered"
		}
		if err := saveConfig(m.cfg); err != nil {
			m.status = "✗ " + err.Error()
		}
		return m, nil
	}
	return m, nil
}

// moveCursor jumps the focused list's cursor by n, clamped to bounds.
func (m *model) moveCursor(n int) {
	if m.focus == focusLeft {
		m.leftCursor = clamp(m.leftCursor+n, 0, len(m.leftItems)-1)
	} else if m.focus == focusRight {
		m.rightCursor = clamp(m.rightCursor+n, 0, len(m.txns)-1)
	}
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }

// toggleLeft flips the selected sidebar item between fNone and target (enter ->
// include, shift+enter -> exclude). Keying on ID sidesteps the spaces/()/,
// escaping the old text-token round-trip needed.
func (m *model) toggleLeft(target filterState) {
	if m.leftCursor >= len(m.leftItems) {
		return
	}
	it := m.leftItems[m.leftCursor]
	var set map[string]filterState
	switch it.kind {
	case "account":
		set = m.selAccts
	case "category":
		set = m.selCats
	case "tag":
		set = m.selTags
	default:
		return
	}
	if set[it.id] == target {
		delete(set, it.id)
	} else {
		set[it.id] = target
	}
	m.rightCursor = 0
	m.refilter()
}

func (m model) onFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		m.filterInput.Blur()
		m.focus = focusRight
		return m, m.applyQuery() // fetch on commit, not per keystroke
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	return m, cmd
}

// editSelected opens the entry form for the highlighted right-pane transaction.
func (m *model) editSelected() {
	if m.focus != focusRight || m.rightCursor >= len(m.txns) {
		return
	}
	m.loadForEdit(m.txns[m.rightCursor])
	m.focus = focusEntry
	m.input.Focus()
}

func (m *model) loadForEdit(t Txn) {
	m.editingID = t.ID
	sign := "-"
	if t.Classification == "income" {
		sign = "+"
	}
	// t.Amount is a currency-formatted display string ("-100,000.00 so'm"); the
	// entry DSL needs a bare number, so rebuild it from cents.
	amt := sign + fmt.Sprintf("%.2f", math.Abs(t.Amountf()))
	if t.Currency != "" {
		amt += t.Currency // "-100.00USD" round-trips through amtCurRe
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s) %s %s", t.Date, t.Account.Name, amt, t.Name)
	if t.Category != nil {
		fmt.Fprintf(&b, " +%s", t.Category.Name)
	}
	for _, tag := range t.Tags {
		fmt.Fprintf(&b, " @%s", tag.Name)
	}
	m.input.SetValue(b.String())
	m.input.CursorEnd()
	m.sugg = nil
}

func (m model) onEntryKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.focus = focusLeft
		m.editingID = ""
		m.input.Blur()
		m.input.Reset()
		return m, nil
	case "tab", "shift+tab":
		if m.sugg == nil { // first tab: freeze the candidates and the base line
			m.sugg = Suggest(m.input.Value(), m.accounts, m.categories, m.tags)
			m.suggBase = m.input.Value()
			m.suggIdx = 0
		} else if msg.String() == "tab" {
			m.suggIdx = (m.suggIdx + 1) % len(m.sugg)
		} else {
			m.suggIdx = (m.suggIdx - 1 + len(m.sugg)) % len(m.sugg)
		}
		if len(m.sugg) > 0 {
			m.input.SetValue(completeToken(m.suggBase, m.sugg[m.suggIdx]))
			m.input.CursorEnd()
		}
		return m, nil
	case "enter":
		req, err := Parse(m.input.Value(), m.accounts, m.categories, m.tags)
		if err != nil {
			m.status = "✗ " + err.Error()
			return m, nil
		}
		id, edit := m.editingID, m.editingID != ""
		api := m.api
		cmd := func() tea.Msg {
			var (
				t Txn
				e error
			)
			if edit {
				t, e = api.Update(id, req)
			} else {
				t, e = api.Create(req)
			}
			if e != nil {
				return errMsg{e}
			}
			// The API response omits amount_cents/classification; backfill them
			// from the request so totals and the local balance delta are correct.
			t.AmountCents = centsFromAmount(req.Amount)
			if t.Classification == "" {
				t.Classification = req.Nature
			}
			return savedMsg{txn: t, created: !edit}
		}
		m.focus = focusLeft
		m.editingID = ""
		m.input.Blur()
		m.input.Reset()
		return m, cmd
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.sugg = nil // typing invalidates the frozen completion list
	return m, cmd
}

// completeToken replaces the trailing token (or open-paren content) with full.
func completeToken(line, full string) string {
	if i := strings.LastIndex(line, "("); i >= 0 && !strings.Contains(line[i:], ")") {
		return line[:i] + "(" + strings.TrimPrefix(full, "(") + ")"
	}
	if i := strings.LastIndex(line, " "); i >= 0 {
		return line[:i+1] + full
	}
	return full
}
