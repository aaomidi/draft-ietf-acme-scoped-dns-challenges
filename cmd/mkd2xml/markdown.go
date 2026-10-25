package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// xrefNode is a kramdown-rfc cross-reference, written {{...}} in the source.
type xrefNode struct {
	ast.BaseInline
	Raw string
}

var kindXref = ast.NewNodeKind("Xref")

func (n *xrefNode) Kind() ast.NodeKind { return kindXref }

func (n *xrefNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Raw": n.Raw}, nil)
}

// xrefParser recognises {{...}} on a single line.
type xrefParser struct{}

func (xrefParser) Trigger() []byte { return []byte{'{'} }

func (xrefParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if !bytes.HasPrefix(line, []byte("{{")) {
		return nil
	}
	end := bytes.Index(line, []byte("}}"))
	if end < 0 {
		return nil
	}
	block.Advance(end + 2)
	return &xrefNode{Raw: string(line[2:end])}
}

var markdown = goldmark.New(
	goldmark.WithParserOptions(
		parser.WithInlineParsers(util.Prioritized(xrefParser{}, 150)),
	),
)

// renderer writes one part of the document as XML.
type renderer struct {
	c   *converter
	p   part
	out *xmlBuf
}

func (c *converter) newRenderer(p part, out *xmlBuf) (*renderer, ast.Node) {
	return &renderer{c: c, p: p, out: out}, markdown.Parser().Parse(text.NewReader(p.src))
}

// renderFlow renders a part that has no sections, such as the abstract.
func (c *converter) renderFlow(p part, out *xmlBuf) error {
	r, doc := c.newRenderer(p, out)
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if _, ok := n.(*ast.Heading); ok {
			return r.errorf(n, "headings are not allowed here")
		}
		if err := r.block(n); err != nil {
			return err
		}
	}
	return nil
}

// renderSections renders a part made of sections: every heading opens a
// <section> that lasts until the next heading of the same or a higher level.
func (c *converter) renderSections(p part, out *xmlBuf) error {
	r, doc := c.newRenderer(p, out)
	depth := 0
	closeTo := func(level int) {
		for ; depth >= level; depth-- {
			out.close("</section>")
		}
	}
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		h, ok := n.(*ast.Heading)
		if !ok {
			if depth == 0 {
				return r.errorf(n, "content before the first heading")
			}
			if err := r.block(n); err != nil {
				return err
			}
			continue
		}

		closeTo(h.Level)
		if h.Level != depth+1 {
			return r.errorf(h, "heading level jumps from %d to %d", depth, h.Level)
		}
		depth = h.Level

		// An attribute list on the line after a heading applies to it.
		var attrs sectionAttrs
		if next := n.NextSibling(); next != nil {
			if raw, ok := r.attributeList(next); ok {
				var err error
				if attrs, err = parseSectionAttrs(raw); err != nil {
					return r.errorf(next, "%v", err)
				}
				n = next
			}
		}

		anchor := attrs.anchor
		if anchor == "" {
			anchor = c.uniqueAnchor(autoAnchor(r.plainText(h)))
		} else if c.anchors[anchor] {
			return r.errorf(h, "anchor %q is used twice", anchor)
		}
		c.anchors[anchor] = true

		tag := "<section"
		for _, a := range attrs.attrs {
			tag += " " + a.name + `="` + escAttr(a.value) + `"`
		}
		out.open(tag + ` anchor="` + escAttr(anchor) + `">`)
		name, err := r.inline(h, false)
		if err != nil {
			return err
		}
		out.line("<name>" + name + "</name>")
	}
	closeTo(1)
	return nil
}

type attr struct{ name, value string }

type sectionAttrs struct {
	anchor string
	attrs  []attr
}

var (
	attributeListLine = regexp.MustCompile(`^\{:\s*([^:].*?)?\s*\}$`)
	attributeToken    = regexp.MustCompile(`^(?:#([A-Za-z][\w.-]*)|([A-Za-z][\w-]*)="([^"]*)")(?:\s+|$)`)
)

// attributeList reports whether n is a paragraph holding only a kramdown
// attribute list such as {:numbered="false"}, and returns its content.
func (r *renderer) attributeList(n ast.Node) (string, bool) {
	if _, ok := n.(*ast.Paragraph); !ok {
		return "", false
	}
	m := attributeListLine.FindStringSubmatch(strings.TrimSpace(r.rawLines(n)))
	if m == nil {
		return "", false
	}
	return m[1], true
}

func parseSectionAttrs(raw string) (sectionAttrs, error) {
	var s sectionAttrs
	for rest := raw; rest != ""; {
		m := attributeToken.FindStringSubmatch(rest)
		if m == nil {
			return s, fmt.Errorf("unsupported attribute list {:%s}", raw)
		}
		rest = rest[len(m[0]):]
		if m[1] != "" {
			s.anchor = m[1]
			continue
		}
		switch m[2] {
		case "numbered", "toc", "removeInRFC":
			s.attrs = append(s.attrs, attr{m[2], m[3]})
		default:
			return s, fmt.Errorf("unsupported section attribute %q", m[2])
		}
	}
	return s, nil
}

var (
	anchorLeading = regexp.MustCompile(`^[^A-Za-z]+`)
	anchorInvalid = regexp.MustCompile(`[^A-Za-z0-9 -]`)
	anchorDashes  = regexp.MustCompile(`-+`)
)

// autoAnchor derives a section anchor from its title the way kramdown-rfc
// does: drop everything before the first letter, keep letters, digits, spaces
// and dashes, turn spaces into dashes, lower-case the result and collapse
// runs of dashes.
func autoAnchor(title string) string {
	s := anchorLeading.ReplaceAllString(title, "")
	s = anchorInvalid.ReplaceAllString(s, "")
	s = strings.ToLower(strings.ReplaceAll(s, " ", "-"))
	if s == "" {
		s = "section"
	}
	return anchorDashes.ReplaceAllString(s, "-")
}

func (c *converter) uniqueAnchor(base string) string {
	anchor := base
	for i := 1; c.anchors[anchor]; i++ {
		anchor = base + "-" + strconv.Itoa(i)
	}
	return anchor
}

const (
	boilerplateBCP14       = "{::boilerplate bcp14}"
	boilerplateBCP14Tagged = "{::boilerplate bcp14-tagged}"
)

var bcp14Words = []string{
	"MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT",
	"RECOMMENDED", "NOT RECOMMENDED", "MAY", "OPTIONAL",
}

// bcp14Boilerplate is the paragraph required by BCP 14 (RFC 8174).
func bcp14Boilerplate(tagged bool) string {
	quoted := make([]string, len(bcp14Words))
	for i, w := range bcp14Words {
		if tagged {
			w = "<bcp14>" + w + "</bcp14>"
		}
		quoted[i] = `"` + w + `"`
	}
	last := len(quoted) - 1
	// The no-break space keeps "BCP 14" on one line in the rendered text.
	return "The key words " + strings.Join(quoted[:last], ", ") + ", and " + quoted[last] +
		" in this document are to be interpreted as described in BCP&#xA0;14" +
		` <xref target="RFC2119"/> <xref target="RFC8174"/> when, and only when, they` +
		" appear in all capitals, as shown here."
}

// block renders one block-level node.
func (r *renderer) block(n ast.Node) error {
	switch n := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		raw := strings.TrimSpace(r.rawLines(n))
		switch {
		case raw == boilerplateBCP14 || raw == boilerplateBCP14Tagged:
			r.c.refs.cite("RFC2119", true)
			r.c.refs.cite("RFC8174", true)
			r.out.line("<t>" + bcp14Boilerplate(raw == boilerplateBCP14Tagged) + "</t>")
			return nil
		case strings.HasPrefix(raw, "{:") && strings.HasSuffix(raw, "}"):
			return r.errorf(n, "unsupported kramdown directive %s", raw)
		}
		s, err := r.inline(n, r.c.tagBCP14)
		if err != nil {
			return err
		}
		r.out.line("<t>" + s + "</t>")
		return nil

	case *ast.List:
		tag, end := `<ul spacing="normal">`, "</ul>"
		if n.IsOrdered() {
			tag, end = `<ol spacing="normal" type="1">`, "</ol>"
			if n.Start != 1 {
				tag = fmt.Sprintf(`<ol spacing="normal" type="1" start="%d">`, n.Start)
			}
		}
		r.out.open(tag)
		for item := n.FirstChild(); item != nil; item = item.NextSibling() {
			r.out.open("<li>")
			for child := item.FirstChild(); child != nil; child = child.NextSibling() {
				if err := r.block(child); err != nil {
					return err
				}
			}
			r.out.close("</li>")
		}
		r.out.close(end)
		return nil

	case *ast.FencedCodeBlock:
		r.code(n, string(n.Language(r.p.src)))
		return nil

	case *ast.CodeBlock:
		r.code(n, "")
		return nil

	case *ast.Heading:
		return r.errorf(n, "headings are only supported at the top level")
	}
	return r.errorf(n, "unsupported Markdown construct (%s)", n.Kind())
}

// code renders a code block: <sourcecode> when it names a language, <artwork>
// otherwise. The content is copied verbatim.
func (r *renderer) code(n ast.Node, language string) {
	content := "\n" + r.rawLines(n)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	open, end := "<artwork>", "</artwork>"
	if language != "" {
		open, end = `<sourcecode type="`+escAttr(language)+`">`, "</sourcecode>"
	}
	r.out.raw(strings.Repeat("  ", r.out.depth) + open + cdata(content) + end + "\n")
}

var bcp14Keyword = regexp.MustCompile(
	`\b(MUST\s+NOT|SHALL\s+NOT|SHOULD\s+NOT|NOT\s+RECOMMENDED|MUST|REQUIRED|SHALL|SHOULD|RECOMMENDED|MAY|OPTIONAL)\b`)

// inline renders the inline content of n. With tag set, BCP 14 keywords in
// plain text are wrapped in <bcp14>.
func (r *renderer) inline(n ast.Node, tag bool) (string, error) {
	var out strings.Builder
	// Adjacent text nodes are joined before they are written, so that a
	// keyword is recognised however the parser happened to split the text.
	var pending strings.Builder
	flush := func() {
		s := pending.String()
		pending.Reset()
		if !tag {
			out.WriteString(escText(s))
			return
		}
		last := 0
		for _, m := range bcp14Keyword.FindAllStringIndex(s, -1) {
			out.WriteString(escText(s[last:m[0]]))
			out.WriteString("<bcp14>" + s[m[0]:m[1]] + "</bcp14>")
			last = m[1]
		}
		out.WriteString(escText(s[last:]))
	}

	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch child := child.(type) {
		case *ast.Text:
			if child.HardLineBreak() {
				return "", r.errorf(child, "hard line breaks are not supported")
			}
			pending.Write(child.Value(r.p.src))
			if child.SoftLineBreak() {
				pending.WriteByte('\n')
			}
			continue
		case *ast.String:
			pending.Write(child.Value)
			continue
		}

		flush()
		switch child := child.(type) {
		case *ast.CodeSpan:
			out.WriteString("<tt>" + escText(r.plainText(child)) + "</tt>")
		case *ast.Emphasis:
			inner, err := r.inline(child, tag)
			if err != nil {
				return "", err
			}
			if child.Level == 2 {
				out.WriteString("<strong>" + inner + "</strong>")
			} else {
				out.WriteString("<em>" + inner + "</em>")
			}
		case *ast.Link:
			inner, err := r.inline(child, false)
			if err != nil {
				return "", err
			}
			target := string(child.Destination)
			if inner == escText(target) {
				out.WriteString(`<eref target="` + escAttr(target) + `"/>`)
			} else {
				out.WriteString(`<eref target="` + escAttr(target) + `">` + inner + "</eref>")
			}
		case *ast.AutoLink:
			target := string(child.URL(r.p.src))
			if child.AutoLinkType == ast.AutoLinkEmail {
				target = "mailto:" + target
			}
			out.WriteString(`<eref target="` + escAttr(target) + `"/>`)
		case *xrefNode:
			x, err := parseXref(child.Raw)
			if err != nil {
				return "", r.errorf(child, "%v", err)
			}
			switch x.cite {
			case '!':
				r.c.refs.cite(x.target, true)
			case '?':
				r.c.refs.cite(x.target, false)
			}
			r.c.xrefs = append(r.c.xrefs, xrefUse{x.target, r.lineOf(child)})
			out.WriteString(x.element())
		default:
			return "", r.errorf(child, "unsupported Markdown construct (%s)", child.Kind())
		}
	}
	flush()
	return out.String(), nil
}

// xref is a parsed {{...}} cross-reference.
type xref struct {
	target string
	// cite is '!' or '?' when the reference is to be added to the normative
	// or informative references, and 0 otherwise.
	cite byte
	// section and format are set for a reference to a part of the target.
	section, format string
}

const (
	xrefTarget  = `([!?]?)([A-Za-z0-9][A-Za-z0-9._-]*)`
	xrefSection = `(?:Section|Appendix)\s+([A-Za-z0-9][A-Za-z0-9.]*)`
)

var xrefForms = []struct {
	re                    *regexp.Regexp
	cite, target, section int
	format                string
}{
	{regexp.MustCompile(`^` + xrefTarget + `$`), 1, 2, 0, ""},
	{regexp.MustCompile(`^` + xrefTarget + `\s*,\s*` + xrefSection + `$`), 1, 2, 3, "comma"},
	{regexp.MustCompile(`^` + xrefSection + `\s+of\s+` + xrefTarget + `$`), 2, 3, 1, "of"},
	{regexp.MustCompile(`^` + xrefTarget + `\s*\(\s*` + xrefSection + `\s*\)$`), 1, 2, 3, "parens"},
}

func parseXref(raw string) (xref, error) {
	s := strings.TrimSpace(raw)
	for _, f := range xrefForms {
		m := f.re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		x := xref{target: m[f.target], format: f.format}
		if m[f.cite] != "" {
			x.cite = m[f.cite][0]
		}
		if f.section != 0 {
			x.section = m[f.section]
		}
		return x, nil
	}
	return xref{}, fmt.Errorf("unsupported cross-reference {{%s}}", raw)
}

func (x xref) element() string {
	if x.section == "" {
		return `<xref target="` + escAttr(x.target) + `"/>`
	}
	return `<xref section="` + escAttr(x.section) + `" sectionFormat="` + x.format +
		`" target="` + escAttr(x.target) + `"/>`
}

// rawLines returns the source text of a block node.
func (r *renderer) rawLines(n ast.Node) string {
	var b strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(r.p.src))
	}
	return b.String()
}

// plainText returns the text of n without any markup.
func (r *renderer) plainText(n ast.Node) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				b.Write(c.Value(r.p.src))
				if c.SoftLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(c.Value)
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}

// lineOf returns the line of the source file that n starts on, or the first
// line of the enclosing block for an inline node.
func (r *renderer) lineOf(n ast.Node) int {
	for cur := n; cur != nil; cur = cur.Parent() {
		if cur.Type() == ast.TypeInline {
			continue
		}
		if off, ok := firstOffset(cur); ok {
			return r.p.firstLine + bytes.Count(r.p.src[:off], []byte("\n"))
		}
	}
	return r.p.firstLine
}

func firstOffset(n ast.Node) (int, bool) {
	if n.Type() == ast.TypeBlock {
		if lines := n.Lines(); lines != nil && lines.Len() > 0 {
			return lines.At(0).Start, true
		}
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if c.Type() == ast.TypeInline {
			continue
		}
		if off, ok := firstOffset(c); ok {
			return off, true
		}
	}
	return 0, false
}

func (r *renderer) errorf(n ast.Node, format string, args ...any) error {
	return fmt.Errorf("line %d: %s", r.lineOf(n), fmt.Sprintf(format, args...))
}
