package rfc

import (
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// htmlBlocks are the elements around which white space does not show.
var htmlBlocks = map[string]bool{
	"html": true, "head": true, "body": true, "title": true, "meta": true, "link": true,
	"style": true, "script": true, "table": true, "thead": true, "tfoot": true, "tr": true,
	"td": true, "div": true, "dl": true, "dt": true, "dd": true, "section": true, "nav": true,
	"ul": true, "ol": true, "li": true, "p": true, "pre": true, "address": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

// normalizeHTML reduces a page to a list of tags and text in which two pages
// that a browser shows identically come out equal: attributes are sorted,
// runs of white space are collapsed, and white space next to a block
// element is dropped. Comments, the generator name and the stylesheet text
// are left out, since those are expected to differ or are checked elsewhere.
func normalizeHTML(t *testing.T, src string) []string {
	t.Helper()
	type token struct {
		text  string
		isTag bool
		block bool
	}
	var tokens []token
	for i := 0; i < len(src); {
		if src[i] != '<' {
			j := strings.IndexByte(src[i:], '<')
			if j < 0 {
				j = len(src) - i
			}
			text := html.UnescapeString(src[i : i+j])
			lead := text != "" && isASCIISpace(rune(text[0]))
			trail := text != "" && isASCIISpace(rune(text[len(text)-1]))
			text = strings.Join(strings.FieldsFunc(text, isASCIISpace), " ")
			if text == "" {
				if lead {
					tokens = append(tokens, token{text: " "})
				}
			} else {
				if lead {
					text = " " + text
				}
				if trail {
					text += " "
				}
				tokens = append(tokens, token{text: text})
			}
			i += j
			continue
		}
		if strings.HasPrefix(src[i:], "<!--") {
			end := strings.Index(src[i:], "-->")
			if end < 0 {
				t.Fatalf("unterminated comment")
			}
			i += end + 3
			continue
		}
		// Find the end of the tag, skipping over quoted attribute values.
		j, quote := i+1, byte(0)
		for ; j < len(src); j++ {
			c := src[j]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
			} else if c == '"' || c == '\'' {
				quote = c
			} else if c == '>' {
				break
			}
		}
		if j >= len(src) {
			t.Fatalf("unterminated tag at byte %d", i)
		}
		body := src[i+1 : j]
		i = j + 1
		if strings.HasPrefix(strings.ToLower(body), "!doctype") {
			tokens = append(tokens, token{text: "<!doctype>", isTag: true, block: true})
			continue
		}
		closing := strings.HasPrefix(body, "/")
		name, rest, _ := strings.Cut(strings.TrimPrefix(body, "/"), " ")
		name = strings.ToLower(strings.TrimSpace(name))

		attrs := map[string]string{}
		for rest = strings.TrimSpace(rest); rest != ""; rest = strings.TrimSpace(rest) {
			k, v, hasValue := strings.Cut(rest, "=")
			if !hasValue || strings.ContainsAny(k, " \t\n") {
				k, rest, _ = strings.Cut(rest, " ")
				attrs[k] = ""
				continue
			}
			if v == "" || (v[0] != '"' && v[0] != '\'') {
				v, rest, _ = strings.Cut(v, " ")
			} else {
				end := strings.IndexByte(v[1:], v[0])
				if end < 0 {
					t.Fatalf("unterminated attribute in <%s>", body)
				}
				v, rest = v[1:end+1], v[end+2:]
			}
			attrs[k] = html.UnescapeString(v)
		}
		if name == "meta" && attrs["name"] == "generator" {
			continue
		}
		if name == "meta" && attrs["name"] == "description" {
			attrs["content"] = strings.Join(strings.Fields(attrs["content"]), " ")
		}
		var keys []string
		for k := range attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		text := "<" + name
		if closing {
			text = "</" + name
		}
		for _, k := range keys {
			text += " " + k + "=" + `"` + attrs[k] + `"`
		}
		tokens = append(tokens, token{text: text + ">", isTag: true, block: htmlBlocks[name]})

		// The content of these elements is taken whole.
		if !closing && (name == "script" || name == "style" || name == "pre") {
			end := strings.Index(src[i:], "</"+name)
			if end < 0 {
				t.Fatalf("unterminated <%s>", name)
			}
			content := src[i : i+end]
			i += end
			switch name {
			case "style":
				content = "(stylesheet)"
			case "script":
				content = strings.TrimSpace(content)
			case "pre":
				content = strings.TrimPrefix(html.UnescapeString(content), "\n")
			}
			tokens = append(tokens, token{text: name + ": " + content, isTag: true, block: true})
		}
	}
	var out []string
	for i, tok := range tokens {
		if tok.isTag {
			out = append(out, tok.text)
			continue
		}
		text := tok.text
		if i == 0 || tokens[i-1].block {
			text = strings.TrimLeft(text, " ")
		}
		if i == len(tokens)-1 || tokens[i+1].block {
			text = strings.TrimRight(text, " ")
		}
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

// compareHTML reports the first place where two pages differ once
// normalized.
func compareHTML(t *testing.T, got, want string) {
	t.Helper()
	g, w := normalizeHTML(t, got), normalizeHTML(t, want)
	for i := 0; i < len(g) || i < len(w); i++ {
		var a, b string
		if i < len(g) {
			a = g[i]
		}
		if i < len(w) {
			b = w[i]
		}
		if a == b {
			continue
		}
		context := strings.Join(w[max(0, i-4):i], " ")
		t.Fatalf("pages differ at item %d of %d\nafter: %s\n  got: %q\n want: %q", i, len(w), context, a, b)
	}
}

// TestHTMLMatchesXml2rfc renders each fixture and compares it with the page
// that xml2rfc produced from the same XML; see testdata/README.md.
//
// Setting RFC_HTML_EXTRA to a directory compares every NAME.xml in it with
// its NAME.html as well, for checking against a fresh xml2rfc run.
func TestHTMLMatchesXml2rfc(t *testing.T) {
	type fixture struct{ name, xml, html string }
	var fixtures []fixture
	for _, name := range []string{"draft", "rich", "long"} {
		fixtures = append(fixtures, fixture{name,
			filepath.Join("testdata", name+".xml"), filepath.Join("testdata", name+".html")})
	}
	if dir := os.Getenv("RFC_HTML_EXTRA"); dir != "" {
		matches, err := filepath.Glob(filepath.Join(dir, "*.xml"))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			fixtures = append(fixtures, fixture{"extra/" + filepath.Base(m), m, strings.TrimSuffix(m, ".xml") + ".html"})
		}
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			in, err := os.Open(f.xml)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, _, err := HTML(in, testToday, HTMLOptions{XMLLink: filepath.Base(f.xml)})
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(f.html)
			if err != nil {
				t.Fatal(err)
			}
			compareHTML(t, got, string(want))
		})
	}
}

// TestCompareHTMLIsStrict guards the comparison itself: a real page with one
// word, one class or one link changed must not compare equal to the original.
func TestCompareHTMLIsStrict(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "draft.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	original := strings.Join(normalizeHTML(t, page), "\x00")
	for _, change := range [][2]string{
		{"each domain can only delegate", "each domain can only relegate"},
		{`class="section-number selfRef">3.1. </a>`, `class="section-number selfRef">3.2. </a>`},
		{`class="relref">Section 7.3</a>`, `class="xref">Section 7.3</a>`},
		{`href="#name-errors"`, `href="#name-error"`},
		{`<code>dns-01</code> challenge specified in section`, `<code>dns-01</code>challenge specified in section`},
	} {
		if !strings.Contains(page, change[0]) {
			t.Fatalf("fixture no longer contains %q", change[0])
		}
		changed := strings.Replace(page, change[0], change[1], 1)
		if strings.Join(normalizeHTML(t, changed), "\x00") == original {
			t.Errorf("changing %q to %q went unnoticed", change[0], change[1])
		}
	}
}

func TestNormalizeHTML(t *testing.T) {
	same := [][2]string{
		{`<p id="a" class="x">one  two</p>`, "<p class=\"x\" id=\"a\">\n  one\ntwo\n</p>"},
		{`<p>a <b>b</b> c</p>`, "<p>a\n<b>b</b>\nc</p>"},
		{`<div><!-- note --><p>x</p></div>`, `<div> <p>x</p> </div>`},
		{`<pre>keep  this</pre>`, "<pre>\nkeep  this</pre>"},
		{`<p>a &lt; b</p>`, `<p>a &#60; b</p>`},
	}
	for _, pair := range same {
		a, b := normalizeHTML(t, pair[0]), normalizeHTML(t, pair[1])
		if strings.Join(a, "|") != strings.Join(b, "|") {
			t.Errorf("expected equal:\n %q -> %q\n %q -> %q", pair[0], a, pair[1], b)
		}
	}
	different := [][2]string{
		{`<p>a<b>b</b></p>`, `<p>a <b>b</b></p>`},
		{`<p class="x">a</p>`, `<p class="y">a</p>`},
		{`<pre>a b</pre>`, `<pre>a  b</pre>`},
		{`<p>a</p>`, `<p>b</p>`},
	}
	for _, pair := range different {
		a, b := normalizeHTML(t, pair[0]), normalizeHTML(t, pair[1])
		if strings.Join(a, "|") == strings.Join(b, "|") {
			t.Errorf("expected different, both gave %q:\n %q\n %q", a, pair[0], pair[1])
		}
	}
}
