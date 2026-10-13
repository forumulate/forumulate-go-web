# forumulate-go-web

Forumulate for Go sites rendered with `html/template`: template funcs for
comments, the docs search / ask-AI box and the "What's new" changelog bell, plus
a cached client for your public blog posts, forum topics, changelog releases and
docs pages, so they can be rendered server-side for SEO. Standard library only.

## Install

```sh
go get github.com/forumulate/forumulate-go-web
```

```go
import forumulate "github.com/forumulate/forumulate-go-web"

fm := forumulate.New(forumulate.Config{
	Org:   "acme",      // organization slug or id
	Space: "community", // default space; funcs accept a space of their own
	// BaseURL:  "https://forumulate.com", // or your custom domain
	// Elements: true,                     // emit <forumulate-*> custom elements
	// CacheTTL: 5 * time.Minute,
})
tmpl := template.Must(template.New("page").Funcs(fm.FuncMap()).ParseGlob("templates/*.html"))
```

## Embeds

```gotemplate
{{ forumulateComments }}
{{ forumulateComments "space" "blog-comments" "theme" "dark" "topic" .TopicID }}
{{ forumulateWidget "mode" "ask" "placeholder" "Ask the docs" }}
{{ forumulateChangelog "space" "changelog" }}
```

Options are key/value pairs; each becomes an HTML-escaped `data-*` attribute
(or an element attribute with `Elements: true`; `"use-elements" true` switches
one call).

## Server-rendered content

```gotemplate
{{ range forumulateItems "blog" 5 }}
  <a href="{{ .url }}">{{ .title }}</a>
{{ end }}

{{ with forumulateItem "forum" .Slug }}<h1>{{ .title }}</h1>{{ end }}
{{ forumulateHTML (forumulateMarkdown "blog" .Slug) }}
```

Kinds: `blog`, `forum`, `changelog`, `docs`; an optional last argument names
the space. Only the public read API is used, no token. If Forumulate is
unreachable or a space is private, lists are empty and the page still renders.

`forumulateHTML` renders escaped paragraphs unless you plug in a Markdown
renderer — for example goldmark, which drops raw HTML and unsafe links unless
`html.WithUnsafe()` is set (keep it off: forum posts are written by visitors):

```go
md := goldmark.New(goldmark.WithExtensions(extension.GFM))
cfg.Markdown = func(s string) (template.HTML, error) {
	var b bytes.Buffer
	err := md.Convert([]byte(s), &b)
	return template.HTML(b.String()), err
}
```

Outside templates, use `fm.Client.Items(ctx, "blog", 10, "")` with a request context.

## License

MIT

---

## About this repository

This repository is a public mirror, refreshed on each Forumulate release
(one commit per release). Pull requests are welcome; accepted changes are
applied upstream and arrive here with the next release.

- API reference: https://forumulate.com/reference
- Guides: https://forumulate.com/guides
- Community and support: https://forumulate.com/community
- Source: https://github.com/forumulate/forumulate-go-web
