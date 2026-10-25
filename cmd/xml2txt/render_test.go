package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testToday = time.Date(2026, time.October, 25, 0, 0, 0, 0, time.UTC)

// TestRenderMatchesXml2rfc renders each fixture and compares it with the text
// that xml2rfc produced from the same XML. The .txt files are xml2rfc's
// output and are not to be regenerated with this tool; see testdata/README.md.
func TestRenderMatchesXml2rfc(t *testing.T) {
	for _, name := range []string{"draft", "rich", "long"} {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(filepath.Join("testdata", name+".xml"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			got, _, err := Render(f, testToday)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", name+".txt"))
			if err != nil {
				t.Fatal(err)
			}
			if got == string(want) {
				return
			}
			gotLines, wantLines := strings.Split(got, "\n"), strings.Split(string(want), "\n")
			for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
				var g, w string
				if i < len(gotLines) {
					g = gotLines[i]
				}
				if i < len(wantLines) {
					w = wantLines[i]
				}
				if g != w {
					t.Fatalf("first difference at line %d:\n got: %q\nwant: %q", i+1, g, w)
				}
			}
		})
	}
}

func TestRenderWarnings(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "draft.xml"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, warnings, err := Render(f, testToday)
	if err != nil {
		t.Fatal(err)
	}
	// The label formula in the draft is wider than its place in the list.
	want := "Artwork too wide, reducing indentation from 3 to 0"
	if len(warnings) != 1 || warnings[0] != want {
		t.Errorf("warnings = %q, want only %q", warnings, want)
	}
}

// minimalXML wraps middle content in the smallest document that renders.
func minimalXML(rfcAttrs, middle string) string {
	return `<rfc version="3" docName="draft-example-00" ` + rfcAttrs + `>
<front><title>T</title><author fullname="A B"/><date year="2026" month="October" day="25"/>
<abstract><t>A.</t></abstract></front>
<middle>` + middle + `</middle></rfc>`
}

const ietfDraft = `ipr="trust200902" category="info" submissionType="IETF"`

func TestRenderErrors(t *testing.T) {
	tests := []struct {
		name, xml, want string
	}{
		{"not rfc", `<html/>`, "expected a single <rfc> root element"},
		{"version 2", `<rfc/>`, "only RFCXML version 3 is supported"},
		{"unsupported element", minimalXML(ietfDraft, `<section><name>S</name><table/></section>`), "<table> inside <section> is not supported"},
		{"unsupported inline", minimalXML(ietfDraft, `<section><name>S</name><t>a<sub>2</sub></t></section>`), "<sub> inside <t> is not supported"},
		{"ipr", minimalXML(`ipr="pre5378Trust200902" category="info" submissionType="IETF"`, ``), `ipr="pre5378Trust200902" is not supported`},
		{"stream", minimalXML(`ipr="trust200902" category="info" submissionType="independent"`, ``), `submissionType="independent" is not supported`},
		{"category", minimalXML(`ipr="trust200902" category="std2" submissionType="IETF"`, ``), `category="std2" is not supported`},
		{"dangling xref", minimalXML(ietfDraft, `<section><name>S</name><t><xref target="nope"/></t></section>`), `<xref target="nope"> refers to nothing`},
		{"list type", minimalXML(ietfDraft, `<section><name>S</name><ol type="a"><li><t>x</t></li></ol></section>`), `<ol type="a"> is not supported`},
		{"numbered after unnumbered", minimalXML(ietfDraft, `<section numbered="false"><name>A</name></section><section anchor="b"><name>B</name></section>`), `section "b" is numbered but follows an unnumbered section`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Render(strings.NewReader(tt.xml), testToday)
			if err == nil {
				t.Fatalf("no error, want one containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestSplitChunks(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"two words", []string{"two", " ", "words"}},
		// A hyphen is a break when letters follow, but not before a number.
		{"multi-region", []string{"multi-", "region"}},
		{"dns-01 x", []string{"dns-01", " ", "x"}},
		{"SHA-256 digest", []string{"SHA-256", " ", "digest"}},
		{"dns-account-01,", []string{"dns-", "account-", "01,"}},
		{"a-b", []string{"a-b"}},
		// A URL breaks after the last slash that a letter follows.
		{"https://example.com/acme/chall/i00", []string{"https://example.com/acme/chall/", "i00"}},
		{"(https://trustee.ietf.org/license-info)", []string{"(https://trustee.ietf.org/", "license-", "info)"}},
		{"x/1", []string{"x/1"}},
		// A no-break space is not a break, and forbids one before it.
		{"BCP\u00A014 [R]", []string{"BCP\u00A014", " ", "[R]"}},
		// A double dash stays with the word in front of it.
		{"is it--really", []string{"is", " ", "it--", "really"}},
	}
	for _, tt := range tests {
		got := splitChunks(tt.in)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("splitChunks(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWrap(t *testing.T) {
	tests := []struct {
		name, in string
		opts     wrapOptions
		want     []string
	}{
		{"fits", "short text", wrapOptions{width: 20}, []string{"short text"}},
		{"breaks at space", "one two three four", wrapOptions{width: 9}, []string{"one two", "three", "four"}},
		{"indents", "one two three", wrapOptions{width: 9, initial: "* ", subsequent: "  "}, []string{"* one two", "  three"}},
		{"two spaces after a sentence", "It ends. Next one", wrapOptions{width: 40}, []string{"It ends.  Next one"}},
		{"not after an abbreviation", "See e.g. This and Dr. Who", wrapOptions{width: 40}, []string{"See e.g. This and Dr. Who"}},
		// xml2rfc takes any capitalised three-letter word for an abbreviation.
		{"not after a three-letter word", "The End. Next one", wrapOptions{width: 40}, []string{"The End. Next one"}},
		{"not before lower case", "Ends. then", wrapOptions{width: 40}, []string{"Ends. then"}},
		{"sentence spacing off", "End. Next", wrapOptions{width: 40, noSentenceSpacing: true}, []string{"End. Next"}},
		{"collapses spaces", "a   b.    C", wrapOptions{width: 40}, []string{"a b.  C"}},
		{"section number stays with its word", "as said in Section 7.3 here", wrapOptions{width: 19}, []string{"as said in", "Section\u00A07.3 here"}},
		{"long word is not broken", "a verylongwordindeed b", wrapOptions{width: 8}, []string{"a", "verylongwordindeed", "b"}},
		{"breaks after a hyphen", "the Internet-Drafts are", wrapOptions{width: 13}, []string{"the Internet-", "Drafts are"}},
		{"line separator", "one\u2028two three", wrapOptions{width: 40, initial: ">"}, []string{">one", "two three"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrap(tt.in, tt.opts)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("wrap(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestKeepURLs(t *testing.T) {
	const text = "see the page at <https://a.example/b-c>."
	if got := wrap(text, wrapOptions{width: 36}); len(got) != 2 || got[1] != "b-c>." {
		t.Errorf("an unmarked URL was not broken at its slash: %q", got)
	}
	if got := wrap(keepURLs(text, 36), wrapOptions{width: 36}); len(got) != 2 || got[1] != "<https://a.example/b-c>." {
		t.Errorf("a URL that fits a line was broken: %q", got)
	}
	long := keepURLs("see https://a.example/bbbb/cccc/dddd", 20)
	if strings.ContainsRune(long, keepJoiner) {
		t.Errorf("a URL longer than a line was marked unbreakable: %q", long)
	}
}

func TestNaturalLess(t *testing.T) {
	sorted := []string{"FIPS180-4", "I-D.DRAFT-X", "IANA", "RFC999", "RFC1035", "RFC2119", "RFC8174", "RFC8174A"}
	for i := range sorted {
		for j := range sorted {
			if got, want := naturalLess(sorted[i], sorted[j]), i < j; got != want {
				t.Errorf("naturalLess(%q, %q) = %v, want %v", sorted[i], sorted[j], got, want)
			}
		}
	}
}

func TestJustifyInline(t *testing.T) {
	got := justifyInline("Internet-Draft", "ACME-DNS-CHALLENGE", "October 2026", 72)
	want := "Internet-Draft             ACME-DNS-CHALLENGE               October 2026"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	got = justifyInline("Chariton, et al.", "Expires 28 April 2027", "[Page 1]", 72)
	want = "Chariton, et al.          Expires 28 April 2027                 [Page 1]"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestExpandEllipsis(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		// The page number is right-aligned in four columns, and the dots
		// are placed so that they line up from one entry to the next.
		{"1.  Introduction\u20260003", 30, "1.  Introduction . . . . .   3"},
		{"2.  Conventions\u20260012", 30, "2.  Conventions  . . . . .  12"},
		{"3.  Placeholder\u20260000", 30, "3.  Placeholder  . . . . .0000"},
		{"no page number", 30, "no page number"},
	}
	for _, tt := range tests {
		if got := expandEllipsis(tt.in, tt.width); got != tt.want {
			t.Errorf("expandEllipsis(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
		}
	}
}

func TestExpiryDate(t *testing.T) {
	d := &document{year: 2026, month: 10, day: 25, today: testToday}
	if got, want := d.expiryString(), "28 April 2027"; got != want {
		t.Errorf("expiry = %q, want %q", got, want)
	}
}
