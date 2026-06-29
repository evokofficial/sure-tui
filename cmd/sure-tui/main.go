package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"sort"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type focus int

const (
	focusLeft focus = iota
	focusRight
	focusFilter
	focusEntry
)

// panel is a full-screen-ish overlay toggled on top of the lists.
type panel int

const (
	panelNone panel = iota
	panelHelp
	panelConfig
	panelDelete // confirm before deleting the selected transaction
)

// filterState is a sidebar item's tri-state: not filtered, must-match, or
// must-not-match.
type filterState int

const (
	fNone filterState = iota
	fInclude
	fExclude
)

type leftItem struct {
	kind  string // account | category | tag
	id    string
	name  string
	color string // hex, for categories/tags
}

type model struct {
	api *Client
	st  Styles
	cfg Config

	panel panel // active overlay (help/config), panelNone when none

	accounts   []Account
	categories []Category
	tags       []Tag
	allTxns    []Txn // master set downloaded for the range; filtered client-side
	txns       []Txn // allTxns after the current query
	leftItems  []leftItem

	rangeDays int // 0 = all-time; how far back to download
	loading   bool

	leftCursor  int
	rightCursor int
	focus       focus

	query       Query
	filterInput textinput.Model

	// Sidebar-driven filters, keyed by ID so names with spaces/()/, can't break
	// the round-trip the way a text token would. Each item is tri-state: none /
	// include / exclude. Includes OR within a kind, excludes always reject; kinds
	// are ANDed. Absent key == fNone.
	selAccts map[string]filterState
	selCats  map[string]filterState
	selTags  map[string]filterState

	input     textinput.Model
	editingID string
	delID     string // transaction awaiting delete confirmation (panelDelete)
	delName   string
	status    string

	// Completion cycling: candidates frozen on the first tab and the line they
	// complete against, so repeated tab/shift+tab rotate options instead of
	// re-suggesting against an already-completed token. Reset on any typing.
	sugg     []string
	suggBase string
	suggIdx  int

	width, height int
}

// --- messages & commands ---

type acctsMsg []Account
type catsMsg []Category
type tagsMsg []Tag
// txnsPageMsg is one page of the range download; pages stream in so rows appear
// as they arrive instead of after the whole range finishes.
type txnsPageMsg struct {
	txns        []Txn
	page, total int
}
type errMsg struct{ error }

// savedMsg carries the transaction the API returned after a create/edit so it
// can be spliced into the list; deletedMsg carries the removed id.
type savedMsg struct {
	txn     Txn
	created bool
}
type deletedMsg struct{ id string }

// fetchAccounts (re)loads the accounts list, which carries the balances.
func (m model) fetchAccounts() tea.Cmd {
	c := m.api
	return func() tea.Msg {
		a, err := c.Accounts()
		if err != nil {
			return errMsg{err}
		}
		return acctsMsg(a)
	}
}

// fetchTxns starts the range download at page 1; each page chains the next via
// txnsPageMsg, so the list fills in progressively.
func (m model) fetchTxns() tea.Cmd { return m.fetchTxnsPage(1) }

func (m model) fetchTxnsPage(page int) tea.Cmd {
	api, days := m.api, m.rangeDays
	v := url.Values{}
	if days > 0 {
		v.Set("start_date", time.Now().AddDate(0, 0, -days).Format(dateFmt))
	}
	return func() tea.Msg {
		t, total, err := api.Transactions(v, page)
		if err != nil {
			return errMsg{err}
		}
		return txnsPageMsg{txns: t, page: page, total: total}
	}
}

// applyQuery reparses the filter input and re-filters in memory. No refetch.
func (m *model) applyQuery() tea.Cmd {
	m.query = ParseQuery(m.filterInput.Value())
	m.rightCursor = 0
	m.refilter()
	return nil
}

// refilter recomputes m.txns from the downloaded master set: the typed query
// first, then the ID-based sidebar selection.
func (m *model) refilter() {
	m.txns = m.txns[:0]
	for _, t := range filterTxns(m.allTxns, m.query) {
		if m.selMatch(t) {
			m.txns = append(m.txns, t)
		}
	}
	if m.rightCursor >= len(m.txns) {
		m.rightCursor = 0
	}
}

// applySaved splices a created/updated transaction into the master set and
// re-applies the current filter, no refetch. Re-sorts so the row lands in date
// order. The cached account balance is adjusted locally because the server
// recomputes balances asynchronously — an immediate refetch reads the stale,
// pre-change value.
func (m *model) applySaved(t Txn, created bool) {
	replaced := false
	for i := range m.allTxns {
		if m.allTxns[i].ID == t.ID {
			m.adjustBalance(t.Account.ID, balanceDeltaCents(t)-balanceDeltaCents(m.allTxns[i]))
			m.allTxns[i] = t
			replaced = true
			break
		}
	}
	if !replaced && created {
		m.adjustBalance(t.Account.ID, balanceDeltaCents(t))
		m.allTxns = append(m.allTxns, t)
	}
	sort.SliceStable(m.allTxns, func(i, j int) bool { return m.allTxns[i].Date > m.allTxns[j].Date })
	m.refilter()
}

// removeTxn drops a deleted transaction from the master set and re-filters,
// undoing its effect on the cached account balance.
func (m *model) removeTxn(id string) {
	out := m.allTxns[:0]
	for _, t := range m.allTxns {
		if t.ID == id {
			m.adjustBalance(t.Account.ID, -balanceDeltaCents(t))
			continue
		}
		out = append(out, t)
	}
	m.allTxns = out
	m.refilter()
}

// balanceDeltaCents is a transaction's effect on its account balance: income
// raises it, expense lowers it. Transfers are skipped (the DSL can't make them).
// Relies on AmountCents being set — list rows carry it, and the create/update
// command backfills it from the request since the API response omits it.
func balanceDeltaCents(t Txn) int {
	if t.IsTransfer() {
		return 0
	}
	a := t.AmountCents
	if a < 0 {
		a = -a
	}
	if t.Classification == "income" {
		return a
	}
	return -a
}

// adjustBalance nudges a cached account balance by deltaCents.
func (m *model) adjustBalance(acctID string, deltaCents int) {
	for i := range m.accounts {
		if m.accounts[i].ID == acctID {
			m.accounts[i].BalanceCents += deltaCents
			return
		}
	}
}

// selMatch applies the sidebar ID filters: includes OR within a kind, excludes
// always reject, kinds ANDed together.
func (m *model) selMatch(t Txn) bool {
	catID := ""
	if t.Category != nil {
		catID = t.Category.ID
	}
	if !passOne(m.selAccts, t.Account.ID) || !passOne(m.selCats, catID) {
		return false
	}
	return passTags(m.selTags, t.Tags)
}

// passOne checks one id against a tri-state set: excluded ids fail; if the set
// has any include, the id must be one of them.
// ponytail: rescans for includes each call; the sidebar set is tiny.
func passOne(s map[string]filterState, id string) bool {
	if s[id] == fExclude {
		return false
	}
	return s[id] == fInclude || !hasInclude(s)
}

// passTags handles the many-tags case: any excluded tag rejects; if any include
// exists, at least one tag must be included.
func passTags(s map[string]filterState, tags []Tag) bool {
	matched := false
	for _, tg := range tags {
		switch s[tg.ID] {
		case fExclude:
			return false
		case fInclude:
			matched = true
		}
	}
	return matched || !hasInclude(s)
}

func hasInclude(s map[string]filterState) bool {
	for _, v := range s {
		if v == fInclude {
			return true
		}
	}
	return false
}

// selState returns a sidebar item's tri-state.
func (m model) selState(it leftItem) filterState {
	switch it.kind {
	case "account":
		return m.selAccts[it.id]
	case "category":
		return m.selCats[it.id]
	case "tag":
		return m.selTags[it.id]
	}
	return fNone
}

// rangeDays maps the -range flag to a lookback in days (0 = all-time).
var rangeDays = map[string]int{"90": 90, "180": 180, "365": 365, "all": 0}

func main() {
	cfg := loadConfig()
	rng := flag.String("range", "", "transactions to download on start: 90, 180, 365, all (default from config)")
	flag.Parse()

	// First run (no api key) or `sure-tui relogin` => interactive setup.
	cfg = ensureConfig(cfg, flag.Arg(0) == "relogin")

	if *rng != "" {
		cfg.Range = *rng
	}
	days, ok := rangeDays[cfg.Range]
	if !ok {
		fmt.Fprintf(os.Stderr, "invalid range %q (want 90, 180, 365, all)\n", cfg.Range)
		os.Exit(2)
	}

	c := NewClient(cfg)
	ti := textinput.New()
	ti.Placeholder = "2026-06-29 (Account) +20.00 USD name +category @tag"
	ti.SetVirtualCursor(true)

	fi := textinput.New()
	fi.Placeholder = "month, july, income, expense, transfer, +Grocery, @Travel, (Cash), name  (or from:/to: dates)"
	fi.SetVirtualCursor(true)

	m := model{api: c, st: newStyles(cfg.Theme), cfg: cfg, focus: focusLeft, input: ti, filterInput: fi, rangeDays: days, loading: true,
		selAccts: map[string]filterState{}, selCats: map[string]filterState{}, selTags: map[string]filterState{}}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func (m model) Init() tea.Cmd {
	c := m.api
	return tea.Batch(
		m.fetchAccounts(),
		func() tea.Msg {
			a, err := c.Categories()
			if err != nil {
				return errMsg{err}
			}
			return catsMsg(a)
		},
		func() tea.Msg {
			a, err := c.Tags()
			if err != nil {
				return errMsg{err}
			}
			return tagsMsg(a)
		},
		m.fetchTxns(),
	)
}

func (m *model) rebuildLeft() {
	m.leftItems = m.leftItems[:0]
	for _, a := range m.accounts {
		m.leftItems = append(m.leftItems, leftItem{kind: "account", id: a.ID, name: a.Name})
	}
	for _, c := range m.categories {
		m.leftItems = append(m.leftItems, leftItem{"category", c.ID, c.Name, c.Color})
	}
	for _, t := range m.tags {
		m.leftItems = append(m.leftItems, leftItem{"tag", t.ID, t.Name, t.Color})
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(msg.Width - 4)
		m.filterInput.SetWidth(msg.Width - 4)
		return m, nil
	case acctsMsg:
		m.accounts = msg
		m.rebuildLeft()
		return m, nil
	case catsMsg:
		m.categories = msg
		m.rebuildLeft()
		return m, nil
	case tagsMsg:
		m.tags = msg
		m.rebuildLeft()
		return m, nil
	case txnsPageMsg:
		if msg.page == 1 {
			m.allTxns = msg.txns // first page replaces the old set
		} else {
			m.allTxns = append(m.allTxns, msg.txns...)
		}
		sort.SliceStable(m.allTxns, func(i, j int) bool { return m.allTxns[i].Date > m.allTxns[j].Date })
		m.refilter()
		// Keep paging until the server runs out or we hit the configured cap.
		if msg.page < msg.total && msg.page < max((m.api.maxTxns+99)/100, 1) {
			return m, m.fetchTxnsPage(msg.page + 1)
		}
		m.loading = false
		return m, nil
	case errMsg:
		m.status = "✗ " + msg.Error()
		return m, nil
	case savedMsg:
		if msg.created {
			m.status = "✓ created"
		} else {
			m.status = "✓ updated"
		}
		if !m.cfg.InstantUpdate {
			m.loading = true
			return m, tea.Batch(m.fetchTxns(), m.fetchAccounts())
		}
		m.applySaved(msg.txn, msg.created)
		return m, nil // balance adjusted locally; server recomputes async
	case deletedMsg:
		m.status = "✓ deleted"
		if !m.cfg.InstantUpdate {
			m.loading = true
			return m, tea.Batch(m.fetchTxns(), m.fetchAccounts())
		}
		m.removeTxn(msg.id)
		return m, nil // balance adjusted locally; server recomputes async
	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	return m, nil
}
