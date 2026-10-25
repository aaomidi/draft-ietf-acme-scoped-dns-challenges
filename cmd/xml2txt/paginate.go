package main

import (
	"fmt"
	"strings"
)

// A page holds 48 lines of text. Above them are 4 lines of header (blank on
// the first page) and below them 4 lines of footer.
const (
	pageText = 48
	// orphans and widows are the most lines of a block that may not be left
	// alone at the bottom or the top of a page.
	orphans = 2
	widows  = 2
	// minSectionStart is the number of lines of text that must follow a
	// section heading on its page.
	minSectionStart = 5
)

// block is a run of lines that belong together, such as a paragraph or a
// section heading with the element that follows it.
type block struct {
	beg, end int
	ended    bool
}

// findBlocks groups the lines into blocks. A block starts where the element
// owning the lines changes, and ends at the next blank line between
// elements, unless the element asks to be kept with the next one.
func findBlocks(lines []*line) {
	var elem *node
	var prev, cur *block
	keep := false
	for n, l := range lines {
		switch {
		case l.elem == nil:
			if cur != nil && !keep && !cur.ended {
				cur.end, cur.ended = n, true
			}
		case l.elem != elem:
			elem = l.elem
			switch elem.tag {
			case "t", "dl", "dt", "figure", "ol", "table", "ul":
			default:
				keep = false
			}
			if !keep {
				cur = &block{beg: n}
				if prev != nil && !prev.ended {
					prev.end, prev.ended = n, true
				}
				prev = cur
			}
			next := elem.next()
			keep = elem.get("keepWithNext") == "true" ||
				(next != nil && next.get("keepWithPrevious") == "true") ||
				elem.tag == "section"
			l.block = cur
		default:
			l.block = cur
		}
	}
	if cur != nil {
		cur.end = len(lines) - 1
	}
}

// justifyInline puts three strings on one line: left, centre and right.
func justifyInline(left, center, right string, width int) string {
	left, center, right = strings.TrimRightFunc(left, isSpace), strings.TrimSpace(center), strings.TrimSpace(right)
	centerWidth := width
	if runeLen(center)%2 != 0 {
		centerWidth++
	}
	pad := centerWidth - runeLen(center)
	lead := pad/2 + (pad & centerWidth & 1)
	out := []rune(left + spaces(width-runeLen(left)))
	place := func(s string, at int) {
		for i, r := range []rune(s) {
			if at+i >= 0 && at+i < len(out) && r != ' ' {
				out[at+i] = r
			}
		}
	}
	// The centre text wins over the right text where they would overlap.
	place(right, width-runeLen(right))
	place(center, lead)
	return string(out)
}

// footerAuthors names the authors in the page footer.
func (w *writer) footerAuthors() string {
	var surnames []string
	for _, a := range w.doc.root.find("front").findAll("author") {
		if a.get("role") == "contributor" {
			continue
		}
		if _, surname := authorNameParts(a); surname != "" {
			surnames = append(surnames, surname)
		}
	}
	switch len(surnames) {
	case 0:
		return ""
	case 1:
		return surnames[0]
	case 2:
		return surnames[0] + " & " + surnames[1]
	}
	return surnames[0] + ", et al."
}

// paginate breaks the lines into pages and adds the page headers and
// footers. It moves a break up when it would otherwise separate a section
// heading from its text, split a figure, or leave a line or two of a
// paragraph alone on a page.
func (w *writer) paginate(lines []*line) []*line {
	title := w.doc.root.find("front").find("title")
	short := title.get("abbrev")
	if short == "" {
		short = title.allText()
	}
	if r := []rune(short); len(r) > 40 {
		w.doc.warnf("the title or its abbreviation is longer than the 40 characters that fit the page header")
		short = string(r[:40])
	}
	header := justifyInline("Internet-Draft", short,
		fmt.Sprintf("%s %d", monthNames[w.doc.month], w.doc.year), maxWidth)
	footerLeft, footerCenter := w.footerAuthors(), "Expires "+w.doc.expiryString()

	isSection := func(i int) bool { return lines[i].elem != nil && lines[i].elem.tag == "section" }

	var out []*line
	start := 0
	target := 4 + pageText
	total := len(lines)
	for page := 1; start < total; page++ {
		brk, pad := target, 0
		if brk >= total {
			// The rest fits on this page.
			pad = brk - total
		} else if b := lines[target].block; b == nil {
			// The break falls between blocks. If a section heading
			// sits just above it, take the heading to the next page.
			found := false
			i := target
			for i > target-12 {
				moved := false
				for j := 1; j < 4; j++ {
					if k := i - j; k >= 0 && isSection(k) {
						found, moved = true, true
						i = k
						break
					}
				}
				if !moved {
					break
				}
			}
			if found {
				pad = target - i
				brk = i
			}
		} else {
			olen := target - b.beg // lines of the block left on this page
			wlen := b.end - target // lines of the block on the next page
			blen := b.end - b.beg
			switch lines[b.beg].elem.tag {
			case "section":
				text := 0
				for r := b.beg; r < target; r++ {
					if lines[r].elem != nil && lines[r].elem.tag != "section" {
						text++
					}
				}
				if text <= minSectionStart {
					pad += brk - b.beg
					brk = b.beg
				}
			case "artset", "artwork", "figure", "sourcecode", "table":
				if blen < pageText || olen <= minSectionStart {
					pad += brk - b.beg
					brk = b.beg
				}
			default:
				orphan := olen >= 1 && olen <= orphans && blen > olen
				widow := wlen >= 1 && wlen <= widows && blen > wlen
				if (orphan || widow) && start < brk-olen {
					brk -= olen
					pad += olen
				}
			}
		}

		from := len(out)
		if page > 1 {
			out = append(out, &line{text: "\f"}, &line{text: header}, blankLine(), blankLine())
		}
		out = append(out, lines[start:min(brk, total)]...)
		for i := 0; i < pad+3; i++ {
			out = append(out, blankLine())
		}
		out = append(out, &line{text: justifyInline(footerLeft, footerCenter, fmt.Sprintf("[Page %d]", page), maxWidth)})
		for _, l := range out[from:] {
			if l.elem != nil {
				l.elem.page = page
			}
		}

		start = brk
		for start < total && trim(lines[start].text) == "" {
			start++
		}
		target = start + pageText
	}
	return out
}

// pageOf returns the page an element is on: its own, or that of the nearest
// enclosing element that has lines.
func pageOf(e *node) int {
	for ; e != nil; e = e.parent {
		if e.page != 0 {
			return e.page
		}
	}
	return 0
}

// fillTOC writes the page numbers into the table of contents. The table was
// laid out with placeholders, so that it already has its final size; here
// it is rendered again with the real numbers and its lines are replaced.
func (w *writer) fillTOC(lines []*line) {
	toc := w.doc.toc
	if toc == nil {
		return
	}
	start, end := -1, -1
	for i, l := range lines {
		if l.elem == nil {
			continue
		}
		if l.elem == toc {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 && l.elem.tag == "section" {
			end = i
			break
		}
	}
	if start < 0 || end < 0 {
		return
	}
	for _, x := range toc.descendants("xref") {
		if x.get("pageno") == "" {
			continue
		}
		if target := w.doc.byID[x.get("target")]; target != nil {
			x.attr["pageno"] = fmt.Sprint(pageOf(target))
		}
	}
	fresh := w.renderLines(toc, maxWidth, context{joiners: baseJoiners})
	j := 2
	for i := start + 2; i < end && j < len(fresh); i++ {
		if lines[i].elem == nil {
			continue
		}
		lines[i].text = fresh[j].text
		j++
	}
}

// Render lays out a prepared document as paginated text.
func (d *document) Render() string {
	w := &writer{doc: d}
	lines := w.renderLines(d.root, maxWidth, context{joiners: baseJoiners})
	findBlocks(lines)
	lines = w.paginate(lines)
	w.fillTOC(lines)

	for i, l := range lines {
		if n := runeLen(l.text); n > maxWidth {
			d.warnf("line %d is %d characters longer than %d: %s", i+1, n-maxWidth, maxWidth, l.text)
		}
	}
	text := rtrim(joinLines(lines, "\n")) + "\n"
	return strings.NewReplacer(
		string(nbsp), " ",
		string(nbHyphen), "-",
		string(zwsp), "",
		string(wordJoiner), "",
	).Replace(text)
}
