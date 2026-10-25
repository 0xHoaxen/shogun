package resume_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
	"strings"
	"testing"
)

const (
	nsW  = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	nsMC = `xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"`
)

// zipOf builds a zip archive from name to content.
func zipOf(t testing.TB, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// wordPart wraps paragraph XML in a document-like root.
func wordPart(root, inner string) string {
	return fmt.Sprintf(`<?xml version="1.0"?><w:%s %s %s><w:body>%s</w:body></w:%s>`, root, nsW, nsMC, inner, root)
}

func para(text string) string { return `<w:p><w:r><w:t>` + text + `</w:t></w:r></w:p>` }

// docxOf is a Word document with a body and optional header and footer text.
func docxOf(t testing.TB, body, header, footer string) []byte {
	files := map[string]string{"word/document.xml": wordPart("document", body)}
	if header != "" {
		files["word/header1.xml"] = wordPart("hdr", para(header))
	}
	if footer != "" {
		files["word/footer1.xml"] = wordPart("ftr", para(footer))
	}
	return zipOf(t, files)
}

// pdfOf builds a PDF with one page per content stream, with a correct
// cross-reference table. trailerExtra goes inside the trailer dictionary.
func pdfOf(streams []string, trailerExtra string) []byte {
	var objs []string
	kids := make([]string, len(streams))
	for i := range streams {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
	}
	objs = append(objs,
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(streams)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	)
	for i, stream := range streams {
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 3 0 R >> >> >>", 5+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		)
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, body := range objs {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R %s >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, trailerExtra, xref)
	return out.Bytes()
}

// textStream draws lines of text, one under the other.
func textStream(lines ...string) string {
	var b strings.Builder
	b.WriteString("BT /F1 12 Tf 14 TL 72 720 Td\n")
	for _, line := range lines {
		fmt.Fprintf(&b, "(%s) Tj T*\n", line)
	}
	b.WriteString("ET")
	return b.String()
}

// shapesStream draws a filled box and no text, like a scanned page does.
const shapesStream = "0 0 1 rg 72 600 200 100 re f"
