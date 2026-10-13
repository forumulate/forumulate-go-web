package forumulate

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestEmbedEscapesEveryValue(t *testing.T) {
	fm := New(Config{Org: "acme", Space: `sp"ace`})
	tmpl := template.Must(template.New("t").Funcs(fm.FuncMap()).Parse(
		`{{ forumulateComments "theme" .Theme }}|{{ forumulateWidget "use-elements" true "placeholder" .Theme }}|{{ forumulateHTML .Body }}`))
	var b bytes.Buffer
	if err := tmpl.Execute(&b, map[string]string{"Theme": `"><script>x</script>`, "Body": "<b>hi</b>\n\nthere"}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if strings.Contains(out, "<script>x") || strings.Contains(out, "<b>hi") {
		t.Fatalf("unescaped value in %s", out)
	}
	for _, want := range []string{`data-space="sp&#34;ace"`, `<forumulate-widget org="acme"`, `src="https://forumulate.com/api/embed/comments.js"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s in %s", want, out)
		}
	}
	if _, err := fm.Embed("comments", "on load", "x"); err == nil {
		t.Fatal("accepted an invalid option name")
	}
}
