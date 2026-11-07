package rfc

import (
	"fmt"
	"strconv"
	"strings"
)

// This file renders a prepared document to lines of text. It follows
// xml2rfc's text writer function by function, because the page breaks and
// line breaks of the two can only agree if the intermediate steps do: which
// element owns each line, how wide each element is told it may be, and how
// blocks are joined.

const maxWidth = 72

// line is one line of output and the element it came from. Pagination looks
// at the element to decide where a page may break. A blank line between
// blocks belongs to no element.
type line struct {
	text  string
	elem  *node
	block *block
}

func blankLine() *line { return &line{} }

// joiner says how a child element is attached to what precedes it.
type joiner struct {
	// join separates the child from the previous text. A join of "\n\n"
	// leaves one blank line.
	join string
	// indent and hang are the indentation of the child and the extra
	// indentation of its lines after the first.
	indent, hang int
	// overlap lets the child's first line continue the previous line.
	overlap bool
	// outdent lets a child that is too wide move left instead.
	outdent bool
}

// context is what a parent passes down to the children it renders.
type context struct {
	// joiners is keyed by tag, with "" as the default. A child inherits the
	// entries its parent does not replace.
	joiners map[string]joiner
	hang    int
	keepURL bool
}

var baseJoiners = map[string]joiner{"": {join: "\n\n"}}

// with returns a copy of c with some joiners replaced.
func (c context) with(update map[string]joiner) context {
	joiners := make(map[string]joiner, len(c.joiners)+len(update))
	for k, v := range c.joiners {
		joiners[k] = v
	}
	for k, v := range update {
		joiners[k] = v
	}
	c.joiners = joiners
	return c
}

func (c context) joiner(tag string) joiner {
	if j, ok := c.joiners[tag]; ok {
		return j
	}
	return c.joiners[""]
}

type writer struct {
	doc *document
}

func trim(s string) string  { return strings.Trim(s, stripSpace) }
func ltrim(s string) string { return strings.TrimLeft(s, stripSpace) }
func rtrim(s string) string { return strings.TrimRight(s, stripSpace) }

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// splitLines splits on line breaks the way Python's splitlines does: a final
// line break does not start another line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, string(lineSep), "\n")
	parts := strings.Split(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// mkLines turns text into lines owned by e.
func mkLines(text string, e *node) []*line {
	var lines []*line
	for _, t := range splitLines(text) {
		lines = append(lines, &line{text: t, elem: e})
	}
	return lines
}

func joinLines(lines []*line, sep string) string {
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.text
	}
	return strings.Join(texts, sep)
}

// stripBlank removes blank lines from both ends.
func stripBlank(lines []*line) []*line {
	for len(lines) > 0 && trim(lines[0].text) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && trim(lines[len(lines)-1].text) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// indentText indents the lines of text, the lines after the first by hang
// more.
func indentText(text string, indent, hang int) string {
	parts := strings.Split(strings.ReplaceAll(text, string(lineSep), "\n"), "\n")
	seen := false
	for i, l := range parts {
		if trim(l) == "" {
			parts[i] = ""
			continue
		}
		if seen {
			parts[i] = spaces(indent+hang) + l
		} else {
			parts[i] = spaces(indent) + l
		}
		seen = true
	}
	return strings.Join(parts, "\n")
}

// renderLines renders a block element.
func (w *writer) renderLines(e *node, width int, c context) []*line {
	switch e.tag {
	case "rfc":
		return w.joinChildren(nil, e.children, width, c)
	case "front":
		return w.renderFront(e, width, c)
	case "abstract":
		c = c.with(map[string]joiner{"": {join: "\n\n", indent: 3}})
		return w.joinChildren([]*line{{text: "Abstract", elem: e}}, e.children, width, c)
	case "note":
		c = c.with(map[string]joiner{"": {join: "\n\n", indent: 3}, "name": {join: ": "}})
		var lines []*line
		if len(e.children) == 0 || e.children[0].tag != "name" {
			lines = append(lines, &line{text: "Note", elem: e})
		}
		return w.joinChildren(lines, e.children, width, c)
	case "boilerplate", "toc":
		return w.joinChildren(nil, e.children, width, c)
	case "middle":
		c.joiners = baseJoiners
		return w.joinChildren(nil, e.children, width, c)
	case "back":
		return w.joinChildren(nil, e.children, width, c)
	case "section":
		return w.renderSection(e, width, c)
	case "references":
		return w.renderReferences(e, width, c)
	case "reference":
		return w.renderReference(e, width, c)
	case "t":
		text := w.innerText(e)
		indent, _ := strconv.Atoi(e.getDefault("indent", "0"))
		return mkLines(fill(text, fillOptions{width: width, indent: indent, hang: c.hang, keepURL: c.keepURL}), e)
	case "ul":
		return w.renderUL(e, width, c)
	case "ol":
		return w.renderOL(e, width, c)
	case "li":
		return w.renderLI(e, width, c)
	case "artwork", "sourcecode":
		return w.renderArtwork(e, width)
	case "author":
		return w.renderAuthor(e, width, c)
	case "address":
		c.joiners = map[string]joiner{"": {join: "\n"}, "email": {overlap: true}}
		return w.joinChildren(nil, e.children, width, c)
	case "postal":
		return w.renderPostal(e, width, c)
	}
	return mkLines(w.renderText(e, width, c), e)
}

// renderText renders an element that produces running text.
func (w *writer) renderText(e *node, width int, c context) string {
	switch e.tag {
	case "name":
		return fill(trim(w.innerText(e)), fillOptions{width: width, hang: c.joiner("name").hang})
	case "email":
		if e.text == "" {
			return ""
		}
		if p := e.prev(); p != nil && p.tag == "email" {
			return fill(", "+e.text, fillOptions{width: width, hang: c.hang})
		}
		return "\n" + fill("Email: "+e.text, fillOptions{width: width, hang: c.hang})
	case "title":
		title := cleanText(e.allText())
		if e.parent.parent.get("quoteTitle") == "true" {
			title = `"` + title + `"`
		}
		return title
	case "seriesInfo":
		name, value := e.get("name"), e.get("value")
		if name == "Internet-Draft" {
			return name + ", " + value
		}
		return name + string(nbsp) + strings.ReplaceAll(value, "/", "/"+string(keepJoiner))
	case "date":
		return w.dateText(e)
	case "refcontent":
		return w.innerText(e)
	case "bcp14":
		return e.text + e.tail
	case "tt":
		return w.innerText(e) + e.tail
	case "em":
		return "_" + w.innerText(e) + "_" + e.tail
	case "strong":
		return "*" + w.innerText(e) + "*" + e.tail
	case "eref":
		target := e.get("target")
		switch e.get("brackets") {
		case "none":
			if e.text != "" && target != "" {
				target = "(" + target + ")"
			}
		case "angle":
			target = "<" + target + ">"
		}
		text := target
		if e.text != "" && target != "" {
			text = e.text + " " + target
		} else if e.text != "" {
			text = e.text
		}
		return text + e.tail
	case "xref":
		return w.xrefText(e)
	}
	panic(fmt.Sprintf("no renderer for <%s>", e.tag))
}

// innerText renders the inline content of e.
func (w *writer) innerText(e *node) string {
	var b strings.Builder
	b.WriteString(e.text)
	for _, c := range e.children {
		b.WriteString(w.renderText(c, 0, context{}))
	}
	return trim(b.String())
}

func (w *writer) xrefText(e *node) string {
	target, section := e.get("target"), e.get("section")
	ref := trim(e.get("derivedContent"))
	own := w.innerText(e)
	if own != "" {
		own += " "
	}
	content := cleanText(e.allText())
	_, isReference := w.doc.refNames[target]

	var text string
	if section == "" {
		format := e.get("format")
		switch {
		case ref == "":
			text = trim(own)
		case format == "none":
			text = own
		case isReference && format == "title":
			text = ref
			if content != "" {
				text = own + "(" + ref + ")"
			}
		case isReference:
			text = own + "[" + ref + "]"
		case content != "":
			text = own + "(" + ref + ")"
		default:
			text = ref
		}
		// A table of contents entry carries its page number after an
		// ellipsis, which the list item later expands into dots.
		if p := e.get("pageno"); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				text += fmt.Sprintf("…%04d", n)
			}
		}
	} else {
		label := "Part"
		if isASCIIDigit(rune(section[0])) {
			label = "Section"
		} else if section[0] >= 'A' && section[0] <= 'Z' && (len(section) == 1 || section[1] == '.') {
			label = "Appendix"
		}
		switch e.get("sectionFormat") {
		case "of":
			text = fmt.Sprintf("%s %s of %s[%s]", label, section, own, ref)
		case "comma":
			text = fmt.Sprintf("%s[%s], %s %s", own, ref, label, section)
		case "parens":
			text = fmt.Sprintf("%s[%s] (%s %s)", own, ref, label, section)
		case "bare":
			text = section
			if own != "" && own != section {
				text = fmt.Sprintf("%s (%s)", section, trim(own))
			}
		}
	}
	// The display text of a reference is never broken at a hyphen.
	return strings.ReplaceAll(text, "-", string(nbHyphen)) + e.tail
}

// dateText renders the date of a reference or of the document.
func (w *writer) dateText(e *node) string {
	year, month, day, err := dateParts(e)
	if err != nil {
		w.doc.warnf("%v", err)
	}
	have := year != 0 || month != 0 || day != 0
	inReference := e.parent != nil && e.parent.parent != nil && e.parent.parent.tag == "reference"
	if !inReference {
		year, month, day = w.doc.year, w.doc.month, w.doc.day
	}
	date := formatDate(year, month, day)
	switch {
	case e.text != "" && have:
		return fmt.Sprintf("%s (%s)", e.text, date)
	case e.text != "":
		return e.text
	}
	return date
}

// joinChildren renders each child and joins it to the lines so far.
func (w *writer) joinChildren(lines []*line, children []*node, width int, c context) []*line {
	for _, child := range children {
		lines = w.ljoin(lines, child, width, c)
	}
	return lines
}

// ljoin renders e and appends it to lines, separated and indented as the
// joiner for its tag says.
func (w *writer) ljoin(lines []*line, e *node, width int, c context) []*line {
	j := c.joiner(e.tag)
	width -= j.indent
	c.hang = j.hang
	// Each element gets its own copy of the joiners, so that what it changes
	// for its children does not leak to its siblings.
	res := w.renderLines(e, width, c.with(nil))
	if len(lines) > 0 {
		for i := 0; i < strings.Count(j.join, "\n")-1; i++ {
			lines = append(lines, blankLine())
		}
	}
	resWidth := 0
	for _, l := range res {
		if n := runeLen(l.text); n > resWidth {
			resWidth = n
		}
	}
	indent := j.indent
	if e.outdent > 0 || (j.outdent && resWidth > width) {
		outdent := e.outdent
		if outdent == 0 {
			outdent = resWidth - width
		}
		if residue := outdent - indent; residue > 0 {
			e.parent.outdent = residue
		}
		indent -= min(indent, outdent)
		w.doc.warnf("%s too wide, reducing indentation from %d to %d",
			strings.ToUpper(e.tag[:1])+e.tag[1:], j.indent, indent)
	}
	for i, l := range res {
		if trim(l.text) == "" {
			continue
		}
		if i == 0 {
			l.text = spaces(indent+j.hang) + l.text
		} else {
			l.text = spaces(indent) + l.text
		}
	}
	if j.overlap && len(res) > 0 {
		first := res[0]
		res = res[1:]
		if trim(first.text) != "" {
			lines[len(lines)-1].text += j.join + ltrim(first.text)
		}
	}
	return append(lines, res...)
}

// tjoin renders e as text and appends it to text.
func (w *writer) tjoin(text string, e *node, width int, c context) string {
	j := c.joiner(e.tag)
	width -= j.indent + j.hang
	c.hang = j.hang
	rendered := w.renderText(e, width, c.with(nil))
	indented := indentText(rendered, j.indent, j.hang)
	switch {
	case text == "":
		return indented
	case strings.Contains(j.join, "\n"):
		return text + j.join + indented
	case trim(j.join) != "" && trim(indented) == "":
		// A separator such as ", " is not written before nothing.
		return text
	}
	return text + j.join + ltrim(indented)
}

func (w *writer) renderSection(e *node, width int, c context) []*line {
	c = c.with(map[string]joiner{
		"":           {join: "\n\n", indent: 3},
		"t":          {join: "\n\n", indent: 3},
		"name":       {join: "  "},
		"section":    {join: "\n\n"},
		"artwork":    {join: "\n\n", indent: 3, outdent: true},
		"sourcecode": {join: "\n\n", indent: 3},
	})
	text := ""
	if e.get("numbered") == "true" {
		pn := e.get("pn")
		text = sectionNumber(pn) + "."
		if isAppendix(pn) && !strings.Contains(sectionNumber(pn), ".") {
			text = "Appendix " + text
		}
		c = c.with(map[string]joiner{"name": {join: "  ", indent: runeLen(text) + 2}})
	}
	var lines []*line
	var rest []*node
	for _, child := range e.children {
		if child.tag == "name" {
			if lines == nil {
				lines = mkLines(w.tjoin(text, child, width, c), e)
			}
			continue
		}
		rest = append(rest, child)
	}
	return w.joinChildren(lines, rest, width, c)
}

func (w *writer) renderReferences(e *node, width int, c context) []*line {
	c = c.with(map[string]joiner{
		"":           {join: "\n\n", indent: 3},
		"name":       {join: "  "},
		"reference":  {join: "\n\n", indent: 3},
		"references": {join: "\n\n"},
	})
	var lines []*line
	var rest []*node
	for _, child := range e.children {
		if child.tag == "name" && lines == nil {
			text := ""
			if e.get("numbered") != "false" {
				text = sectionNumber(e.get("pn")) + "."
			}
			lines = mkLines(w.tjoin(text, child, width, c), e)
			continue
		}
		rest = append(rest, child)
	}
	return w.joinChildren(lines, rest, width, c)
}

// renderReference renders one bibliography entry: the label, then authors,
// title, series information, date and URL, separated by commas.
func (w *writer) renderReference(e *node, width int, c context) []*line {
	const labelWidth = 11
	label := "[" + w.doc.refNames[e.get("anchor")] + "]"
	label += spaces(labelWidth - runeLen(label))

	var parts []*node
	for _, tag := range []string{"title", "refcontent", "seriesInfo", "date"} {
		parts = append(parts, e.descendants(tag)...)
	}
	if target := e.get("target"); target != "" {
		url := textNode("refcontent", "<"+target+">")
		url.parent = e
		parts = append(parts, url)
	}

	c.joiners = map[string]joiner{"": {join: ", "}}
	width -= labelWidth
	text := referenceAuthors(e)
	for _, p := range parts {
		text = w.tjoin(text, p, width, c)
	}
	text += "."
	text = ltrim(fill(text, fillOptions{width: width, keepURL: true, noSentenceSpacing: true}))
	text = indentText(text, labelWidth, 0)
	if runeLen(trim(label)) > labelWidth-1 {
		// A label too long for its column gets a line of its own.
		label += "\n"
	} else {
		text = ltrim(text)
	}
	return mkLines(label+text, e)
}

// authorNameParts returns the initials and the surname of an author, derived
// from the full name when they are not given.
func authorNameParts(a *node) (initials, surname string) {
	_, hasInitials := a.attr["initials"]
	_, hasSurname := a.attr["surname"]
	if hasInitials && hasSurname {
		initials = a.get("initials")
		if initials != "" && !strings.HasSuffix(initials, ".") {
			initials += "."
		}
		return initials, a.get("surname")
	}
	words := strings.Fields(a.get("fullname"))
	if len(words) == 0 {
		return "", ""
	}
	var parts []string
	for _, word := range words[:len(words)-1] {
		r := []rune(word)
		if len(r) > 1 {
			parts = append(parts, strings.ToUpper(string(r[0]))+".")
		} else {
			parts = append(parts, strings.ToUpper(word))
		}
	}
	return strings.Join(parts, " "), words[len(words)-1]
}

func joinNonEmpty(sep string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

// shortName is an author's name as "J. Smith".
func shortName(a *node) string {
	initials, surname := authorNameParts(a)
	return joinNonEmpty(" ", initials, surname)
}

func orgName(a *node) string {
	org := a.find("organization")
	if org == nil {
		return ""
	}
	if abbrev := org.get("abbrev"); abbrev != "" {
		return abbrev
	}
	return org.text
}

// referenceAuthors lists the authors of a reference in the RFC Editor's
// style: "Smith, J., Jones, K., and L. Brown".
func referenceAuthors(e *node) string {
	authors := e.descendants("author")
	var b strings.Builder
	for i, a := range authors {
		last := i == len(authors)-1 && len(authors) > 1
		var name string
		if last {
			b.WriteString("and ")
			name = shortName(a)
		} else {
			initials, surname := authorNameParts(a)
			name = joinNonEmpty(", ", surname, initials)
		}
		if name == "" {
			name = orgName(a)
		}
		b.WriteString(name)
		if a.get("role") == "editor" {
			b.WriteString(", Ed.")
		}
		switch {
		case len(authors) == 2 && i == 0:
			b.WriteString(" ")
		case i < len(authors)-1:
			b.WriteString(", ")
		}
	}
	return b.String()
}

// listState is the numbering of a list while its items are rendered.
type listState struct {
	ordered bool
	counter int
	symbol  string
	bare    bool
	padding int
}

func (l *listState) marker() string {
	var text string
	switch {
	case l.ordered:
		text = strconv.Itoa(l.counter) + "."
		l.counter++
	case l.bare:
		return ""
	default:
		text = l.symbol
	}
	return text + spaces(l.padding-runeLen(text))
}

var listSymbols = []string{"*", "-", "o", "+"}

func listJoin(e *node) string {
	if e.get("spacing") == "compact" {
		return "\n"
	}
	return "\n\n"
}

func (w *writer) renderUL(e *node, width int, c context) []*line {
	empty := e.get("empty") == "true"
	state := &listState{bare: empty && e.get("bare") == "true"}
	depth := 0
	for p := e.parent; p != nil; p = p.parent {
		if p.tag == "ul" {
			depth++
		}
	}
	state.symbol = listSymbols[depth%len(listSymbols)]
	if empty {
		state.symbol = " "
	}
	padding := runeLen(state.symbol) + 2
	if state.bare && len(e.children) > 0 {
		// A bare list hangs its continuation lines under the text that
		// follows the first word of its last item, at most 8 columns in.
		e.list = state
		last := w.renderLines(e.children[len(e.children)-1], width, c.with(nil))
		if words := strings.FieldsFunc(joinLines(last, " "), isSpace); len(words) > 0 {
			padding = min(8, runeLen(words[0])+2)
		}
	}
	indent := padding
	if v, err := strconv.Atoi(e.get("indent")); err == nil {
		indent = v
	}
	hang := max(padding, indent) - indent
	state.padding = indent
	e.list = state

	join := listJoin(e)
	c = c.with(map[string]joiner{
		"":   {join: join, indent: indent},
		"li": {join: join},
		"t":  {join: join, indent: indent, hang: hang},
	})
	return w.joinChildren(nil, e.children, width, c)
}

func (w *writer) renderOL(e *node, width int, c context) []*line {
	start, err := strconv.Atoi(e.get("start"))
	if err != nil {
		start = 1
	}
	// The marker column is as wide as the largest number, its period and
	// two spaces, unless the list sets an indent.
	indent := len(strconv.Itoa(max(len(e.children), 1))) + 1 + 2
	if v, err := strconv.Atoi(e.get("indent")); err == nil {
		indent = v
	}
	e.list = &listState{ordered: true, counter: start, padding: indent}

	join := listJoin(e)
	c = c.with(map[string]joiner{
		"":   {join: join, indent: indent},
		"li": {join: join},
		"t":  {join: join, indent: indent},
	})
	return w.joinChildren(nil, e.children, width, c)
}

func (w *writer) renderLI(e *node, width int, c context) []*line {
	marker := e.parent.list.marker()
	var lines []*line
	if trim(e.text) != "" || hasInline(e) {
		// An item holding text directly is rendered as one paragraph.
		e.tag = "t"
		res := w.ljoin(nil, e, width, c)
		e.tag = "li"
		lines = mkLines(marker+ltrim(joinLines(res, "\n")), e)
	} else {
		lines = stripBlank(w.joinChildren(nil, e.children, width, c))
		if len(lines) > 0 {
			switch lines[0].elem.tag {
			case "artwork", "sourcecode", "li":
			default:
				lines[0].text = marker + ltrim(lines[0].text)
			}
		}
	}
	for _, l := range lines {
		l.text = expandEllipsis(l.text, width)
	}
	return lines
}

// hasInline reports whether e mixes text-level content with its blocks.
func hasInline(e *node) bool {
	for _, c := range e.children {
		switch c.tag {
		case "t", "ul", "ol", "artwork", "sourcecode":
			if trim(c.tail) != "" {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// expandEllipsis turns "title…0003" into "title . . . .   3", filling the
// line to the width with a dotted leader.
func expandEllipsis(text string, width int) string {
	i := strings.LastIndex(text, "…")
	if i < 0 {
		return text
	}
	head, tail := text[:i]+" ", text[i+len("…"):]
	if len(tail) == 0 || strings.TrimLeft(tail, "0123456789") != "" {
		return text
	}
	if tail != "0000" {
		tail = fmt.Sprintf("%4s", strings.TrimLeft(tail, "0"))
	}
	last := head
	if j := strings.LastIndex(head, "\n"); j >= 0 {
		last = head[j+1:]
	}
	leader := ""
	if lack := width - (runeLen(last) + runeLen(tail)); lack > 0 {
		dots := strings.Repeat(" .", 40)
		leader = dots[len(dots)-min(lack, len(dots)):]
	}
	return head + leader + tail
}

func (w *writer) renderArtwork(e *node, width int) []*line {
	text := strings.Trim(e.allText(), "\n")
	if trim(text) == "" {
		return nil
	}
	var lines []*line
	for _, l := range strings.Split(expandTabs(text), "\n") {
		lines = append(lines, &line{text: rtrim(l), elem: e})
	}
	if align := e.get("align"); align == "center" || align == "right" {
		widest := 0
		for _, l := range lines {
			widest = max(widest, runeLen(l.text))
		}
		if shift := width - widest; shift > 0 {
			if align == "center" {
				shift /= 2
			}
			for _, l := range lines {
				if trim(l.text) != "" {
					l.text = spaces(shift) + l.text
				}
			}
		}
	}
	return lines
}

// renderAuthor renders one entry of the Authors' Addresses section.
func (w *writer) renderAuthor(e *node, width int, c context) []*line {
	address := e.find("address")
	if address == nil {
		address = newNode("address")
		e.append(address)
	}
	if address.find("postal") == nil {
		address.insert(0, newNode("postal"))
	}
	c.joiners = map[string]joiner{"": {join: "\n"}}
	lines := stripBlank(w.ljoin(nil, address, width, c))
	return append(lines, blankLine())
}

// renderPostal renders the name and organization of an author. Postal
// addresses themselves are not supported.
func (w *writer) renderPostal(e *node, width int, c context) []*line {
	author := e.parent.parent
	name := author.get("fullname")
	if name == "" {
		name = shortName(author)
	}
	org := ""
	if o := author.find("organization"); o != nil {
		org = trim(o.text)
	}
	return mkLines(joinNonEmpty("\n", name, org), e)
}

func (w *writer) renderFront(e *node, width int, c context) []*line {
	lines := mkLines("\n\n\n\n"+w.firstPageTop(e, width)+"\n", e)
	for _, child := range e.children {
		switch child.tag {
		case "abstract", "note", "boilerplate", "toc":
			lines = append(lines, blankLine())
			lines = append(lines, w.renderLines(child, width, c.with(nil))...)
		}
	}
	return lines
}

// firstPageTop renders the header of the first page: the working group,
// status and expiry on the left, the authors and the date on the right, and
// the centred title below.
func (w *writer) firstPageTop(front *node, width int) string {
	root := w.doc.root

	var right []string
	prevOrg, prevHasOrg := "", false
	for _, a := range front.findAll("author") {
		name := shortName(a)
		org, hasOrg := "", false
		if o := a.find("organization"); o != nil {
			if o.get("showOnFrontPage") == "true" {
				org, hasOrg = orgName(a), true
			}
		} else {
			hasOrg = true
		}
		if org != "" && name == "" {
			name, org, hasOrg = org, "", false
		}
		if a.get("role") == "editor" {
			name += ", Ed."
		}
		// Consecutive authors from one organization share its line.
		if len(right) > 0 && name != "" && org != "" && org == prevOrg {
			right[len(right)-1] = name
			right = append(right, org)
		} else {
			if name != "" {
				right = append(right, name)
			}
			if hasOrg {
				right = append(right, org)
			}
		}
		prevOrg, prevHasOrg = org, hasOrg
	}
	// An author without an organization leaves an empty line below the
	// name; the last one must not, or the date would be set apart.
	if prevHasOrg && prevOrg == "" && len(right) > 0 {
		right = right[:len(right)-1]
	}
	right = append(right, formatDate(w.doc.year, w.doc.month, w.doc.day))

	var left []string
	for _, g := range front.findAll("workgroup") {
		if t := trim(g.text); t != "" {
			left = append(left, t)
		}
	}
	if len(left) == 0 {
		left = append(left, "Network Working Group")
	}
	left = append(left, "Internet-Draft")
	for _, rel := range []struct{ attr, label string }{{"obsoletes", "Obsoletes: "}, {"updates", "Updates: "}} {
		if v := root.get(rel.attr); v != "" {
			var nums []string
			for _, n := range strings.Split(v, ",") {
				if n = strings.TrimSpace(n); n != "" {
					nums = append(nums, n)
				}
			}
			left = append(left, rel.label+strings.Join(nums, ", ")+" (if approved)")
		}
	}
	left = append(left, "Intended status: "+categoryNames[root.get("category")])
	left = append(left, "Expires: "+w.doc.expiryString())

	var b strings.Builder
	for i := 0; i < max(len(left), len(right)); i++ {
		var l, r string
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		b.WriteString(l + spaces(maxWidth-runeLen(l)-runeLen(r)) + r + "\n")
	}
	header := rtrim(b.String()) + "\n\n\n"

	title := front.find("title")
	text := fill(trim(title.allText()), fillOptions{width: width})
	titleLines := strings.Split(text, string(lineSep))
	for _, l := range titleLines {
		if runeLen(l) > width {
			titleLines = wrap(text, wrapOptions{width: width - 4})
			break
		}
	}
	titleLines = append(titleLines, trim(root.get("docName")))
	for i, l := range titleLines {
		titleLines[i] = rtrim(center(l, width))
	}
	return header + strings.ReplaceAll(strings.Join(titleLines, "\n"), string(nbsp), " ")
}

// center pads s on both sides to the width, the way Python's str.center does.
func center(s string, width int) string {
	pad := width - runeLen(s)
	if pad <= 0 {
		return s
	}
	left := pad/2 + (pad & width & 1)
	return spaces(left) + s + spaces(pad-left)
}
