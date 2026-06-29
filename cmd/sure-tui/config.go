package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/x/term"
)

// Config is the on-disk config at $XDG_CONFIG_HOME/sure-tui/config.toml (created
// from a template on first run). Env vars still override their fields, so old
// setups keep working: SURE_API_KEY, SURE_API_URL, SURE_THEME.
type Config struct {
	APIKey string `toml:"api_key"`
	APIURL string `toml:"api_url"`
	Range  string `toml:"range"`
	// InstantUpdate adds new/edited rows straight to the list on success instead
	// of refetching the whole range. Default true (absent key stays true).
	InstantUpdate bool  `toml:"instant_update"`
	// UI picks the pane layout: "classic" (│ separator) or "bordered" (boxed
	// panes, spotify-tui style). Unknown/empty falls back to classic.
	UI string `toml:"ui"`
	// MaxTransactions caps how many rows are downloaded for the range. 0/absent
	// keeps the 5000 default.
	MaxTransactions int   `toml:"max_transactions"`
	Theme           Theme `toml:"theme"`

	path string // where it loaded from, shown in the config window
}

// configPath honors $XDG_CONFIG_HOME, else ~/.config (os.UserConfigDir skips
// XDG on macOS, so we check it ourselves).
func configPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "sure-tui", "config.toml")
}

const configTemplate = `# sure-tui config. Env vars override these when set:
#   SURE_API_KEY, SURE_API_URL, SURE_THEME
api_key = ""
api_url = "https://app.sure.am"
range   = "180"   # transactions to download on start: 90, 180, 365, all
instant_update = true  # add new/edited rows to the list instead of refetching
ui      = "classic"   # pane layout: "classic" (│ separator) or "bordered" (boxed panes)
max_transactions = 5000  # cap on rows downloaded for the range

[theme]            # hex colors; omit any key to keep the built-in default
# header         = "#7aa2f7"
# selection      = "#283457"
# selection_text = "#c0caf5"
# dim            = "#565f89"
# text           = "#c0caf5"
# income         = "#9ece6a"
# expense        = "#f7768e"
# transfer       = "#7dcfff"
# border         = "#7aa2f7"
# separator      = "#3b4261"
`

// loadConfig reads the config, writing a template on first run, then overlays
// any env vars.
func loadConfig() Config {
	c := Config{APIURL: "https://app.sure.am", Range: "180", InstantUpdate: true, UI: "classic", MaxTransactions: 5000, Theme: defaultTheme}
	c.path = configPath()

	if _, err := os.Stat(c.path); os.IsNotExist(err) {
		os.MkdirAll(filepath.Dir(c.path), 0o755)
		if err := os.WriteFile(c.path, []byte(configTemplate), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "config %s: %v\n", c.path, err)
		}
	} else if _, err := toml.DecodeFile(c.path, &c); err != nil {
		fmt.Fprintf(os.Stderr, "config %s: %v\n", c.path, err)
	}

	if v := os.Getenv("SURE_API_KEY"); v != "" {
		c.APIKey = v
	}
	if v := os.Getenv("SURE_API_URL"); v != "" {
		c.APIURL = v
	}
	if path := os.Getenv("SURE_THEME"); path != "" {
		if _, err := toml.DecodeFile(path, &c.Theme); err != nil {
			fmt.Fprintf(os.Stderr, "theme %s: %v\n", path, err)
		}
	}
	return c
}

// ensureConfig runs interactive setup when there's no API key yet (first run) or
// when the user asked to relogin. Non-interactive (piped) runs skip the prompt
// and let NewClient report the missing key.
func ensureConfig(cfg Config, relogin bool) Config {
	if cfg.APIKey != "" && !relogin {
		return cfg
	}
	if !term.IsTerminal(os.Stdin.Fd()) {
		return cfg
	}
	return runSetup(cfg)
}

// runSetup prompts for url/key/range and saves the config.
func runSetup(cfg Config) Config {
	fmt.Println("sure-tui setup — press Enter to keep the value in [brackets].")
	r := bufio.NewReader(os.Stdin)
	abort := func() { fmt.Fprintln(os.Stderr, "\nsetup cancelled"); os.Exit(1) }

	v, ok := prompt(r, "API URL", orDefault(cfg.APIURL, "https://app.sure.am"))
	if !ok {
		abort()
	}
	cfg.APIURL = v

	// ponytail: key is echoed; no hidden input to avoid pulling in a tty-raw dep.
	label := "API key"
	if cfg.APIKey != "" {
		label = "API key (Enter keeps current)"
	}
	for {
		v, ok := prompt(r, label, "")
		if !ok {
			abort()
		}
		if v != "" {
			cfg.APIKey = v
			break
		}
		if cfg.APIKey != "" {
			break // keep existing on relogin
		}
		fmt.Println("  API key is required.")
	}

	for {
		v, ok := prompt(r, "Range 90/180/365/all", orDefault(cfg.Range, "180"))
		if !ok {
			abort()
		}
		if _, valid := rangeDays[v]; valid {
			cfg.Range = v
			break
		}
		fmt.Println("  must be 90, 180, 365, or all")
	}

	if err := saveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save %s: %v\n", cfg.path, err)
	} else {
		fmt.Printf("Saved %s\n\n", cfg.path)
	}
	return cfg
}

// prompt reads one line; ok is false only when input closes (EOF) with nothing
// typed, so a required field can't loop forever on a piped/closed stdin.
func prompt(r *bufio.Reader, label, def string) (val string, ok bool) {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	line, err := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line != "" {
		return line, true
	}
	if err != nil {
		return "", false
	}
	return def, true
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// saveConfig writes the config as TOML (replacing the template comments once the
// user has set real values).
func saveConfig(c Config) error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(c.path)
	if err != nil {
		return err
	}
	defer f.Close()
	f.Chmod(0o600) // best effort: it holds the API key
	return toml.NewEncoder(f).Encode(c)
}
