package rfc

import (
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// This file renders a prepared document as HTML with the same structure,
// ids and classes as xml2rfc's HTML, so that xml2rfc's stylesheet applies
// unchanged and links into a draft keep working.

// The stylesheet and scripts are xml2rfc's own; see assets/LICENSE.
var (
	//go:embed assets/xml2rfc.css
	stylesheet string
	//go:embed assets/metadata.min.js
	metadataScript string
	//go:embed assets/xml2rfc.js
	tocScript string
)

// Generator is written to the page's "generator" meta tag.
const Generator = "xml2html (github.com/aaomidi/draft-ietf-acme-scoped-dns-challenges)"

var (
	htmlText = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	htmlAttr = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
)

// htmlWriter accumulates the page.
type htmlWriter struct {
	doc *document
	b   strings.Builder
}

func (h *htmlWriter) write(parts ...string) {
	for _, p := range parts {
		h.b.WriteString(p)
	}
}

// tag returns an opening tag. Attributes are name, value pairs; one with an
// empty value is left out, and classes are sorted as xml2rfc sorts them.
func tag(name string, attrs ...string) string {
	var b strings.Builder
	b.WriteString("<" + name)
	for i := 0; i+1 < len(attrs); i += 2 {
		value := attrs[i+1]
		if value == "" {
			continue
		}
		if attrs[i] == "class" {
			classes := strings.Fields(value)
			sort.Strings(classes)
			value = strings.Join(classes, " ")
		}
		b.WriteString(" " + attrs[i] + `="` + htmlAttr.Replace(value) + `"`)
	}
	b.WriteString(">")
	return b.String()
}

// elementID is the HTML id of an element from its paragraph number. Back
// sections are "appendix-A" rather than "section-appendix.a".
func elementID(pn string) string {
	if !isAppendix(pn) {
		return pn
	}
	num, para, _ := strings.Cut(strings.TrimPrefix(pn, "section-appendix."), "-")
	id := "appendix-" + strings.ToUpper(num)
	if para != "" {
		id += "-" + para
	}
	return id
}

func pilcrow(id string) string {
	return tag("a", "href", "#"+id, "class", "pilcrow") + "¶</a>"
}

func (d *document) html(opts HTMLOptions) string {
	h := &htmlWriter{doc: d}
	root := d.root
	front := root.find("front")
	title := cleanText(strings.Join(front.find("title").textPieces(), " "))

	h.write("<!DOCTYPE html>\n", tag("html", "lang", "en", "class", "Internet-Draft"), "\n<head>\n",
		`<meta charset="utf-8">`, "\n",
		`<meta content="Common,Latin" name="scripts">`, "\n",
		`<meta content="initial-scale=1.0" name="viewport">`, "\n",
		"<title>", htmlText.Replace(title), "</title>\n")
	meta := func(name, content string) {
		h.write(tag("meta", "content", content, "name", name), "\n")
	}
	for _, a := range front.findAll("author") {
		if name := fullName(a); name != "" {
			meta("author", name)
		}
	}
	meta("description", strings.Join(front.find("abstract").textPieces(), " "))
	meta("generator", Generator)
	for _, k := range front.findAll("keyword") {
		meta("keyword", trim(k.text))
	}
	meta("ietf.draft", root.get("docName"))
	if opts.XMLLink != "" {
		h.write(tag("link", "href", opts.XMLLink, "rel", "alternate", "type", "application/rfc+xml"), "\n")
	}
	h.write(`<link href="#copyright" rel="license">`, "\n",
		`<style type="text/css">`, stylesheet, "</style>\n",
		// A place for a site to add its own styles, as xml2rfc leaves one.
		`<link href="rfc-local.css" rel="stylesheet" type="text/css">`, "\n",
		`<script type="application/javascript">`, metadataScript, "</script>\n",
		"</head>\n", `<body class="xml2rfc">`, "\n")

	h.front(front, title)
	for _, s := range root.find("middle").children {
		h.block(s, "")
	}
	for _, s := range root.find("back").children {
		h.block(s, "")
	}
	h.write("<script>", tocScript, "</script>\n</body>\n</html>\n")
	return h.b.String()
}

func fullName(a *node) string {
	if name := a.get("fullname"); name != "" {
		return name
	}
	return shortName(a)
}

// front renders everything above the first section: the running header that
// shows when printed, the document information, the title, and the front
// matter sections.
func (h *htmlWriter) front(front *node, title string) {
	d := h.doc
	root := d.root
	short := front.find("title").get("abbrev")
	if short == "" {
		short = title
	}
	w := &writer{doc: d}
	expires := d.expiryDate()
	published := formatDate(d.year, d.month, d.day)

	cell := func(class, text string) string {
		return tag("td", "class", class) + htmlText.Replace(text) + "</td>\n"
	}
	h.write(`<table class="ears">`, "\n<thead><tr>\n",
		cell("left", "Internet-Draft"), cell("center", short),
		cell("right", fmt.Sprintf("%s %d", monthNames[d.month], d.year)),
		"</tr></thead>\n<tfoot><tr>\n",
		cell("left", w.footerAuthors()), cell("center", "Expires "+d.expiryString()), cell("right", "[Page]"),
		"</tr></tfoot>\n</table>\n",
		`<div id="external-metadata" class="document-information"></div>`, "\n",
		`<div id="internal-metadata" class="document-information">`, "\n", `<dl id="identifiers">`, "\n")

	// entry writes one line of the document information. The value is
	// already HTML.
	entry := func(label, value string) {
		class := strings.ReplaceAll(strings.ToLower(label), " ", "-")
		h.write(tag("dt", "class", "label-"+class), label, ":</dt>\n", tag("dd", "class", class), value, "</dd>\n")
	}
	hasGroup := false
	for _, g := range front.findAll("workgroup") {
		if t := trim(g.text); t != "" {
			entry("Workgroup", htmlText.Replace(t))
			hasGroup = true
		}
	}
	if !hasGroup {
		entry("Workgroup", "Network Working Group")
	}
	for _, si := range front.findAll("seriesInfo") {
		entry(si.get("name"), htmlText.Replace(si.get("value")))
	}
	for _, rel := range []struct{ attr, label string }{{"obsoletes", "Obsoletes"}, {"updates", "Updates"}} {
		if v := root.get(rel.attr); v != "" {
			var links []string
			for _, n := range strings.Split(v, ",") {
				if n = strings.TrimSpace(n); n != "" {
					links = append(links, tag("a", "href", "https://www.rfc-editor.org/rfc/rfc"+n, "class", "eref")+htmlText.Replace(n)+"</a>")
				}
			}
			entry(rel.label, strings.Join(links, ", ")+" (if approved)")
		}
	}
	iso := func(year, month, day int) string {
		if day == 0 {
			return fmt.Sprintf("%04d-%02d", year, month)
		}
		return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
	}
	entry("Published", tag("time", "datetime", iso(d.year, d.month, d.day), "class", "published")+published+"</time>")
	entry("Intended Status", categoryNames[root.get("category")])
	entry("Expires", tag("time", "datetime", iso(expires.Year(), int(expires.Month()), expires.Day()))+d.expiryString()+"</time>")

	authors := front.findAll("author")
	var list strings.Builder
	for _, a := range authors {
		list.WriteString(`<div class="author">` + "\n")
		name := shortName(a)
		if a.get("role") == "editor" {
			name += ", Ed."
		}
		if name != "" {
			list.WriteString(`<div class="author-name">` + htmlText.Replace(name) + "</div>\n")
		}
		if o := a.find("organization"); o != nil && o.get("showOnFrontPage") == "true" && orgName(a) != "" {
			list.WriteString(`<div class="org">` + htmlText.Replace(orgName(a)) + "</div>\n")
		}
		list.WriteString("</div>\n")
	}
	label := "Authors"
	if len(authors) == 1 {
		label = "Author"
	}
	h.write(tag("dt", "class", "label-authors"), label, ":</dt>\n", tag("dd", "class", "authors"), "\n", list.String(), "</dd>\n")
	h.write("</dl>\n</div>\n", `<h1 id="title">`, htmlText.Replace(title), "</h1>\n")

	for _, c := range front.children {
		switch c.tag {
		case "abstract":
			h.write(tag("section", "id", c.get("pn")), "\n",
				`<h2 id="abstract"><a href="#abstract" class="selfRef">Abstract</a></h2>`, "\n")
			h.blocks(c.children, "")
			h.write("</section>\n")
		case "note":
			class := "note"
			if c.get("removeInRFC") == "true" {
				class += " rfcEditorRemove"
			}
			h.write(tag("section", "class", class, "id", c.get("pn")), "\n")
			h.blocks(c.children, "")
			h.write("</section>\n")
		case "boilerplate", "toc":
			h.blocks(c.children, "")
		}
	}
}

func (h *htmlWriter) blocks(children []*node, parentClass string) {
	for _, c := range children {
		h.block(c, parentClass)
	}
}

// block renders a block element. The class of the enclosing HTML element is
// passed down because lists inherit it.
func (h *htmlWriter) block(e *node, parentClass string) {
	id := elementID(e.get("pn"))
	switch e.tag {
	case "section", "references":
		// A section with an anchor is wrapped so that both its anchor and
		// its number can be linked to.
		anchor := e.get("anchor")
		if anchor != "" {
			h.write(tag("div", "id", anchor), "\n")
		}
		h.write(tag("section", "id", id), "\n")
		if anchor == "toc" {
			h.write(`<a href="#" onclick="scroll(0,0)" class="toplink">`, "▲</a>")
		}
		inList := false
		for _, c := range e.children {
			if c.tag == "reference" && !inList {
				h.write(`<dl class="references">`, "\n")
				inList = true
			}
			h.block(c, "")
		}
		if inList {
			h.write("</dl>\n")
		}
		h.write("</section>\n")
		if anchor != "" {
			h.write("</div>\n")
		}

	case "name":
		h.heading(e)

	case "t":
		class := ""
		if e.get("keepWithNext") == "true" {
			class = "keepWithNext"
		}
		h.write(tag("p", "id", id, "class", class), h.withPilcrow(h.inline(e), id, e), "</p>\n")

	case "ul":
		// A list takes the classes of what it is in, except that being
		// empty is not inherited.
		classes := map[string]bool{}
		for _, c := range strings.Fields(parentClass) {
			if c != "ulEmpty" {
				classes[c] = true
			}
		}
		inTOC := e.parent.get("anchor") == "toc"
		if inTOC {
			classes["toc"] = true
		}
		if e.get("empty") == "true" {
			classes["ulEmpty"] = true
			if e.get("bare") == "true" {
				classes["ulBare"] = true
			}
		}
		if s := e.get("spacing"); s != "" {
			classes[s] = true
		}
		var list []string
		for c := range classes {
			list = append(list, c)
		}
		class := strings.Join(list, " ")
		if inTOC {
			h.write(`<nav class="toc">`)
		}
		h.write(tag("ul", "class", class), "\n")
		for _, li := range e.children {
			h.item(li, class)
		}
		h.write("</ul>\n")
		if inTOC {
			h.write("</nav>\n")
		}

	case "ol":
		h.write(tag("ol", "start", e.get("start"), "type", e.get("type"),
			"class", e.get("spacing")+" type-"+e.get("type"), "id", id), "\n")
		for _, li := range e.children {
			h.item(li, "")
		}
		h.write("</ol>\n")

	case "artwork", "sourcecode":
		class := "artwork art-text align" + strings.ToUpper(e.get("align")[:1]) + e.get("align")[1:]
		if e.tag == "sourcecode" {
			class = "sourcecode"
			if t := e.get("type"); t != "" {
				class += " lang-" + t
			}
			if strings.Count(e.text, "\n") >= 50 {
				class += " breakable"
			}
		}
		h.write(tag("div", "class", class, "id", id), "\n<pre>", htmlText.Replace(expandTabs(e.allText())), "</pre>")
		if trim(e.allText()) != "" {
			h.write(pilcrow(id))
		}
		h.write("\n</div>\n")

	case "reference":
		h.reference(e)

	case "author":
		h.address(e)

	default:
		panic(fmt.Sprintf("no HTML renderer for <%s>", e.tag))
	}
}

// item renders a list item.
func (h *htmlWriter) item(li *node, class string) {
	id := elementID(li.get("pn"))
	h.write(tag("li", "class", class, "id", id), "\n")
	if trim(li.text) != "" {
		// An item that holds text directly, rather than paragraphs.
		h.write(h.withPilcrow(h.inline(li), id, li), "\n")
	} else {
		h.blocks(li.children, class)
	}
	h.write("</li>\n")
}

// withPilcrow appends the paragraph mark that links to a block, unless the
// block is empty or is part of the table of contents.
func (h *htmlWriter) withPilcrow(content, id string, e *node) string {
	for p := e; p != nil; p = p.parent {
		if p.get("anchor") == "toc" {
			return content
		}
	}
	content = strings.TrimRightFunc(content, isSpace)
	if id == "" || content == "" {
		return content
	}
	return content + pilcrow(id)
}

// heading renders the title of a section as a heading that links to itself,
// by number and by name.
func (h *htmlWriter) heading(name *node) {
	section := name.parent
	pn := section.get("pn")
	num := sectionNumber(pn)
	level := min(6, strings.Count(num, ".")+2)
	slug := name.get("slugifiedName")

	hn := "h" + strconv.Itoa(level)
	h.write(tag(hn, "id", slug), "\n")
	if section.get("numbered") == "true" {
		label := num + "."
		if isAppendix(pn) && !strings.Contains(num, ".") {
			label = "Appendix " + label
		}
		h.write(tag("a", "href", "#"+elementID(pn), "class", "section-number selfRef"), label, " </a>")
	}
	h.write(tag("a", "href", "#"+slug, "class", "section-name selfRef"), trim(h.inlineIn(name, true)), "</a>\n</"+hn+">\n")
}

// inline renders the text and inline elements inside e.
func (h *htmlWriter) inline(e *node) string { return h.inlineIn(e, false) }

func (h *htmlWriter) inlineIn(e *node, inName bool) string {
	var b strings.Builder
	b.WriteString(htmlText.Replace(e.text))
	for _, c := range e.children {
		b.WriteString(h.inlineElement(c, inName))
		b.WriteString(htmlText.Replace(c.tail))
	}
	return b.String()
}

func (h *htmlWriter) inlineElement(e *node, inName bool) string {
	wrap := func(name, class string) string {
		return tag(name, "class", class) + h.inlineIn(e, inName) + "</" + name + ">"
	}
	switch e.tag {
	case "tt":
		return wrap("code", "")
	case "em":
		return wrap("em", "")
	case "strong":
		return wrap("strong", "")
	case "bcp14":
		return wrap("span", "bcp14")
	case "eref":
		target := e.get("target")
		link := tag("a", "href", target)
		switch {
		case e.text != "":
			return link + htmlText.Replace(e.text) + "</a>"
		case e.get("brackets") == "angle":
			return "<span>&lt;" + link + htmlText.Replace(target) + "</a>&gt;</span>"
		}
		return "<span>" + link + htmlText.Replace(target) + "</a></span>"
	case "xref":
		return h.xref(e, inName)
	}
	panic(fmt.Sprintf("no HTML renderer for <%s>", e.tag))
}

// xref renders a cross-reference: a citation in brackets, a link to a
// section, or a link to a section of a cited document.
func (h *htmlWriter) xref(e *node, inName bool) string {
	target, format := e.get("target"), e.get("format")
	ref := htmlText.Replace(e.get("derivedContent"))
	own := strings.TrimSpace(htmlText.Replace(e.text))
	_, isReference := h.doc.refNames[target]
	cite := func() string {
		return tag("a", "href", "#"+target, "class", "xref cite") + ref + "</a>"
	}

	if section := e.get("section"); section != "" {
		label := "Part"
		if isASCIIDigit(rune(section[0])) {
			label = "Section"
		} else if section[0] >= 'A' && section[0] <= 'Z' && (len(section) == 1 || section[1] == '.') {
			label = "Appendix"
		}
		part := tag("a", "href", e.get("derivedLink"), "class", "relref") + label + " " + htmlText.Replace(section) + "</a>"
		if own != "" {
			own += " "
		}
		switch e.get("sectionFormat") {
		case "of":
			return "<span>" + part + " of " + own + "[" + cite() + "]</span>"
		case "comma":
			return "<span>" + own + "[" + cite() + "], " + part + "</span>"
		case "parens":
			return "<span>" + own + "[" + cite() + "] (" + part + ")</span>"
		}
		panic(fmt.Sprintf("no HTML renderer for <xref sectionFormat=%q>", e.get("sectionFormat")))
	}

	// A target given as a section number is linked by its HTML id.
	if isAppendix(target) {
		target = elementID(target)
	}
	if inName {
		// A heading is itself a link, so a reference in one is plain text.
		text := ref
		switch {
		case format == "none":
			text = own
		case isReference && format != "title":
			text = "[" + ref + "]"
			if own != "" {
				text = own + " " + text
			}
		case own != "":
			text = own + " (" + ref + ")"
		}
		return `<span class="cite xref">` + text + "</span>"
	}

	class := "xref internal"
	if own == "" && format != "title" {
		class += " auto"
	}
	link := func(class, text string) string {
		return tag("a", "href", "#"+target, "class", class) + text + "</a>"
	}
	switch {
	case ref == "":
		return link(class, own)
	case format == "none":
		return link("xref plain", own)
	case isReference && format != "title":
		if own != "" {
			return "<span>" + link(class, own) + " [" + cite() + "]</span>"
		}
		return "<span>[" + cite() + "]</span>"
	case own != "":
		refClass := class
		if format != "title" {
			refClass += " auto"
		}
		return "<span>" + link(class, own) + " (" + link(refClass, ref) + ")</span>"
	}
	return link(class, ref)
}

// reference renders one bibliography entry as a term and its description.
func (h *htmlWriter) reference(e *node) {
	anchor := e.get("anchor")
	h.write(tag("dt", "id", anchor), "[", htmlText.Replace(h.doc.refNames[anchor]), "]</dt>\n<dd>\n")

	var parts []string
	// Authors read "Smith, J., Jones, K., and L. Brown".
	var authors strings.Builder
	list := e.descendants("author")
	var named []*node
	for _, a := range list {
		if referenceAuthorName(a, false) != "" {
			named = append(named, a)
		}
	}
	for i, a := range named {
		last := i == len(named)-1
		name := referenceAuthorName(a, last && i > 0)
		if a.get("role") == "editor" {
			name += ", Ed."
		}
		authors.WriteString(`<span class="refAuthor">` + htmlText.Replace(name) + "</span>")
		switch {
		case last:
		case i == len(named)-2 && i == 0:
			authors.WriteString(" and ")
		case i == len(named)-2:
			authors.WriteString(", and ")
		default:
			authors.WriteString(", ")
		}
	}
	if authors.Len() > 0 {
		parts = append(parts, authors.String())
	}

	for _, t := range e.descendants("title") {
		title := cleanText(t.allText())
		if e.get("quoteTitle") == "true" {
			title = `"` + title + `"`
		}
		if title != "" {
			parts = append(parts, `<span class="refTitle">`+htmlText.Replace(title)+"</span>")
		}
	}
	for _, c := range e.descendants("refcontent") {
		parts = append(parts, `<span class="refContent">`+h.inline(c)+"</span>")
	}
	for _, s := range e.descendants("seriesInfo") {
		sep := " "
		if s.get("name") == "Internet-Draft" {
			sep = ", "
		}
		parts = append(parts, `<span class="seriesInfo">`+htmlText.Replace(s.get("name")+sep+s.get("value"))+"</span>")
	}
	for _, d := range e.descendants("date") {
		year, month, day, err := dateParts(d)
		if err != nil {
			h.doc.warnf("%v", err)
		}
		text := formatDate(year, month, day)
		switch {
		case year == 0 && month == 0 && day == 0:
			if d.text != "" {
				parts = append(parts, "<span>"+htmlText.Replace(d.text)+"</span>")
			}
			continue
		case d.text != "":
			text = fmt.Sprintf("%s (%s)", d.text, text)
		}
		iso := fmt.Sprintf("%04d", year)
		if month != 0 {
			iso += fmt.Sprintf("-%02d", month)
			if day != 0 {
				iso += fmt.Sprintf("-%02d", day)
			}
		}
		parts = append(parts, tag("time", "datetime", iso, "class", "refDate")+htmlText.Replace(text)+"</time>")
	}
	if target := e.get("target"); target != "" {
		parts = append(parts, "<span>&lt;"+tag("a", "href", target)+htmlText.Replace(target)+"</a>&gt;</span>")
	}
	h.write(strings.Join(parts, ", "), ". </dd>\n", `<dd class="break"></dd>`, "\n")
}

// referenceAuthorName is an author's name in a reference: "Smith, J.", or
// "J. Smith" for the last of several, or the organization for a body.
func referenceAuthorName(a *node, last bool) string {
	initials, surname := authorNameParts(a)
	name := joinNonEmpty(", ", surname, initials)
	if last {
		name = joinNonEmpty(" ", initials, surname)
	}
	if name == "" {
		name = orgName(a)
	}
	return name
}

// address renders an entry of the Authors' Addresses section.
func (h *htmlWriter) address(a *node) {
	line := func(class, text string) {
		h.write(`<div dir="auto" class="left">`, tag("span", "class", class), htmlText.Replace(text), "</span></div>\n")
	}
	h.write(`<address class="vcard">`, "\n")
	if name := fullName(a); name != "" {
		line("fn nameRole", name)
	}
	if o := a.find("organization"); o != nil && trim(o.text) != "" {
		line("org", trim(o.text))
	}
	if addr := a.find("address"); addr != nil {
		var emails []string
		for _, e := range addr.findAll("email") {
			if v := trim(e.text); v != "" {
				emails = append(emails, tag("a", "href", "mailto:"+v, "class", "email")+htmlText.Replace(v)+"</a>")
			}
		}
		if len(emails) > 0 {
			h.write(`<div class="email">`, "\n<span>Email:</span>\n", strings.Join(emails, ", "), "\n</div>\n")
		}
	}
	h.write("</address>\n")
}
