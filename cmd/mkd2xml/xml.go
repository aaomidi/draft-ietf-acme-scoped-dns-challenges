package main

import "strings"

// xmlBuf builds indented XML one line at a time. Callers are responsible for
// escaping; see escText and escAttr.
type xmlBuf struct {
	b     strings.Builder
	depth int
}

func (x *xmlBuf) line(s string) {
	if s != "" {
		x.b.WriteString(strings.Repeat("  ", x.depth))
	}
	x.b.WriteString(s)
	x.b.WriteByte('\n')
}

func (x *xmlBuf) open(tag string) {
	x.line(tag)
	x.depth++
}

func (x *xmlBuf) close(tag string) {
	x.depth--
	x.line(tag)
}

// raw writes s as is, for content where whitespace is significant.
func (x *xmlBuf) raw(s string) {
	x.b.WriteString(s)
}

func (x *xmlBuf) String() string {
	return x.b.String()
}

var (
	textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
)

func escText(s string) string { return textEscaper.Replace(s) }
func escAttr(s string) string { return attrEscaper.Replace(s) }

// cdata wraps s in a CDATA section, splitting it where s contains the CDATA
// terminator.
func cdata(s string) string {
	return "<![CDATA[" + strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>") + "]]>"
}
