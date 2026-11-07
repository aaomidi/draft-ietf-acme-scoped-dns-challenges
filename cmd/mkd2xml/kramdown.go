package main

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"
)

// The source of a draft is kramdown, which this tool reads with a CommonMark
// parser. The two agree on ordinary text, but not everywhere. This file
// holds what bridges the gap: the escapes and entities that kramdown
// resolves, and checks that turn a construct the two would read differently
// into an error, so that it never becomes quietly different XML.

// kramdownEscapable are the characters a backslash escapes in kramdown. A
// backslash before anything else is an ordinary backslash.
const kramdownEscapable = "\\.*_+-=`()[]{}<>#!:|\"'$"

var entityRef = regexp.MustCompile(`^&(#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{1,31});`)

// resolveText turns the source of a run of plain text into the text it
// stands for, by resolving backslash escapes and character entities.
func resolveText(raw string) string {
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c == '\\' && i+1 < len(raw) && strings.IndexByte(kramdownEscapable, raw[i+1]) >= 0:
			i++
			b.WriteByte(raw[i])
		case c == '&':
			if ref := entityRef.FindString(raw[i:]); ref != "" {
				if decoded := html.UnescapeString(ref); decoded != ref {
					b.WriteString(decoded)
					i += len(ref) - 1
					continue
				}
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// unescaped returns the positions in raw of the bytes for which match
// holds and that are not escaped by a backslash.
func unescaped(raw string, match func(byte) bool) []int {
	var at []int
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\\' {
			i++
			continue
		}
		if match(raw[i]) {
			at = append(at, i)
		}
	}
	return at
}

func isAlnum(b byte) bool {
	return b >= 0x80 || unicode.IsLetter(rune(b)) || unicode.IsDigit(rune(b))
}

func isBlank(b byte) bool { return b == ' ' || b == '\t' || b == '\n' }

// checkText reports plain text that kramdown would not read as plain text,
// or would read differently from CommonMark. The advice in each message is
// what makes the text mean the same to both.
func checkText(raw string) error {
	for _, c := range []struct{ marker, problem string }{
		{"[^", "a footnote"},
		{"(((", "an index entry"},
		{"$$", "math"},
		{"{{{", "a contact name"},
		{"{:", "an attribute list"},
		{"{::", "a kramdown directive"},
	} {
		if strings.Contains(raw, c.marker) {
			return fmt.Errorf("%q would be read by kramdown-rfc as %s, which is not supported", c.marker, c.problem)
		}
	}
	if at := unescaped(raw, func(b byte) bool { return b == '|' }); len(at) > 0 {
		return fmt.Errorf(`"|" in text would be read by kramdown-rfc as a table; write "\|", or put the text in backticks`)
	}
	// An emphasis marker that did not pair up is read differently by the
	// two parsers, unless it plainly is not one: inside a word for "_", or
	// with space on both sides.
	for _, i := range unescaped(raw, func(b byte) bool { return b == '*' || b == '_' }) {
		before, after := byte(' '), byte(' ')
		if i > 0 {
			before = raw[i-1]
		}
		if i+1 < len(raw) {
			after = raw[i+1]
		}
		if isBlank(before) && isBlank(after) {
			continue
		}
		if raw[i] == '_' && isAlnum(before) && isAlnum(after) {
			continue
		}
		return fmt.Errorf(`%q here is not clearly emphasis or plain text; write "\%c" for the character itself, or put the text in backticks`, raw[i], raw[i])
	}
	if strings.HasSuffix(raw, `\\`) || strings.Contains(raw, "\\\\\n") {
		return fmt.Errorf(`"\\" at the end of a line is a line break in kramdown, which is not supported`)
	}
	return nil
}

// checkParagraph reports a paragraph that kramdown would read as something
// other than a paragraph.
func checkParagraph(raw string) error {
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimLeft(line, " ")
		switch {
		case i > 0 && (strings.HasPrefix(line, ": ") || strings.HasPrefix(line, ":\t")):
			return fmt.Errorf(`a line starting with ":" makes this a definition list in kramdown, which is not supported`)
		case strings.HasPrefix(line, "*["):
			return fmt.Errorf(`a line starting with "*[" is an abbreviation definition in kramdown, which is not supported`)
		}
	}
	return nil
}

// reservedAnchor reports whether xml2rfc keeps anchors with this prefix for
// its own use and rejects a document that defines one.
func reservedAnchor(anchor string) bool {
	for _, p := range []string{"section-", "figure-", "table-", "iref-", "u-"} {
		if strings.HasPrefix(anchor, p) {
			return true
		}
	}
	return false
}

// headingID matches the "{#id}" that kramdown allows at the end of a heading
// to set its anchor.
var headingID = regexp.MustCompile(`\s*\{#([A-Za-z][\w.-]*)\}\s*$`)

// bracketed matches a name in square brackets. kramdown-rfc turns "[RFC8555]"
// into a reference when the document cites RFC8555 anywhere, so the names
// are collected and checked once all the references are known.
var bracketed = regexp.MustCompile(`\[([A-Za-z0-9][A-Za-z0-9._-]*)\]`)

// expandTabs replaces each tab by spaces up to the next multiple of 8
// columns.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
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
		case '\n':
			b.WriteRune(r)
			col = 0
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}
