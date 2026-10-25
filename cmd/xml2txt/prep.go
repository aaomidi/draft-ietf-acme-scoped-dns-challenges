package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file does what xml2rfc's "preptool" does before rendering: it fills in
// everything the author leaves implicit. Sections get numbers, the
// boilerplate and the table of contents are generated, references are sorted
// and given URLs, and cross-references get their display text.

// document is a prepared RFCXML document, ready to render.
type document struct {
	root *node
	// today is the build date. It supplies missing parts of the document
	// date and the copyright year.
	today time.Time
	// date is the document date.
	year, month, day int
	// toc is the generated table of contents section, or nil.
	toc *node
	// refNames maps a reference anchor to the name it is cited by.
	refNames map[string]string
	// byID finds elements by anchor, section number id or name slug.
	byID map[string]*node

	warnings []string
}

func (d *document) warnf(format string, args ...any) {
	d.warnings = append(d.warnings, fmt.Sprintf(format, args...))
}

var monthNames = []string{"", "January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December"}

// parseMonth accepts a month number, name or three-letter abbreviation.
func parseMonth(s string) (int, error) {
	if n, err := strconv.Atoi(s); err == nil {
		if n < 1 || n > 12 {
			return 0, fmt.Errorf("month %q is out of range", s)
		}
		return n, nil
	}
	for i, name := range monthNames[1:] {
		if strings.EqualFold(s, name) || (len(s) >= 3 && strings.EqualFold(s, name[:3])) {
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("unknown month %q", s)
}

// dateParts reads the year, month and day attributes of a <date>. A part
// that is absent is zero.
func dateParts(e *node) (year, month, day int, err error) {
	if v := e.get("year"); v != "" {
		if year, err = strconv.Atoi(v); err != nil {
			return 0, 0, 0, fmt.Errorf("date: year %q is not a number", v)
		}
	}
	if v := e.get("month"); v != "" {
		if month, err = parseMonth(v); err != nil {
			return 0, 0, 0, fmt.Errorf("date: %v", err)
		}
	}
	if v := e.get("day"); v != "" {
		if day, err = strconv.Atoi(v); err != nil {
			return 0, 0, 0, fmt.Errorf("date: day %q is not a number", v)
		}
	}
	return year, month, day, nil
}

// formatDate writes a date as "25 October 2026", leaving out absent parts.
func formatDate(year, month, day int) string {
	switch {
	case month != 0 && day != 0:
		return fmt.Sprintf("%d %s %d", day, monthNames[month], year)
	case month != 0:
		return fmt.Sprintf("%s %d", monthNames[month], year)
	case year != 0:
		return strconv.Itoa(year)
	}
	return ""
}

// expiryDate is 185 days after the document date, as for any Internet-Draft.
func (d *document) expiryDate() time.Time {
	day := d.day
	if day == 0 {
		doc, now := d.year*12+d.month, d.today.Year()*12+int(d.today.Month())
		switch {
		case doc < now:
			day = time.Date(d.year, time.Month(d.month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
		case doc == now:
			day = d.today.Day()
		default:
			// xml2rfc uses the weekday number of the first of the month
			// here, which looks unintended; a first-of-month default is
			// what this renders.
			day = 1
		}
	}
	return time.Date(d.year, time.Month(d.month), day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 185)
}

func (d *document) expiryString() string {
	e := d.expiryDate()
	return formatDate(e.Year(), int(e.Month()), e.Day())
}

var categoryNames = map[string]string{
	"std":      "Standards Track",
	"bcp":      "Best Current Practice",
	"exp":      "Experimental",
	"info":     "Informational",
	"historic": "Historic",
}

// prepare turns a parsed document into one that can be rendered.
func prepare(root *node, today time.Time) (*document, error) {
	d := &document{root: root, today: today, refNames: map[string]string{}, byID: map[string]*node{}}

	front, middle := root.find("front"), root.find("middle")
	if front == nil || middle == nil {
		return nil, fmt.Errorf("<rfc> needs a <front> and a <middle>")
	}
	if front.find("title") == nil || front.find("author") == nil || front.find("abstract") == nil {
		return nil, fmt.Errorf("<front> needs a <title>, an <author> and an <abstract>")
	}
	back := root.find("back")
	if back == nil {
		back = newNode("back")
		root.append(back)
	}

	// Only what an IETF stream Internet-Draft needs is implemented; the
	// boilerplate differs for everything else.
	if ipr := root.get("ipr"); ipr != "trust200902" {
		return nil, fmt.Errorf(`ipr=%q is not supported, only "trust200902"`, ipr)
	}
	if st := root.get("submissionType"); st != "IETF" {
		return nil, fmt.Errorf(`submissionType=%q is not supported, only "IETF"`, st)
	}
	if _, ok := categoryNames[root.get("category")]; !ok {
		return nil, fmt.Errorf("category=%q is not supported", root.get("category"))
	}
	if root.get("symRefs") == "false" {
		return nil, fmt.Errorf(`symRefs="false" is not supported`)
	}
	for _, si := range front.findAll("seriesInfo") {
		if si.get("name") == "RFC" {
			return nil, fmt.Errorf("rendering a published RFC is not supported, only Internet-Drafts")
		}
	}

	if err := d.prepareDate(front); err != nil {
		return nil, err
	}
	for _, e := range root.descendants("reference") {
		d.refNames[e.get("anchor")] = e.get("anchor")
	}

	d.setDefaults(root)
	for _, ol := range root.descendants("ol") {
		if ol.get("type") != "1" {
			return nil, fmt.Errorf(`<ol type=%q> is not supported, only type="1"`, ol.get("type"))
		}
	}
	d.addRemovalNotes(root)
	d.addBoilerplate(front)
	d.addAuthorsSection(front, back)
	if err := d.numberSections(middle, back); err != nil {
		return nil, err
	}
	d.prepareReferences(back)
	if err := d.resolveXrefs(root); err != nil {
		return nil, err
	}
	// The table of contents is built last, from finished titles, and is
	// placed after the boilerplate.
	if root.getDefault("tocInclude", "true") == "true" {
		d.addTOC(front)
	}
	return d, nil
}

func (d *document) prepareDate(front *node) error {
	date := front.find("date")
	if date == nil {
		date = newNode("date")
		front.append(date)
	}
	var err error
	if d.year, d.month, d.day, err = dateParts(date); err != nil {
		return err
	}
	if d.year == 0 {
		d.year = d.today.Year()
	}
	if d.month == 0 {
		if d.year != d.today.Year() {
			return fmt.Errorf("date: a year other than the current one needs a month")
		}
		d.month = int(d.today.Month())
	}
	return nil
}

// setDefaults writes the default values of the attributes that rendering
// depends on.
func (d *document) setDefaults(root *node) {
	set := func(e *node, name, value string) {
		if _, ok := e.attr[name]; !ok {
			e.attr[name] = value
		}
	}
	var walk func(*node)
	walk = func(e *node) {
		switch e.tag {
		case "ul":
			set(e, "indent", "3")
			set(e, "spacing", "normal")
		case "ol":
			set(e, "indent", "adaptive")
			set(e, "spacing", "normal")
			set(e, "type", "1")
			set(e, "start", "1")
		case "section":
			set(e, "numbered", "true")
			set(e, "toc", "include")
		case "artwork", "sourcecode":
			set(e, "align", "left")
		case "eref":
			set(e, "brackets", "none")
		case "reference":
			set(e, "quoteTitle", "true")
		case "organization":
			set(e, "showOnFrontPage", "true")
		}
		for _, c := range e.children {
			walk(c)
		}
	}
	walk(root)
}

// addRemovalNotes adds the standard first paragraph to notes and sections
// marked removeInRFC.
func (d *document) addRemovalNotes(root *node) {
	for _, tag := range []string{"note", "section"} {
		for _, e := range root.descendants(tag) {
			if e.get("removeInRFC") != "true" {
				continue
			}
			pos := 0
			if len(e.children) > 0 && e.children[0].tag == "name" {
				pos = 1
			}
			e.insert(pos, textNode("t", fmt.Sprintf("This %s is to be removed before publishing as an RFC.", tag)))
		}
	}
}

// addBoilerplate adds the "Status of This Memo" and "Copyright Notice"
// sections every IETF stream Internet-Draft carries.
func (d *document) addBoilerplate(front *node) {
	section := func(anchor, title string, paragraphs ...*node) *node {
		s := newNode("section", "anchor", anchor, "numbered", "false", "toc", "exclude")
		s.append(textNode("name", title))
		return s.append(paragraphs...)
	}
	// para builds a paragraph from text with one optional link in it.
	para := func(before, url, after string) *node {
		t := textNode("t", before)
		if url != "" {
			link := newNode("eref", "target", url, "brackets", "none")
			link.tail = after
			t.append(link)
		}
		return t
	}

	status := section("status-of-memo", "Status of This Memo",
		para("This Internet-Draft is submitted in full conformance with the provisions of BCP 78 and BCP 79.", "", ""),
		para("Internet-Drafts are working documents of the Internet Engineering Task Force (IETF). "+
			"Note that other groups may also distribute working documents as Internet-Drafts. "+
			"The list of current Internet-Drafts is at ", "https://datatracker.ietf.org/drafts/current/", "."),
		para("Internet-Drafts are draft documents valid for a maximum of six months and may be updated, "+
			"replaced, or obsoleted by other documents at any time. It is inappropriate to use "+
			"Internet-Drafts as reference material or to cite them other than as \"work in progress.\"", "", ""),
		para("This Internet-Draft will expire on "+d.expiryString()+".", "", ""),
	)
	copyright := section("copyright", "Copyright Notice",
		para(fmt.Sprintf("Copyright (c) %d IETF Trust and the persons identified as the document authors. "+
			"All rights reserved.", d.today.Year()), "", ""),
		para("This document is subject to BCP 78 and the IETF Trust's Legal Provisions Relating to IETF Documents (",
			"https://trustee.ietf.org/license-info",
			") in effect on the date of publication of this document. Please review these documents carefully, "+
				"as they describe your rights and restrictions with respect to this document. "+
				"Code Components extracted from this document must include Revised BSD License text as described "+
				"in Section 4.e of the Trust Legal Provisions and are provided without warranty as described in "+
				"the Revised BSD License."),
	)
	front.append(newNode("boilerplate").append(status, copyright))
}

// addAuthorsSection appends the "Authors' Addresses" section.
func (d *document) addAuthorsSection(front, back *node) {
	authors := front.findAll("author")
	title := "Authors' Addresses"
	if len(authors) == 1 {
		title = "Author's Address"
	}
	s := newNode("section", "anchor", "authors-addresses", "numbered", "false", "toc", "include")
	s.append(textNode("name", title))
	for _, a := range authors {
		s.append(a.copyOf())
	}
	back.append(s)
}

// numberSections gives every section its number, as the "pn" attribute.
// Middle sections are numbered 1, 2, ...; the references follow on; numbered
// back sections are appendices A, B, ...
func (d *document) numberSections(middle, back *node) error {
	var number func(e *node, prefix string)
	number = func(e *node, prefix string) {
		e.attr["pn"] = "section-" + prefix
		i := 0
		for _, c := range e.children {
			if c.tag == "section" || c.tag == "references" {
				i++
				number(c, prefix+"."+strconv.Itoa(i))
			}
		}
	}

	sections := middle.findAll("section")
	numbered := true
	for i, s := range sections {
		if s.get("numbered") == "false" {
			numbered = false
		} else if !numbered {
			return fmt.Errorf("section %q is numbered but follows an unnumbered section", s.get("anchor"))
		}
		number(s, strconv.Itoa(i+1))
	}
	next := len(sections) + 1
	for _, r := range back.findAll("references") {
		// The references are numbered like the last middle section.
		r.attr["numbered"] = strconv.FormatBool(numbered)
		number(r, strconv.Itoa(next))
		next++
	}

	// Back sections share one sequence of letters, whether or not they
	// show their number.
	for i, s := range back.findAll("section") {
		if i >= 26 {
			return fmt.Errorf("more than 26 sections in <back> are not supported")
		}
		number(s, "appendix."+string(rune('a'+i)))
	}

	// A section nested in an unnumbered one is unnumbered too.
	var inherit func(e *node, off bool)
	inherit = func(e *node, off bool) {
		if e.tag == "section" {
			if off {
				e.attr["numbered"] = "false"
			}
			off = e.get("numbered") == "false"
		}
		for _, c := range e.children {
			inherit(c, off)
		}
	}
	inherit(middle, false)
	inherit(back, false)
	return nil
}

// sectionNumber returns the display number of a section from its pn, such as
// "3.1" or "A.2".
func sectionNumber(pn string) string {
	num := strings.TrimPrefix(pn, "section-")
	for _, p := range []string{"appendix.", "boilerplate.", "note.", "toc."} {
		num = strings.TrimPrefix(num, p)
	}
	return strings.ToUpper(num)
}

func isAppendix(pn string) bool { return strings.HasPrefix(pn, "section-appendix.") }

// prepareReferences sorts the references and completes them.
func (d *document) prepareReferences(back *node) {
	sortRefs := d.root.getDefault("sortRefs", "true") == "true"
	for _, refs := range back.descendants("references") {
		if sortRefs {
			var others, list []*node
			for _, c := range refs.children {
				if c.tag == "reference" {
					list = append(list, c)
				} else {
					others = append(others, c)
				}
			}
			sort.SliceStable(list, func(i, j int) bool {
				return naturalLess(strings.ToUpper(list[i].get("anchor")), strings.ToUpper(list[j].get("anchor")))
			})
			refs.children = append(others, list...)
		}
	}
	for _, ref := range back.descendants("reference") {
		series := ref.descendants("seriesInfo")
		// A reference without a URL gets the canonical one for its series.
		if ref.get("target") == "" {
			for _, s := range series {
				base, ok := map[string]string{
					"RFC":            "https://www.rfc-editor.org/rfc/rfc",
					"Internet-Draft": "https://datatracker.ietf.org/doc/html/",
					"DOI":            "https://doi.org/",
				}[s.get("name")]
				if ok && s.get("value") != "" {
					ref.attr["target"] = base + s.get("value")
					break
				}
			}
		}
		for _, s := range series {
			if s.get("name") == "Internet-Draft" {
				ref.append(textNode("refcontent", "Work in Progress"))
				break
			}
		}
	}
}

var digitRun = regexp.MustCompile(`\d+`)

// naturalLess orders strings so that numbers inside them compare by value:
// "RFC999" comes before "RFC1035".
func naturalLess(a, b string) bool {
	split := func(s string) (text []string, nums []string) {
		last := 0
		for _, m := range digitRun.FindAllStringIndex(s, -1) {
			text = append(text, s[last:m[0]])
			nums = append(nums, strings.TrimLeft(s[m[0]:m[1]], "0"))
			last = m[1]
		}
		return append(text, s[last:]), nums
	}
	at, an := split(a)
	bt, bn := split(b)
	for i := 0; ; i++ {
		if i >= len(at) || i >= len(bt) {
			return len(at) < len(bt)
		}
		if at[i] != bt[i] {
			return at[i] < bt[i]
		}
		if i >= len(an) || i >= len(bn) {
			return len(an) < len(bn)
		}
		if an[i] != bn[i] {
			if len(an[i]) != len(bn[i]) {
				return len(an[i]) < len(bn[i])
			}
			return an[i] < bn[i]
		}
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// resolveXrefs indexes everything that can be referred to and gives each
// <xref> its display text.
func (d *document) resolveXrefs(root *node) error {
	var index func(*node)
	index = func(e *node) {
		for _, name := range []string{"anchor", "pn"} {
			if v := e.get(name); v != "" {
				if _, dup := d.byID[v]; !dup {
					d.byID[v] = e
				}
			}
		}
		for _, c := range e.children {
			index(c)
		}
	}
	index(root)

	// Section titles are addressed by a slug, which the table of contents
	// uses to find the page a section starts on.
	for _, tag := range []string{"section", "references", "note"} {
		for _, e := range root.descendants(tag) {
			name := e.find("name")
			if name == nil {
				continue
			}
			base := "name-" + strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(cleanText(name.allText())), "-"), "-")
			slug := base
			for i := 2; d.byID[slug] != nil; i++ {
				slug = base + "-" + strconv.Itoa(i)
			}
			name.attr["slugifiedName"] = slug
			d.byID[slug] = name
		}
	}

	for _, x := range root.descendants("xref") {
		target := x.get("target")
		if _, ok := d.refNames[target]; ok {
			x.attr["derivedContent"] = d.refNames[target]
			continue
		}
		t := d.byID[target]
		if t == nil {
			return fmt.Errorf("<xref target=%q> refers to nothing", target)
		}
		if x.get("section") != "" {
			return fmt.Errorf("<xref target=%q section=...> must point at a reference", target)
		}
		if t.tag != "section" && t.tag != "references" {
			return fmt.Errorf("<xref target=%q> points at a <%s>; only sections and references are supported", target, t.tag)
		}
		// A section is cited by its number, or by its quoted title when
		// it has none.
		pn := t.get("pn")
		label := sectionNumber(pn)
		if t.get("numbered") == "false" {
			label = `"` + cleanText(t.find("name").allText()) + `"`
		}
		kind := "Section"
		if isAppendix(pn) {
			kind = "Appendix"
		}
		x.attr["derivedContent"] = kind + " " + label
	}
	return nil
}

// cleanText replaces the special spaces and joiners used during rendering
// with their plain equivalents.
func cleanText(s string) string {
	s = strings.NewReplacer(string(nbsp), " ", string(lineSep), " ",
		string(zwsp), "", string(wordJoiner), "", string(keepJoiner), "").Replace(s)
	return strings.TrimFunc(s, isSpace)
}

// addTOC builds the table of contents as a section holding nested lists, one
// entry per section: its number, its title, and a placeholder for the page.
func (d *document) addTOC(front *node) {
	const spacer = "  "
	// The first three entries are kept on one page with what follows.
	keep := 3

	entry := func(s *node) *node {
		name := s.find("name")
		t := newNode("t", "indent", "0")
		if keep > 0 {
			t.attr["keepWithNext"] = "true"
			keep--
		}
		num := ""
		if s.get("numbered") != "false" {
			num = sectionNumber(s.get("pn"))
			if isAppendix(s.get("pn")) && !strings.Contains(num, ".") {
				num = "Appendix " + num
			}
		}
		number := newNode("xref", "target", s.get("pn"), "format", "counter", "derivedContent", num)
		if num != "" {
			number.tail = "." + spacer
		}
		title := newNode("xref", "target", name.get("slugifiedName"), "format", "title",
			"derivedContent", "", "pageno", "0000")
		title.text = cleanText(name.allText())
		return t.append(number, title)
	}

	newList := func() *node {
		return newNode("ul", "empty", "true", "spacing", "compact", "bare", "true", "indent", "2")
	}
	var entries func(e *node) []*node
	entries = func(e *node) []*node {
		isSection := e.tag == "section" || e.tag == "references"
		if isSection && e.getDefault("toc", "include") != "include" {
			return nil
		}
		var sub []*node
		for _, c := range e.children {
			switch c.tag {
			case "front", "middle", "back", "section", "references":
				sub = append(sub, entries(c)...)
			}
		}
		// Numbers longer than the first one in a list give up one of
		// their two spaces, so that the titles stay aligned.
		if len(sub) > 0 {
			first := runeLen(sub[0].find("t").find("xref").get("derivedContent"))
			for _, li := range sub {
				x := li.find("t").find("xref")
				num := x.get("derivedContent")
				if runeLen(num) > first && strings.HasSuffix(x.tail, spacer) && !strings.HasPrefix(num, "Appendix") {
					x.tail = strings.TrimSuffix(x.tail, string(nbsp))
				}
			}
		}
		if !isSection {
			return sub
		}
		li := newNode("li").append(entry(e))
		if len(sub) > 0 {
			li.append(newList().append(sub...))
		}
		return []*node{li}
	}

	toc := newNode("section", "anchor", "toc", "numbered", "false", "toc", "exclude")
	toc.append(textNode("name", "Table of Contents"))
	toc.append(newList().append(entries(d.root)...))
	front.append(newNode("toc").append(toc))
	d.toc = toc
}
