package resume_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/0xHoaxen/shogun/services/katana/internal/resume"
)

func TestAWordFileKeepsItsParagraphsTabsAndBreaks(t *testing.T) {
	body := para("Priya Raman") +
		`<w:p><w:r><w:t>Go</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>Kubernetes</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>line one</w:t><w:br/><w:t>line two</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t xml:space="preserve">Staff </w:t></w:r><w:r><w:t>Engineer</w:t></w:r></w:p>`

	got, err := resume.Extract(docxOf(t, body, "", ""))

	want := "Priya Raman\nGo\tKubernetes\nline one\nline two\nStaff Engineer"
	if err != nil || got.Text != want || got.Format != resume.FormatDOCX {
		t.Fatalf("got %q (%s), err %v; want %q", got.Text, got.Format, err, want)
	}
}

func TestHeaderAndFooterTextIsReadAroundTheBody(t *testing.T) {
	got, err := resume.Extract(docxOf(t, para("Experience"), "Priya Raman, priya@example.com", "Page 1"))

	if err != nil || got.Text != "Priya Raman, priya@example.com\nExperience\nPage 1" {
		t.Fatalf("got %q, err %v; want the header first and the footer last", got.Text, err)
	}
}

func TestATextBoxIsReadOnceNotTwice(t *testing.T) {
	// Word writes a text box for modern readers and again inside mc:Fallback.
	box := `<mc:AlternateContent><mc:Choice Requires="wps"><w:p><w:r><w:t>Skills: Go</w:t></w:r></w:p></mc:Choice>` +
		`<mc:Fallback><w:p><w:r><w:t>Skills: Go</w:t></w:r></w:p></mc:Fallback></mc:AlternateContent>`

	got, err := resume.Extract(docxOf(t, box+para("After"), "", ""))

	if err != nil || strings.Count(got.Text, "Skills: Go") != 1 || !strings.Contains(got.Text, "After") {
		t.Fatalf("got %q, err %v; want the box once and the text after it", got.Text, err)
	}
}

func TestTextDeletedInTrackedChangesIsNotText(t *testing.T) {
	body := `<w:p><w:r><w:t>Kept</w:t></w:r><w:del><w:r><w:delText>Removed</w:delText></w:r></w:del></w:p>`

	got, err := resume.Extract(docxOf(t, body, "", ""))

	if err != nil || got.Text != "Kept" {
		t.Fatalf("got %q, err %v", got.Text, err)
	}
}

func TestAWordFileWithNoBodyTextHasNoText(t *testing.T) {
	_, err := resume.Extract(docxOf(t, `<w:p/>`, "", ""))

	if err == nil || reasonOf(t, err) != resume.ReasonNoText {
		t.Fatalf("err = %v, want %s", err, resume.ReasonNoText)
	}
}

func TestBrokenWordXMLIsUnreadable(t *testing.T) {
	data := zipOf(t, map[string]string{"word/document.xml": "<w:document><w:body><w:p><w:r><w:t>never closed"})

	_, err := resume.Extract(data)

	if err == nil || reasonOf(t, err) != resume.ReasonUnreadable {
		t.Fatalf("err = %v, want %s", err, resume.ReasonUnreadable)
	}
}

func TestAZipBombIsRefusedWithoutReadingItAll(t *testing.T) {
	// About 30 MB of XML that compresses to a few tens of kilobytes.
	filler := strings.Repeat("<w:p/>", 5_000_000)
	data := zipOf(t, map[string]string{"word/document.xml": wordPart("document", filler)})
	if len(data) > 200_000 {
		t.Fatalf("the fixture is %d bytes; it should be tiny to be a bomb", len(data))
	}

	_, err := resume.Extract(data)

	if err == nil || reasonOf(t, err) != resume.ReasonUnreadable {
		t.Fatalf("err = %v, want %s", err, resume.ReasonUnreadable)
	}
}

func TestAnArchiveWithTooManyFilesIsRefused(t *testing.T) {
	files := map[string]string{"word/document.xml": wordPart("document", para("Priya"))}
	for i := 0; i < 150; i++ {
		files[fmt.Sprintf("word/media/image%d.png", i)] = "x"
	}

	_, err := resume.Extract(zipOf(t, files))

	if err == nil || reasonOf(t, err) != resume.ReasonUnreadable {
		t.Fatalf("err = %v, want %s", err, resume.ReasonUnreadable)
	}
}

func TestTheWholeBudgetIsSharedAcrossParts(t *testing.T) {
	// Three parts of 8 MB each are each under the limit and over it together.
	part := wordPart("document", strings.Repeat("<w:p/>", 1_300_000))
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"word/document.xml", "word/header1.xml", "word/footer1.xml"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(part))
	}
	_ = zw.Close()

	_, err := resume.Extract(buf.Bytes())

	if err == nil || reasonOf(t, err) != resume.ReasonUnreadable {
		t.Fatalf("err = %v, want %s", err, resume.ReasonUnreadable)
	}
}
