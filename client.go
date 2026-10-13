package forumulate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Item is one blog post, forum topic, changelog release or docs page, as the
// public API returns it, plus a "url" key pointing at its reader page.
type Item = map[string]any

// kind -> list path, list key, item path, reader prefix
var kinds = map[string][4]string{
	"blog":      {"/blog", "articles", "/blog/t/%s", "b"},
	"forum":     {"/forum", "topics", "/forum/t/%s", "f"},
	"changelog": {"/changelog", "releases", "/changelog/t/%s", "c"},
	"docs":      {"", "", "/pages/%s", "p"},
}

type cacheEntry struct {
	until time.Time
	value any // nil = a cached failure
}

// Client reads from the PUBLIC read API (/public/orgs/:org/spaces/:space/...)
// and keeps each answer in memory for Config.CacheTTL. No token is ever sent.
// A failure (network, a private space's 404) yields an empty list / nil, kept
// for 30s, so a page that lists posts still renders when Forumulate is down.
type Client struct {
	cfg   Config
	http  *http.Client
	mu    sync.Mutex
	cache map[string]cacheEntry
}

// NewClient returns a client for cfg. A zero HTTPClient gets cfg.Timeout.
func NewClient(cfg Config) *Client {
	cfg = cfg.withDefaults()
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: cfg.Timeout}
	}
	return &Client{cfg: cfg, http: hc, cache: map[string]cacheEntry{}}
}

// Items lists blog posts, forum topics, changelog releases or docs pages.
// space may be "" for the configured default.
func (c *Client) Items(ctx context.Context, kind string, limit int, space string) ([]Item, error) {
	spec, ok := kinds[kind]
	if !ok {
		return nil, fmt.Errorf("forumulate: unknown kind %q", kind)
	}
	base, err := c.spaceURL(space)
	if err != nil {
		return nil, err
	}
	var rows []any
	if kind == "docs" {
		data, _ := c.fetch(ctx, base, true).(map[string]any)
		cols, _ := data["collections"].([]any)
		for _, col := range cols {
			m, _ := col.(map[string]any)
			pages, _ := m["pages"].([]any)
			for _, p := range pages {
				if pm, ok := p.(map[string]any); ok && pm["hideFromNav"] != true {
					rows = append(rows, pm)
				}
			}
		}
		if len(rows) > limit {
			rows = rows[:limit]
		}
	} else {
		data, _ := c.fetch(ctx, fmt.Sprintf("%s%s?limit=%d", base, spec[0], limit), true).(map[string]any)
		rows, _ = data[spec[1]].([]any)
	}
	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		item := make(Item, len(m)+1)
		for k, v := range m {
			item[k] = v
		}
		if ext, _ := m["externalUrl"].(string); ext != "" {
			item["url"] = ext
		} else {
			item["url"] = c.readerURL(spec[3], space, firstString(m, "slug", "id"))
		}
		out = append(out, item)
	}
	return out, nil
}

// Item returns one piece as JSON (the `article` / `topic` object for
// blog/forum, the page for docs), or nil when it cannot be read.
func (c *Client) Item(ctx context.Context, kind, slug, space string) (Item, error) {
	spec, ok := kinds[kind]
	if !ok {
		return nil, fmt.Errorf("forumulate: unknown kind %q", kind)
	}
	base, err := c.spaceURL(space)
	if err != nil {
		return nil, err
	}
	data, _ := c.fetch(ctx, base+fmt.Sprintf(spec[2], url.PathEscape(slug)), true).(map[string]any)
	if data == nil {
		return nil, nil
	}
	piece := data
	for _, key := range []string{"article", "topic", "release"} {
		if m, ok := data[key].(map[string]any); ok {
			piece = m
			break
		}
	}
	item := make(Item, len(piece)+1)
	for k, v := range piece {
		item[k] = v
	}
	if s := firstString(piece, "slug"); s != "" {
		slug = s
	}
	item["url"] = c.readerURL(spec[3], space, slug)
	return item, nil
}

// Markdown returns the piece as Markdown ("" when it cannot be read).
func (c *Client) Markdown(ctx context.Context, kind, slug, space string) (string, error) {
	spec, ok := kinds[kind]
	if !ok {
		return "", fmt.Errorf("forumulate: unknown kind %q", kind)
	}
	base, err := c.spaceURL(space)
	if err != nil {
		return "", err
	}
	body, _ := c.fetch(ctx, base+fmt.Sprintf(spec[2], url.PathEscape(slug))+".md", false).(string)
	return body, nil
}

func (c *Client) spaceURL(space string) (string, error) {
	if space == "" {
		space = c.cfg.Space
	}
	if c.cfg.Org == "" || space == "" {
		return "", fmt.Errorf("forumulate: set Config.Org and Config.Space (or pass a space)")
	}
	return fmt.Sprintf("%s/public/orgs/%s/spaces/%s", c.cfg.APIURL, url.PathEscape(c.cfg.Org), url.PathEscape(space)), nil
}

func (c *Client) readerURL(prefix, space, slug string) string {
	if space == "" {
		space = c.cfg.Space
	}
	return fmt.Sprintf("%s/%s/%s/%s/%s", c.cfg.BaseURL, prefix, url.PathEscape(c.cfg.Org), url.PathEscape(space), url.PathEscape(slug))
}

func (c *Client) fetch(ctx context.Context, u string, asJSON bool) any {
	now := time.Now()
	c.mu.Lock()
	if e, ok := c.cache[u]; ok && now.Before(e.until) {
		c.mu.Unlock()
		return e.value
	}
	c.mu.Unlock()

	value, ttl := c.request(ctx, u, asJSON)
	c.mu.Lock()
	if len(c.cache) > 2000 {
		c.cache = map[string]cacheEntry{}
	}
	c.cache[u] = cacheEntry{until: now.Add(ttl), value: value}
	c.mu.Unlock()
	return value
}

func (c *Client) request(ctx context.Context, u string, asJSON bool) (any, time.Duration) {
	const failTTL = 30 * time.Second
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, failTTL
	}
	if asJSON {
		req.Header.Set("Accept", "application/json")
	} else {
		req.Header.Set("Accept", "text/markdown")
	}
	req.Header.Set("User-Agent", "forumulate-go-web")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, failTTL
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, failTTL
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, failTTL
	}
	if !asJSON {
		return string(body), c.cfg.CacheTTL
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, failTTL
	}
	return v, c.cfg.CacheTTL
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
