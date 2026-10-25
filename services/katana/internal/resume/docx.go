package resume

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"sort"
	"strings"
)

const (
	// maxDOCXEntries is the most files a Word archive may hold. A real resume has
	// about fifteen; a bomb has thousands.
	maxDOCXEntries = 100
	// maxDOCXXMLBytes is the most XML read from one archive, whatever it says its
	// files weigh.
	maxDOCXXMLBytes = 20 << 20

	docxBody = "word/document.xml"
)

// readDOCX reads the text of a Word document: its headers, then its body, then
// its footers. Resumes often keep the name and contact line in a header.
func readDOCX(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fail(ReasonUnreadable, "That Word file could not be opened.")
	}
	if len(zr.File) > maxDOCXEntries {
		return "", fail(ReasonUnreadable, "That Word file could not be read.")
	}
	var headers, footers []*zip.File
	var body *zip.File
	for _, f := range zr.File {
		switch {
		case f.Name == docxBody:
			body = f
		case strings.HasPrefix(f.Name, "word/header") && strings.HasSuffix(f.Name, ".xml"):
			headers = append(headers, f)
		case strings.HasPrefix(f.Name, "word/footer") && strings.HasSuffix(f.Name, ".xml"):
			footers = append(footers, f)
		}
	}
	if body == nil {
		return "", fail(ReasonUnsupported, "That does not look like a Word (.docx) file.")
	}
	byName := func(files []*zip.File) {
		sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	}
	byName(headers)
	byName(footers)

	budget := int64(maxDOCXXMLBytes)
	var out strings.Builder
	for _, f := range append(append(headers, body), footers...) {
		text, used, err := readDOCXPart(f, budget)
		if err != nil {
			return "", err
		}
		budget -= used
		out.WriteString(strings.TrimRight(text, "\n"))
		out.WriteString("\n")
	}
	return out.String(), nil
}

// readDOCXPart reads one XML file of the archive, never more than budget bytes
// of it, and returns its text and how many bytes it used.
func readDOCXPart(f *zip.File, budget int64) (string, int64, error) {
	rc, err := f.Open()
	if err != nil {
		return "", 0, fail(ReasonUnreadable, "That Word file could not be read.")
	}
	defer func() { _ = rc.Close() }()
	counted := &countingReader{r: io.LimitReader(rc, budget+1)}
	text, err := docxText(counted)
	if counted.n > budget {
		return "", 0, fail(ReasonUnreadable, "That Word file is too large to read.")
	}
	if err != nil {
		return "", 0, fail(ReasonUnreadable, "That Word file could not be read.")
	}
	return text, counted.n, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// docxText walks WordprocessingML and returns its text. w:t is text, the end of
// a w:p a new line, w:tab a tab, w:br and w:cr a line break. Word writes a text
// box twice, once for modern readers and once, inside mc:Fallback, for old ones;
// only the first is read. Deleted text (w:delText) is not text.
func docxText(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)
	var out strings.Builder
	var inText bool
	fallback := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return out.String(), nil
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "Fallback":
				fallback++
			case "t":
				inText = fallback == 0
			case "tab":
				if fallback == 0 {
					out.WriteString("\t")
				}
			case "br", "cr":
				if fallback == 0 {
					out.WriteString("\n")
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "Fallback":
				fallback--
			case "t":
				inText = false
			case "p":
				if fallback == 0 {
					out.WriteString("\n")
				}
			}
		case xml.CharData:
			if inText {
				out.Write(t)
			}
		}
	}
}
