package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Client struct {
	key, base string
	maxTxns   int // cap on rows AllTransactions downloads
	http      *http.Client
}

func NewClient(cfg Config) *Client {
	if cfg.APIKey == "" {
		fmt.Fprintln(os.Stderr, "api_key is required (set it in the config file or SURE_API_KEY)")
		os.Exit(1)
	}
	base := cfg.APIURL
	if base == "" {
		base = "https://app.sure.am"
	}
	maxTxns := cfg.MaxTransactions
	if maxTxns <= 0 {
		maxTxns = 5000
	}
	return &Client{key: cfg.APIKey, base: base, maxTxns: maxTxns, http: &http.Client{Timeout: 20 * time.Second}}
}

type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// BalanceCents is the signed balance in minor units; only the accounts list
	// endpoint populates it (the account embedded in a Txn won't).
	BalanceCents int    `json:"balance_cents"`
	Currency     string `json:"currency"`
}

type Category struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Tag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Transfer struct {
	OtherAccount *Account `json:"other_account"`
}

type Txn struct {
	ID             string    `json:"id"`
	Date           string    `json:"date"`
	Amount         string    `json:"amount"`
	AmountCents    int       `json:"amount_cents"`
	Currency       string    `json:"currency"`
	Name           string    `json:"name"`
	Classification string    `json:"classification"`
	Account        Account   `json:"account"`
	Category       *Category `json:"category"`
	Tags           []Tag     `json:"tags"`
	Transfer       *Transfer `json:"transfer"`
}

// IsTransfer reports whether this txn is one side of a transfer. The API still
// classifies each side as income/expense, so the transfer object is the tell.
func (t Txn) IsTransfer() bool { return t.Transfer != nil }

// Amountf returns the signed amount in major units. Uses amount_cents because
// the amount string is currency-formatted and won't ParseFloat cleanly.
func (t Txn) Amountf() float64 {
	return float64(t.AmountCents) / 100
}

// TxnReq is the create/update body (account_id omitted on update).
type TxnReq struct {
	AccountID  string   `json:"account_id,omitempty"`
	Date       string   `json:"date,omitempty"`
	Amount     string   `json:"amount,omitempty"`
	Currency   string   `json:"currency,omitempty"`
	Name       string   `json:"name,omitempty"`
	Nature     string   `json:"nature,omitempty"`
	CategoryID string   `json:"category_id,omitempty"`
	TagIDs     []string `json:"tag_ids,omitempty"`
}

type pagination struct {
	Page       int `json:"page"`
	TotalPages int `json:"total_pages"`
}

func (c *Client) do(method, path string, q url.Values, body, out any) error {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", c.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s %s: reading response: %w", method, path, err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, truncate(string(data), 200))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s %s: bad response: %w: %s", method, path, err, truncate(string(data), 200))
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (c *Client) Accounts() ([]Account, error) {
	var r struct {
		Accounts []Account `json:"accounts"`
	}
	q := url.Values{"per_page": {"100"}}
	return r.Accounts, c.do("GET", "/api/v1/accounts", q, nil, &r)
}

func (c *Client) Categories() ([]Category, error) {
	var r struct {
		Categories []Category `json:"categories"`
	}
	q := url.Values{"per_page": {"100"}}
	return r.Categories, c.do("GET", "/api/v1/categories", q, nil, &r)
}

func (c *Client) Tags() ([]Tag, error) {
	var r []Tag
	return r, c.do("GET", "/api/v1/tags", nil, nil, &r)
}

// Transactions fetches a single page (100 rows) matching the server-side
// filter params (start_date, end_date, type, search, *_ids[]). Returns the
// page's rows and the total page count for navigation.
func (c *Client) Transactions(filter url.Values, page int) ([]Txn, int, error) {
	var r struct {
		Transactions []Txn      `json:"transactions"`
		Pagination   pagination `json:"pagination"`
	}
	q := url.Values{"per_page": {"100"}, "page": {strconv.Itoa(page)}}
	for k, vs := range filter {
		q[k] = vs
	}
	err := c.do("GET", "/api/v1/transactions", q, nil, &r)
	return r.Transactions, r.Pagination.TotalPages, err
}

// Create returns the created transaction so it can be added to the list without
// a full refetch.
func (c *Client) Create(req TxnReq) (Txn, error) {
	var t Txn
	return t, c.do("POST", "/api/v1/transactions", nil, map[string]any{"transaction": req}, &t)
}

func (c *Client) Update(id string, req TxnReq) (Txn, error) {
	req.AccountID = "" // not allowed on update
	var t Txn
	return t, c.do("PATCH", "/api/v1/transactions/"+id, nil, map[string]any{"transaction": req}, &t)
}

func (c *Client) Delete(id string) error {
	return c.do("DELETE", "/api/v1/transactions/"+id, nil, nil, nil)
}
