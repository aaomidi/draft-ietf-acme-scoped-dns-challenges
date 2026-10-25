package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// frontMatter is the YAML header of the draft. Decoding is strict: a key that
// is not listed here is an error, so nothing is silently dropped.
type frontMatter struct {
	Title          string       `yaml:"title"`
	Abbrev         string       `yaml:"abbrev"`
	Category       string       `yaml:"category"`
	SubmissionType string       `yaml:"submissiontype"`
	Docname        string       `yaml:"docname"`
	Updates        string       `yaml:"updates"`
	Obsoletes      string       `yaml:"obsoletes"`
	Version        int          `yaml:"v"`
	Date           string       `yaml:"date"`
	Consensus      *bool        `yaml:"consensus"`
	Area           string       `yaml:"area"`
	Workgroup      string       `yaml:"workgroup"`
	Keyword        stringList   `yaml:"keyword"`
	IPR            string       `yaml:"ipr"`
	Venue          *venue       `yaml:"venue"`
	Author         []author     `yaml:"author"`
	Normative      referenceMap `yaml:"normative"`
	Informative    referenceMap `yaml:"informative"`
}

type venue struct {
	Group  string `yaml:"group"`
	Type   string `yaml:"type"`
	Mail   string `yaml:"mail"`
	Arch   string `yaml:"arch"`
	GitHub string `yaml:"github"`
	Latest string `yaml:"latest"`
}

// author accepts both the long field names and kramdown-rfc's short ones.
type author struct {
	Fullname string `yaml:"fullname"`
	Name     string `yaml:"name"`
	Initials string `yaml:"initials"`
	Surname  string `yaml:"surname"`
	// Ins is "J. Smith": the initials, then the surname.
	Ins          string `yaml:"ins"`
	Organization string `yaml:"organization"`
	Org          string `yaml:"org"`
	Email        string `yaml:"email"`
}

// normalize folds the short field names into the long ones.
func (a *author) normalize() error {
	if a.Name != "" {
		if a.Fullname != "" {
			return fmt.Errorf("author has both fullname and name")
		}
		a.Fullname = a.Name
	}
	if a.Org != "" {
		if a.Organization != "" {
			return fmt.Errorf("author has both organization and org")
		}
		a.Organization = a.Org
	}
	if a.Ins != "" {
		if a.Initials != "" || a.Surname != "" {
			return fmt.Errorf("author has both ins and initials or surname")
		}
		var ok bool
		if a.Initials, a.Surname, ok = strings.Cut(a.Ins, " "); !ok {
			return fmt.Errorf("author ins %q is not of the form %q", a.Ins, "J. Smith")
		}
	}
	if a.Fullname == "" && a.Surname == "" {
		return fmt.Errorf("author needs a fullname, or a name, or ins")
	}
	return nil
}

// stringList accepts either a single string or a list of strings.
type stringList []string

func (s *stringList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*s = []string{n.Value}
		return nil
	}
	var list []string
	if err := n.Decode(&list); err != nil {
		return err
	}
	*s = list
	return nil
}

// referenceMap is the "normative" or "informative" block. Order is kept, as it
// decides the order of the references in the output.
type referenceMap []referenceEntry

type referenceEntry struct {
	Key string
	// Def is nil for an entry without a body, which is fetched by its key.
	Def *referenceDef
}

// referenceDef is a reference written out by hand in the front matter.
type referenceDef struct {
	Title  string
	Date   string
	Target string
	Author []referenceAuthor
	// Series is the series information, such as ISBN or DOI, in order.
	Series []attr
}

type referenceAuthor struct {
	Org  string
	Name string
	Ins  string
}

func (m *referenceMap) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected a mapping of references", n.Line)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		entry := referenceEntry{Key: key.Value}
		if !(val.Kind == yaml.ScalarNode && val.Tag == "!!null") {
			def, err := decodeReferenceDef(val)
			if err != nil {
				return fmt.Errorf("reference %s: %v", key.Value, err)
			}
			entry.Def = def
		}
		*m = append(*m, entry)
	}
	return nil
}

func decodeReferenceDef(n *yaml.Node) (*referenceDef, error) {
	fields, err := mappingFields(n)
	if err != nil {
		return nil, err
	}
	def := &referenceDef{}
	for _, f := range fields {
		switch f.key {
		case "title":
			def.Title = f.val.Value
		case "date":
			def.Date = f.val.Value
		case "target":
			def.Target = f.val.Value
		case "author":
			nodes := []*yaml.Node{f.val}
			if f.val.Kind == yaml.SequenceNode {
				nodes = f.val.Content
			}
			for _, an := range nodes {
				a, err := decodeReferenceAuthor(an)
				if err != nil {
					return nil, err
				}
				def.Author = append(def.Author, a)
			}
		case "seriesinfo":
			series, err := mappingFields(f.val)
			if err != nil {
				return nil, err
			}
			for _, s := range series {
				def.Series = append(def.Series, attr{s.key, s.val.Value})
			}
		default:
			return nil, fmt.Errorf("line %d: unsupported field %q", f.val.Line, f.key)
		}
	}
	if def.Title == "" {
		return nil, fmt.Errorf("line %d: missing title", n.Line)
	}
	return def, nil
}

func decodeReferenceAuthor(n *yaml.Node) (referenceAuthor, error) {
	var a referenceAuthor
	fields, err := mappingFields(n)
	if err != nil {
		return a, err
	}
	for _, f := range fields {
		switch f.key {
		case "org":
			a.Org = f.val.Value
		case "name":
			a.Name = f.val.Value
		case "ins":
			a.Ins = f.val.Value
		default:
			return a, fmt.Errorf("line %d: unsupported author field %q", f.val.Line, f.key)
		}
	}
	return a, nil
}

type yamlField struct {
	key string
	val *yaml.Node
}

func mappingFields(n *yaml.Node) ([]yamlField, error) {
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: expected a mapping", n.Line)
	}
	var fields []yamlField
	for i := 0; i+1 < len(n.Content); i += 2 {
		fields = append(fields, yamlField{n.Content[i].Value, n.Content[i+1]})
	}
	return fields, nil
}

var unknownField = regexp.MustCompile(`field (\S+) not found in type \S+`)

func parseFrontMatter(src []byte) (*frontMatter, error) {
	if len(bytes.TrimSpace(src)) == 0 {
		return nil, fmt.Errorf("front matter: empty")
	}
	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)
	fm := &frontMatter{}
	if err := dec.Decode(fm); err != nil {
		// Say "unsupported key" where the YAML library names a Go type.
		msg := unknownField.ReplaceAllString(err.Error(), `unsupported key "$1"`)
		return nil, fmt.Errorf("front matter: %s", msg)
	}
	for _, required := range []struct{ name, value string }{
		{"title", fm.Title},
		{"docname", fm.Docname},
		{"ipr", fm.IPR},
		{"category", fm.Category},
	} {
		if required.value == "" {
			return nil, fmt.Errorf("front matter: missing %q", required.name)
		}
	}
	if fm.Version != 0 && fm.Version != 3 {
		return nil, fmt.Errorf("front matter: only v: 3 is supported, got %d", fm.Version)
	}
	if len(fm.Author) == 0 {
		return nil, fmt.Errorf("front matter: missing %q", "author")
	}
	for i := range fm.Author {
		if err := fm.Author[i].normalize(); err != nil {
			return nil, fmt.Errorf("front matter: %v", err)
		}
	}
	return fm, nil
}

// baseDocname is the draft name without its revision, as the datatracker uses
// it in URLs. Like kramdown-rfc, it takes everything before the last dash.
func baseDocname(docname string) string {
	i := strings.LastIndexByte(docname, '-')
	if i < 0 {
		return ""
	}
	return docname[:i]
}

// xmlDate is the content of a <date> element.
type xmlDate struct {
	Year, Month, Day string
}

// parseDate accepts YYYY, YYYY-MM and YYYY-MM-DD.
func parseDate(s string) (xmlDate, error) {
	parts := strings.Split(s, "-")
	if len(parts) > 3 {
		return xmlDate{}, fmt.Errorf("unsupported date %q", s)
	}
	var d xmlDate
	year, err := strconv.Atoi(parts[0])
	if err != nil || len(parts[0]) != 4 {
		return xmlDate{}, fmt.Errorf("unsupported date %q", s)
	}
	d.Year = strconv.Itoa(year)
	if len(parts) > 1 {
		month, err := strconv.Atoi(parts[1])
		if err != nil || month < 1 || month > 12 {
			return xmlDate{}, fmt.Errorf("unsupported date %q", s)
		}
		d.Month = time.Month(month).String()
	}
	if len(parts) > 2 {
		day, err := strconv.Atoi(parts[2])
		if err != nil || day < 1 || day > 31 {
			return xmlDate{}, fmt.Errorf("unsupported date %q", s)
		}
		// The day is kept as written, as kramdown-rfc keeps it.
		d.Day = parts[2]
	}
	return d, nil
}

func dateOf(t time.Time) xmlDate {
	return xmlDate{
		Year:  strconv.Itoa(t.Year()),
		Month: t.Month().String(),
		Day:   strconv.Itoa(t.Day()),
	}
}
