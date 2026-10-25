package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// node is an XML element. Text content is kept the way lxml keeps it, because
// the rendering rules are written in those terms: text is what precedes the
// first child, and tail is what follows the element inside its parent.
type node struct {
	tag        string
	attr       map[string]string
	text, tail string
	children   []*node
	parent     *node

	// page is the page the element's last line landed on, once paginated.
	page int
	// outdent is how far the element must move left for a too-wide child to
	// fit; see ljoin.
	outdent int
	// list holds the numbering state of a <ul> or <ol> while it is rendered.
	list *listState
}

func newNode(tag string, attrs ...string) *node {
	n := &node{tag: tag, attr: map[string]string{}}
	for i := 0; i+1 < len(attrs); i += 2 {
		n.attr[attrs[i]] = attrs[i+1]
	}
	return n
}

func textNode(tag, text string, attrs ...string) *node {
	n := newNode(tag, attrs...)
	n.text = text
	return n
}

func (n *node) get(name string) string { return n.attr[name] }

func (n *node) getDefault(name, def string) string {
	if v, ok := n.attr[name]; ok {
		return v
	}
	return def
}

func (n *node) append(children ...*node) *node {
	for _, c := range children {
		c.parent = n
		n.children = append(n.children, c)
	}
	return n
}

func (n *node) insert(i int, c *node) {
	c.parent = n
	n.children = append(n.children, nil)
	copy(n.children[i+1:], n.children[i:])
	n.children[i] = c
}

// find returns the first child with the given tag, or nil.
func (n *node) find(tag string) *node {
	for _, c := range n.children {
		if c.tag == tag {
			return c
		}
	}
	return nil
}

func (n *node) findAll(tag string) []*node {
	var out []*node
	for _, c := range n.children {
		if c.tag == tag {
			out = append(out, c)
		}
	}
	return out
}

// descendants returns every element below n with the given tag, in document
// order.
func (n *node) descendants(tag string) []*node {
	var out []*node
	var walk func(*node)
	walk = func(e *node) {
		for _, c := range e.children {
			if c.tag == tag {
				out = append(out, c)
			}
			walk(c)
		}
	}
	walk(n)
	return out
}

// next returns the element following n in its parent, or nil.
func (n *node) next() *node {
	if n.parent == nil {
		return nil
	}
	for i, c := range n.parent.children {
		if c == n && i+1 < len(n.parent.children) {
			return n.parent.children[i+1]
		}
	}
	return nil
}

func (n *node) prev() *node {
	if n.parent == nil {
		return nil
	}
	for i, c := range n.parent.children {
		if c == n && i > 0 {
			return n.parent.children[i-1]
		}
	}
	return nil
}

// allText returns the text of n and everything inside it.
func (n *node) allText() string {
	var b strings.Builder
	var walk func(*node)
	walk = func(e *node) {
		b.WriteString(e.text)
		for _, c := range e.children {
			walk(c)
			b.WriteString(c.tail)
		}
	}
	walk(n)
	return b.String()
}

// copyOf returns a deep copy of n without a parent or tail.
func (n *node) copyOf() *node {
	c := &node{tag: n.tag, attr: map[string]string{}, text: n.text}
	for k, v := range n.attr {
		c.attr[k] = v
	}
	for _, child := range n.children {
		cc := child.copyOf()
		cc.tail = child.tail
		c.append(cc)
	}
	return c
}

// allowed lists, for every supported element, the elements it may contain.
// Anything else in the input is an error: this tool renders the subset of
// RFCXML that the draft uses, and says so when it meets something outside it.
var allowed = map[string][]string{
	"rfc":          {"front", "middle", "back"},
	"front":        {"title", "seriesInfo", "author", "date", "area", "workgroup", "keyword", "abstract", "note"},
	"title":        {},
	"seriesInfo":   {},
	"author":       {"organization", "address"},
	"organization": {},
	"address":      {"email"},
	"email":        {},
	"date":         {},
	"area":         {},
	"workgroup":    {},
	"keyword":      {},
	"abstract":     {"t", "ul", "ol"},
	"note":         {"name", "t", "ul", "ol"},
	"name":         {"tt", "em", "strong", "bcp14", "xref", "eref"},
	"middle":       {"section"},
	"section":      {"name", "t", "ul", "ol", "artwork", "sourcecode", "section"},
	"t":            {"tt", "em", "strong", "bcp14", "xref", "eref"},
	"tt":           {"em", "strong", "bcp14", "xref", "eref"},
	"em":           {"tt", "strong", "bcp14", "xref", "eref"},
	"strong":       {"tt", "em", "bcp14", "xref", "eref"},
	"bcp14":        {},
	"xref":         {},
	"eref":         {},
	"ul":           {"li"},
	"ol":           {"li"},
	"li":           {"t", "ul", "ol", "artwork", "sourcecode"},
	"artwork":      {},
	"sourcecode":   {},
	"back":         {"references", "section"},
	"references":   {"name", "references", "reference"},
	"reference":    {"front", "seriesInfo", "refcontent"},
	"refcontent":   {},
	// The front of a reference; told apart from the document front by
	// its parent.
	"reference/front": {"title", "author", "date", "abstract", "seriesInfo"},
	// The abstract of a reference is not rendered, so anything goes.
	"reference/front/abstract": nil,
}

// parse reads an RFCXML document into a tree.
func parse(r io.Reader) (*node, error) {
	dec := xml.NewDecoder(r)
	var root, cur *node
	// skip counts the depth inside an element whose content is ignored.
	skip := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		line, _ := dec.InputPos()
		switch tok := tok.(type) {
		case xml.StartElement:
			if skip > 0 {
				skip++
				continue
			}
			tag := tok.Name.Local
			n := newNode(tag)
			for _, a := range tok.Attr {
				n.attr[a.Name.Local] = a.Value
			}
			if cur == nil {
				if root != nil || tag != "rfc" {
					return nil, fmt.Errorf("line %d: expected a single <rfc> root element, found <%s>", line, tag)
				}
				root, cur = n, n
				continue
			}
			kids, known := allowed[contextKey(cur)]
			if !known || !contains(kids, tag) {
				return nil, fmt.Errorf("line %d: <%s> inside <%s> is not supported", line, tag, cur.tag)
			}
			cur.append(n)
			cur = n
			if kids, ok := allowed[contextKey(n)]; ok && kids == nil {
				skip = 1
			}
		case xml.EndElement:
			if skip > 1 {
				skip--
				continue
			}
			skip = 0
			cur = cur.parent
		case xml.CharData:
			if cur == nil || skip > 0 {
				continue
			}
			if k := len(cur.children); k > 0 {
				cur.children[k-1].tail += string(tok)
			} else {
				cur.text += string(tok)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("no <rfc> element found")
	}
	if v := root.get("version"); v != "3" {
		return nil, fmt.Errorf("only RFCXML version 3 is supported, found version %q", v)
	}
	return root, nil
}

// contextKey is the key of n in the allowed table.
func contextKey(n *node) string {
	if n.parent != nil && n.parent.tag == "reference" && n.tag == "front" {
		return "reference/front"
	}
	if n.tag == "abstract" && n.parent != nil && contextKey(n.parent) == "reference/front" {
		return "reference/front/abstract"
	}
	return n.tag
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
