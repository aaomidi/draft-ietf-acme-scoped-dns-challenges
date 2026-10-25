# Rendering test fixtures

Each `.xml` file here is an RFCXML document. The `.txt` and `.html` files
beside it are what **xml2rfc** renders it to. The tests check that this
package produces the same text, byte for byte, and an equivalent page: the
same elements, attributes and text, compared without regard to white space
that a browser does not show.

The `.txt` and `.html` files must come from xml2rfc, never from this package
itself, or the tests would only show that it agrees with itself. To
regenerate them:

    xml2rfc --date 2026-10-25 --text --v3 NAME.xml -o NAME.txt
    xml2rfc --date 2026-10-25 --html --v3 NAME.xml -o NAME.html

They were generated with xml2rfc 3.34.1. The `--date` matters: xml2rfc puts
the year of the build date in the copyright notice, and the tests render
with the same date.

| Fixture | What it is |
|---------|------------|
| `draft` | The dns-account-01 draft itself. |
| `rich`  | A made-up draft using everything `mkd2xml` can produce: emphasis, links, source code, long titles, numbered appendices, a note to be removed, a list of more than nine items, authors with and without organizations. |
| `long`  | A made-up draft of about 50 sections, so that the table of contents spans a page break. |
