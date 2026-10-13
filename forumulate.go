// Package forumulate adds Forumulate to Go sites rendered with html/template:
// template funcs for the comments, docs-search and changelog embeds, and a
// cached client for your public blog posts, forum topics, changelog releases
// and docs pages, for server-rendered (SEO) pages.
//
//	fm := forumulate.New(forumulate.Config{Org: "acme", Space: "community"})
//	tmpl := template.New("page").Funcs(fm.FuncMap())
//
//	{{ forumulateComments }}
//	{{ forumulateWidget "mode" "ask" "placeholder" "Ask the docs" }}
//	{{ range forumulateItems "blog" 5 }}<a href="{{ .url }}">{{ .title }}</a>{{ end }}
package forumulate

import (
	"context"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Config says where your Forumulate content lives.
type Config struct {
	// BaseURL is your Forumulate address or custom domain. Default https://forumulate.com.
	BaseURL string
	// APIURL defaults to BaseURL + "/api".
	APIURL string
	// Org and Space (slug or id). Every func accepts a space of its own.
	Org, Space string
	// Elements emits <forumulate-*> custom elements (from the @forumulate/embed
	// package at ElementsSrc) instead of the widget.js / comments.js /
	// changelog.js scripts the API serves.
	Elements    bool
	ElementsSrc string
	// CacheTTL is how long a content read is kept. Default 5 minutes.
	CacheTTL time.Duration
	Timeout  time.Duration
	// Markdown renders Markdown to HTML for forumulateHTML. It MUST NOT pass raw
	// HTML through (forum posts are written by visitors): with goldmark, leave
	// html.WithUnsafe() off. Nil renders escaped paragraphs.
	Markdown   func(string) (template.HTML, error)
	HTTPClient *http.Client
}

func (c Config) withDefaults() Config {
	if c.BaseURL == "" {
		c.BaseURL = "https://forumulate.com"
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.APIURL == "" {
		c.APIURL = c.BaseURL + "/api"
	}
	c.APIURL = strings.TrimRight(c.APIURL, "/")
	if c.ElementsSrc == "" {
		c.ElementsSrc = "https://cdn.jsdelivr.net/npm/@forumulate/embed@1"
	}
	if c.CacheTTL == 0 {
		c.CacheTTL = 5 * time.Minute
	}
	if c.Timeout == 0 {
		c.Timeout = 5 * time.Second
	}
	return c
}

// Forumulate bundles the embed builder and the content client.
type Forumulate struct {
	cfg    Config
	Client *Client
}

// New returns a Forumulate for cfg.
func New(cfg Config) *Forumulate {
	cfg = cfg.withDefaults()
	return &Forumulate{cfg: cfg, Client: NewClient(cfg)}
}

var embeds = map[string][2]string{
	"comments":  {"comments.js", "forumulate-comments"},
	"widget":    {"widget.js", "forumulate-widget"},
	"changelog": {"changelog.js", "forumulate-changelog"},
	"feed":      {"", "forumulate-feed"},
	"search":    {"", "forumulate-search"},
}

var attrName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Embed returns the markup for one embed. options are key/value pairs
// ("theme", "dark", "space", "blog-comments", "use-elements", true); every value
// is HTML-escaped, so the template.HTML it returns is safe to print.
func (f *Forumulate) Embed(kind string, options ...any) (template.HTML, error) {
	spec, ok := embeds[kind]
	if !ok {
		return "", fmt.Errorf("forumulate: unknown embed %q", kind)
	}
	if len(options)%2 != 0 {
		return "", fmt.Errorf("forumulate: %s options must be key/value pairs", kind)
	}
	org, space, useElements := f.cfg.Org, f.cfg.Space, f.cfg.Elements
	var keys, values []string
	for i := 0; i < len(options); i += 2 {
		name := strings.ToLower(strings.ReplaceAll(fmt.Sprint(options[i]), "_", "-"))
		if !attrName.MatchString(name) {
			return "", fmt.Errorf("forumulate: invalid option name %q", name)
		}
		value := options[i+1]
		switch name {
		case "org":
			org = fmt.Sprint(value)
			continue
		case "space":
			space = fmt.Sprint(value)
			continue
		case "use-elements":
			useElements = value == true || value == "true"
			continue
		}
		if value == nil || value == false {
			continue
		}
		keys = append(keys, name)
		values = append(values, fmt.Sprint(value))
	}
	if org == "" || space == "" {
		return "", fmt.Errorf("forumulate: set Config.Org and Config.Space (or pass \"space\")")
	}
	keys = append([]string{"org", "space", "api"}, keys...)
	values = append([]string{org, space, f.cfg.APIURL}, values...)

	var b strings.Builder
	attrs := func(prefix string) {
		for i, k := range keys {
			fmt.Fprintf(&b, ` %s%s="%s"`, prefix, k, html.EscapeString(values[i]))
		}
	}
	if useElements || spec[0] == "" {
		b.WriteString("<" + spec[1])
		attrs("")
		fmt.Fprintf(&b, `></%s><script type="module" src="%s" async></script>`, spec[1], html.EscapeString(f.cfg.ElementsSrc))
	} else {
		// The scripts mount into their parent node, so each gets its own box.
		fmt.Fprintf(&b, `<div class="forumulate-%s"><script src="%s"`, kind, html.EscapeString(f.cfg.APIURL+"/embed/"+spec[0]))
		attrs("data-")
		b.WriteString(` async></script></div>`)
	}
	return template.HTML(b.String()), nil
}

// RenderMarkdown turns Markdown into HTML with Config.Markdown, or, without
// one, into escaped paragraphs.
func (f *Forumulate) RenderMarkdown(text string) (template.HTML, error) {
	if f.cfg.Markdown != nil {
		return f.cfg.Markdown(text)
	}
	var b strings.Builder
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		b.WriteString("<p>" + strings.ReplaceAll(html.EscapeString(para), "\n", "<br>") + "</p>")
	}
	return template.HTML(b.String()), nil
}

func optSpace(space []string) string {
	if len(space) > 0 {
		return space[0]
	}
	return ""
}

// FuncMap returns the template funcs. Content funcs use context.Background();
// call f.Client directly to pass a request context.
func (f *Forumulate) FuncMap() template.FuncMap {
	embed := func(kind string) func(...any) (template.HTML, error) {
		return func(o ...any) (template.HTML, error) { return f.Embed(kind, o...) }
	}
	ctx := context.Background()
	return template.FuncMap{
		"forumulateComments":  embed("comments"),
		"forumulateWidget":    embed("widget"),
		"forumulateChangelog": embed("changelog"),
		"forumulateFeed":      embed("feed"),
		"forumulateSearch":    embed("search"),
		"forumulateItems": func(kind string, limit int, space ...string) ([]Item, error) {
			return f.Client.Items(ctx, kind, limit, optSpace(space))
		},
		"forumulateItem": func(kind, slug string, space ...string) (Item, error) {
			return f.Client.Item(ctx, kind, slug, optSpace(space))
		},
		"forumulateMarkdown": func(kind, slug string, space ...string) (string, error) {
			return f.Client.Markdown(ctx, kind, slug, optSpace(space))
		},
		"forumulateHTML": f.RenderMarkdown,
	}
}
