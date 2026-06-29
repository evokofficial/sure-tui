package main

import (
	"charm.land/lipgloss/v2"
)

// Theme is a set of hex colors loaded from a TOML file. Missing keys keep the
// default below, so partial themes are fine.
type Theme struct {
	Header    string `toml:"header"`
	Selection string `toml:"selection"`
	SelText   string `toml:"selection_text"`
	Dim       string `toml:"dim"`
	Text      string `toml:"text"`
	Income    string `toml:"income"`
	Expense   string `toml:"expense"`
	Transfer  string `toml:"transfer"`
	Border    string `toml:"border"`
	Separator string `toml:"separator"`
}

var defaultTheme = Theme{
	Header:    "#7aa2f7",
	Selection: "#283457",
	SelText:   "#c0caf5",
	Dim:       "#565f89",
	Text:      "#c0caf5",
	Income:    "#9ece6a",
	Expense:   "#f7768e",
	Transfer:  "#7dcfff",
	Border:    "#7aa2f7",
	Separator: "#3b4261",
}

type Styles struct {
	header, sel, dim, text, income, expense, transfer, sep, modal lipgloss.Style
	// Bordered-UI panes: focused gets the border color, blurred the separator color.
	paneFocus, paneBlur lipgloss.Style
}

func newStyles(t Theme) Styles {
	c := lipgloss.Color
	pane := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	return Styles{
		header:   lipgloss.NewStyle().Bold(true).Foreground(c(t.Header)),
		sel:      lipgloss.NewStyle().Background(c(t.Selection)).Foreground(c(t.SelText)),
		dim:      lipgloss.NewStyle().Foreground(c(t.Dim)),
		text:     lipgloss.NewStyle().Foreground(c(t.Text)),
		income:   lipgloss.NewStyle().Foreground(c(t.Income)),
		expense:  lipgloss.NewStyle().Foreground(c(t.Expense)),
		transfer: lipgloss.NewStyle().Foreground(c(t.Transfer)),
		sep:      lipgloss.NewStyle().Foreground(c(t.Separator)),
		modal: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(c(t.Border)).Padding(0, 1),
		paneFocus: pane.BorderForeground(c(t.Border)),
		paneBlur:  pane.BorderForeground(c(t.Separator)),
	}
}

// colored renders text in an element's own hex color, falling back to text.
func (s Styles) colored(hex string) lipgloss.Style {
	if hex == "" {
		return s.text
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
}
