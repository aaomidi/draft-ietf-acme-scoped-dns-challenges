package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RefSource returns the bibxml <reference> element for a reference key such as
// "RFC8555" or "I-D.ietf-acme-ari".
type RefSource interface {
	Reference(key string) ([]byte, error)
}

// CachedFetcher fetches references from the IETF bibxml services and keeps a
// copy in Dir, so that later runs need no network access.
type CachedFetcher struct {
	Dir     string
	Offline bool
	Client  *http.Client
}

var (
	rfcKey   = regexp.MustCompile(`^RFC(\d+)$`)
	draftKey = regexp.MustCompile(`^I-D\.(?:draft-)?([a-z0-9][a-z0-9-]*)$`)
)

// refLocation maps a reference key to its cache file name and source URL.
func refLocation(key string) (file, url string, err error) {
	if m := rfcKey.FindStringSubmatch(key); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return "", "", fmt.Errorf("bad RFC number in %q", key)
		}
		file = fmt.Sprintf("reference.RFC.%04d.xml", n)
		return file, "https://bib.ietf.org/public/rfc/bibxml/" + file, nil
	}
	if m := draftKey.FindStringSubmatch(key); m != nil {
		file = fmt.Sprintf("reference.I-D.%s.xml", m[1])
		return file, "https://datatracker.ietf.org/doc/bibxml3/draft-" + m[1] + ".xml", nil
	}
	return "", "", fmt.Errorf("cannot fetch reference %q: only RFCnnnn and I-D.name are fetched; define others in the front matter", key)
}

func (f *CachedFetcher) Reference(key string) ([]byte, error) {
	file, url, err := refLocation(key)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(f.Dir, file)
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if f.Offline {
		return nil, fmt.Errorf("reference %s is not in %s and -offline is set", key, f.Dir)
	}

	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching reference %s: %v", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching reference %s: %s returned %s", key, url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("fetching reference %s: %v", key, err)
	}
	// Only cache what is usable, so a bad response is not kept forever.
	if _, err := normalizeReference(key, data); err != nil {
		return nil, fmt.Errorf("fetching reference %s from %s: %v", key, url, err)
	}

	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, err
	}
	return data, nil
}

var (
	xmlProlog    = regexp.MustCompile(`^\s*<\?xml[^>]*\?>`)
	referenceTag = regexp.MustCompile(`^<reference\b[^>]*>`)
)

// normalizeReference prepares a fetched <reference> for inclusion: the anchor
// becomes the key used in the draft, and the target is dropped so that the
// renderer derives the canonical URL from the series information, as
// kramdown-rfc does.
func normalizeReference(key string, data []byte) (string, error) {
	s := strings.TrimSpace(xmlProlog.ReplaceAllString(string(data), ""))
	if !referenceTag.MatchString(s) {
		return "", errors.New("not a <reference> element")
	}
	s = referenceTag.ReplaceAllString(s, `<reference anchor="`+escAttr(key)+`">`)

	dec := xml.NewDecoder(strings.NewReader(s))
	for {
		if _, err := dec.Token(); err == io.EOF {
			break
		} else if err != nil {
			return "", fmt.Errorf("not well-formed XML: %v", err)
		}
	}
	return s, nil
}

// references collects what the draft cites, split into the two lists of the
// References section.
type references struct {
	normative   []referenceEntry
	informative []referenceEntry
	known       map[string]bool
}

func newReferences(fm *frontMatter) (*references, error) {
	r := &references{known: map[string]bool{}}
	for _, list := range []struct {
		entries referenceMap
		dst     *[]referenceEntry
	}{
		{fm.Normative, &r.normative},
		{fm.Informative, &r.informative},
	} {
		for _, e := range list.entries {
			if r.known[e.Key] {
				return nil, fmt.Errorf("front matter: reference %s is defined twice", e.Key)
			}
			r.known[e.Key] = true
			*list.dst = append(*list.dst, e)
		}
	}
	return r, nil
}

// cite records a citation made with {{!KEY}} (normative) or {{?KEY}}
// (informative). A key that is already listed keeps its place.
func (r *references) cite(key string, normative bool) {
	if r.known[key] {
		return
	}
	r.known[key] = true
	if normative {
		r.normative = append(r.normative, referenceEntry{Key: key})
	} else {
		r.informative = append(r.informative, referenceEntry{Key: key})
	}
}

func (r *references) render(out *xmlBuf, src RefSource) error {
	lists := []struct {
		anchor, name string
		entries      []referenceEntry
	}{
		{"sec-normative-references", "Normative References", r.normative},
		{"sec-informative-references", "Informative References", r.informative},
	}
	nonEmpty := 0
	for _, l := range lists {
		if len(l.entries) > 0 {
			nonEmpty++
		}
	}
	if nonEmpty == 0 {
		return nil
	}
	if nonEmpty == 2 {
		out.open(`<references anchor="sec-combined-references">`)
		out.line("<name>References</name>")
	}
	for _, l := range lists {
		if len(l.entries) == 0 {
			continue
		}
		out.open(`<references anchor="` + l.anchor + `">`)
		out.line("<name>" + l.name + "</name>")
		for _, e := range l.entries {
			if e.Def != nil {
				if err := renderReferenceDef(out, e.Key, e.Def); err != nil {
					return err
				}
				continue
			}
			if src == nil {
				return fmt.Errorf("reference %s needs to be fetched, but no reference source is configured", e.Key)
			}
			data, err := src.Reference(e.Key)
			if err != nil {
				return err
			}
			ref, err := normalizeReference(e.Key, data)
			if err != nil {
				return fmt.Errorf("reference %s: %v", e.Key, err)
			}
			for _, line := range strings.Split(ref, "\n") {
				out.line(strings.TrimRight(line, " \t\r"))
			}
		}
		out.close("</references>")
	}
	if nonEmpty == 2 {
		out.close("</references>")
	}
	return nil
}

func renderReferenceDef(out *xmlBuf, key string, def *referenceDef) error {
	tag := `<reference anchor="` + escAttr(key) + `"`
	if def.Target != "" {
		tag += ` target="` + escAttr(def.Target) + `"`
	}
	out.open(tag + ">")
	out.open("<front>")
	out.line("<title>" + escText(def.Title) + "</title>")
	if len(def.Author) == 0 {
		out.line("<author/>")
	}
	for _, a := range def.Author {
		tag := "<author"
		if a.Name != "" {
			tag += ` fullname="` + escAttr(a.Name) + `"`
		}
		if a.Ins != "" {
			// "ins" is "J. Smith": initials, then the surname.
			initials, surname, ok := strings.Cut(a.Ins, " ")
			if !ok {
				return fmt.Errorf("reference %s: author ins %q is not of the form %q", key, a.Ins, "J. Smith")
			}
			tag += ` initials="` + escAttr(initials) + `" surname="` + escAttr(surname) + `"`
		}
		if a.Org == "" {
			out.line(tag + "/>")
			continue
		}
		out.open(tag + ">")
		out.line("<organization>" + escText(a.Org) + "</organization>")
		out.close("</author>")
	}
	if def.Date == "" {
		out.line("<date>n.d.</date>")
	} else {
		d, err := parseDate(def.Date)
		if err != nil {
			return fmt.Errorf("reference %s: %v", key, err)
		}
		out.line(dateElement(d))
	}
	out.close("</front>")
	out.close("</reference>")
	return nil
}

func dateElement(d xmlDate) string {
	s := `<date year="` + d.Year + `"`
	if d.Month != "" {
		s += ` month="` + d.Month + `"`
	}
	if d.Day != "" {
		s += ` day="` + d.Day + `"`
	}
	return s + "/>"
}
