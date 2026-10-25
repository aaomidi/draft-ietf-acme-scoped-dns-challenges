package rfc

import (
	"regexp"
	"strings"
	"unicode"
)

// This file reproduces the line filling of xml2rfc, which is Python's textwrap
// with xml2rfc's own rules for where a line may break. The break rules are
// written out by hand because the original expresses them with lookaheads,
// which Go's regexp package does not have.

const (
	nbsp       = ' ' // no-break space
	nbHyphen   = '‑' // no-break hyphen
	zwsp       = '​' // zero width space: a permitted break
	wordJoiner = '⁠'
	// keepJoiner is a private-use word joiner that xml2rfc inserts to keep
	// short URLs on one line. It never reaches the output.
	keepJoiner = ''
	// lineSep separates the lines of filled text, as in xml2rfc.
	lineSep = ' '
)

// stripSpace is the whitespace xml2rfc trims from the ends of text blocks.
const stripSpace = " \t\n\r\f\v"

func isASCIISpace(r rune) bool { return strings.ContainsRune(stripSpace, r) }

// isSpace matches what Python's \s does for text: Unicode whitespace.
func isSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// isWord matches what Python's \w does for text.
func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

func isASCIIDigit(r rune) bool  { return r >= '0' && r <= '9' }
func isASCIILetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

// noBreakBefore reports whether r forbids a break in front of it.
func noBreakBefore(r rune) bool {
	return r == nbsp || r == '|' || r == wordJoiner || r == keepJoiner
}

// separatorAt returns the end of the separator that starts at t[p], or -1. A
// separator is a piece of text that a line may end after: a run of spaces, a
// word up to and including a slash or a hyphen, a zero width space, or a dash.
func separatorAt(t []rune, p int) int {
	n := len(t)
	free := func(i int) bool { return i >= n || !noBreakBefore(t[i]) }

	// A run of ASCII whitespace.
	if isASCIISpace(t[p]) {
		q := p
		for q < n && isASCIISpace(t[q]) {
			q++
		}
		if free(q) {
			return q
		}
		if q-p >= 2 {
			return q - 1
		}
	}

	// The word characters in front of a break: anything but spaces and
	// hyphens.
	runEnd := p
	for runEnd < n && !isSpace(t[runEnd]) && t[runEnd] != '-' {
		runEnd++
	}

	// "word/" when a letter follows: break after the last such slash.
	for s := runEnd - 1; s > p; s-- {
		if t[s] == '/' && isWord(t[s-1]) && s+1 < n && isASCIILetter(t[s+1]) {
			return s + 1
		}
	}

	// "word-" when more of the word follows. Neither side may be a number,
	// so that "dns-01" and "SHA-256" stay whole.
	wordFollows := func(i int) bool {
		w := 0
		for i+w < n && isWord(t[i+w]) {
			w++
		}
		if w == 0 {
			return false
		}
		if i+w < n && !isASCIIDigit(t[i+w]) && !isSpace(t[i+w]) {
			return true
		}
		for k := 1; k < w; k++ {
			if !isASCIIDigit(t[i+k]) {
				return true
			}
		}
		return false
	}
	if runEnd < n && t[runEnd] == '-' {
		if runEnd+1 < n && t[runEnd+1] == '-' && runEnd-1 >= p && isWord(t[runEnd-1]) &&
			wordFollows(runEnd+2) && free(runEnd+2) {
			return runEnd + 2
		}
		if runEnd-2 >= p && isWord(t[runEnd-2]) && !isASCIIDigit(t[runEnd-1]) &&
			wordFollows(runEnd+1) && free(runEnd+1) {
			return runEnd + 1
		}
	}

	if t[p] == zwsp && free(p+1) {
		return p + 1
	}

	// Two or more dashes used as an em-dash between words.
	if t[p] == '-' && p > 0 && (isWord(t[p-1]) || strings.ContainsRune(`!"'&.,?`, t[p-1])) {
		q := p
		for q < n && t[q] == '-' {
			q++
		}
		if q-p >= 2 && q < n && isWord(t[q]) && free(q) {
			return q
		}
	}
	return -1
}

// splitChunks cuts text into the chunks that lines are assembled from.
func splitChunks(text string) []string {
	t := []rune(text)
	var chunks []string
	add := func(from, to int) {
		if to > from {
			chunks = append(chunks, string(t[from:to]))
		}
	}
	last := 0
	for p := 0; p < len(t); {
		end := separatorAt(t, p)
		if end < 0 {
			p++
			continue
		}
		add(last, p)
		add(p, end)
		last, p = end, end
	}
	add(last, len(t))
	return chunks
}

var (
	sentenceEnd    = regexp.MustCompile(`[A-Za-z0-9>\])"'][.!?]["')\]]?$`)
	notSentenceEnd = regexp.MustCompile(`(^|[\s\p{Z}])(\.[\])'"]*|[(\[]?([A-Z][a-z][a-z]|Eq|[Cc]f|vs|resp|viz|ibid|[JS]r|M[rs]|Messrs|Mmes|Dr|Profs?|St|Lt|a\.k\.a|i\.e|e\.g)\.)$`)
	sentenceStart  = regexp.MustCompile(`^["'(\[]*[A-Z]`)

	sectionWord = regexp.MustCompile(`(Section|Appendix|Figure|Table) `)
)

// fixSentenceEndings puts two spaces between sentences.
func fixSentenceEndings(chunks []string) {
	for i := 0; i < len(chunks)-2; {
		if chunks[i+1] == " " && sentenceEnd.MatchString(chunks[i]) &&
			!notSentenceEnd.MatchString(chunks[i]) && sentenceStart.MatchString(chunks[i+2]) {
			chunks[i+1] = "  "
			i += 2
		} else {
			i++
		}
	}
}

// collapseSpaces reduces runs of spaces to one, or to two after the end of a
// sentence.
func collapseSpaces(s string) string {
	t := []rune(s)
	pass := func(sentence bool, min int, keep string) {
		var out []rune
		for i := 0; i < len(t); i++ {
			out = append(out, t[i])
			if strings.ContainsRune(".!?", t[i]) != sentence {
				continue
			}
			j := i + 1
			for j < len(t) && t[j] == ' ' {
				j++
			}
			if j-(i+1) >= min {
				out = append(out, []rune(keep)...)
				i = j - 1
			}
		}
		t = out
	}
	pass(false, 2, " ")
	pass(true, 3, "  ")
	return string(t)
}

func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch r {
		case '\t':
			n := 8 - col%8
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case '\n', '\r':
			b.WriteRune(r)
			col = 0
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

var zeroWidth = strings.NewReplacer(string(zwsp), "", "‌", "", string(wordJoiner), "", string(keepJoiner), "")
var joinersToPlain = strings.NewReplacer(string(wordJoiner), "", string(keepJoiner), "", string(nbsp), " ")

type wrapOptions struct {
	width int
	// initial and subsequent are the indents of the first and later lines.
	initial, subsequent string
	// noSentenceSpacing turns off the double space between sentences.
	noSentenceSpacing bool
}

func runeLen(s string) int { return len([]rune(s)) }

// wrap breaks text into lines no longer than the width, where that is
// possible without breaking a word.
func wrap(text string, o wrapOptions) []string {
	if strings.ContainsRune(text, lineSep) {
		var lines []string
		for _, part := range strings.Split(text, string(lineSep)) {
			part = strings.TrimLeftFunc(part, isSpace)
			if part == "" {
				lines = append(lines, "")
			} else {
				lines = append(lines, wrap(part, o)...)
			}
			o.initial = ""
		}
		return lines
	}

	text = strings.Map(func(r rune) rune {
		if isASCIISpace(r) {
			return ' '
		}
		return r
	}, expandTabs(text))
	text = collapseSpaces(text)
	// Keep "Section 3" and the like on one line.
	text = sectionWord.ReplaceAllString(text, "$1"+string(nbsp))

	// A chunk too long for any line is split again without the characters
	// that were holding it together.
	maxWord := o.width - runeLen(o.subsequent)
	var chunks []string
	for _, chunk := range splitChunks(text) {
		clean := zeroWidth.Replace(chunk)
		if runeLen(clean) <= maxWord {
			chunks = append(chunks, clean)
			continue
		}
		for _, bit := range splitChunks(joinersToPlain.Replace(chunk)) {
			bit = joinersToPlain.Replace(bit)
			if runeLen(bit) > maxWord {
				chunks = append(chunks, splitChunks(bit)...)
			} else {
				chunks = append(chunks, bit)
			}
		}
	}
	if !o.noSentenceSpacing {
		fixSentenceEndings(chunks)
	}

	blank := func(s string) bool { return strings.TrimFunc(s, isSpace) == "" }
	var lines []string
	for len(chunks) > 0 {
		indent := o.subsequent
		if len(lines) == 0 {
			indent = o.initial
		}
		width := o.width - runeLen(indent)
		// A line does not start with whitespace, except the first.
		if blank(chunks[0]) && len(lines) > 0 {
			chunks = chunks[1:]
		}
		var cur []string
		curLen := 0
		for len(chunks) > 0 {
			l := runeLen(chunks[0])
			if curLen+l > width {
				break
			}
			cur = append(cur, chunks[0])
			curLen += l
			chunks = chunks[1:]
		}
		// A chunk too long for any line goes on a line of its own.
		if len(chunks) > 0 && runeLen(chunks[0]) > width && len(cur) == 0 {
			cur = append(cur, chunks[0])
			chunks = chunks[1:]
		}
		if len(cur) > 0 && blank(cur[len(cur)-1]) {
			cur = cur[:len(cur)-1]
		}
		if len(cur) > 0 {
			lines = append(lines, indent+strings.Join(cur, ""))
		}
	}
	for i, l := range lines {
		lines[i] = strings.ReplaceAll(l, string(keepJoiner), "")
	}
	return lines
}

var urlPattern = regexp.MustCompile(`https?:[^\s\p{Z}]*`)

// keepURLs marks URLs that fit on a line so that they are not broken.
func keepURLs(text string, max int) string {
	if !strings.Contains(text, "http://") && !strings.Contains(text, "https://") {
		return text
	}
	j := string(keepJoiner)
	glue := strings.NewReplacer("/", "/"+j, "-", "-"+j, ":", ":"+j)
	return urlPattern.ReplaceAllStringFunc(text, func(url string) string {
		if runeLen(url) > max {
			return url
		}
		return glue.Replace(url)
	})
}

type fillOptions struct {
	width        int
	indent, hang int
	keepURL      bool
	// noSentenceSpacing turns off the double space between sentences.
	noSentenceSpacing bool
}

// fill wraps text and returns the lines joined by lineSep.
func fill(text string, o fillOptions) string {
	if o.keepURL {
		text = keepURLs(text, o.width)
	}
	return strings.Join(wrap(text, wrapOptions{
		width:             o.width,
		initial:           strings.Repeat(" ", o.indent),
		subsequent:        strings.Repeat(" ", o.indent+o.hang),
		noSentenceSpacing: o.noSentenceSpacing,
	}), string(lineSep))
}
