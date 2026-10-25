package resume

import (
	"bytes"
	"strings"
	"time"

	"github.com/dslipak/pdf"
)

const (
	// maxPDFPages is the most pages read. A resume is one to four.
	maxPDFPages = 40
	// pdfTimeout is how long a PDF may take to read.
	pdfTimeout = 10 * time.Second
)

// pdfText and pdfLimit are variables so a test can stand in a reader that hangs.
var (
	pdfText  = extractPDFText
	pdfLimit = pdfTimeout
)

type pdfOutcome struct {
	text  string
	pages int
	err   error
}

// readPDF reads a PDF's text with a library that parses untrusted files, so it
// is fenced in: a panic inside it is an unreadable file, and a read that takes
// too long is given up on (its goroutine is left to finish by itself).
func readPDF(data []byte) (string, int, error) {
	done := make(chan pdfOutcome, 1)
	read, limit := pdfText, pdfLimit
	go func() {
		defer func() {
			if recover() != nil {
				done <- pdfOutcome{err: fail(ReasonUnreadable, "That PDF could not be read.")}
			}
		}()
		text, pages, err := read(data)
		done <- pdfOutcome{text: text, pages: pages, err: err}
	}()
	select {
	case out := <-done:
		return out.text, out.pages, out.err
	case <-time.After(limit):
		return "", 0, fail(ReasonUnreadable, "That PDF took too long to read.")
	}
}

// extractPDFText reads up to maxPDFPages pages in order.
func extractPDFText(data []byte) (string, int, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", 0, fail(ReasonUnreadable, "That PDF could not be read. If it is password protected, remove the password and try again.")
	}
	pages := min(r.NumPage(), maxPDFPages)
	var out strings.Builder
	read, failed := 0, 0
	for i := 1; i <= pages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			failed++
			continue
		}
		read++
		out.WriteString(text)
		out.WriteString("\n")
	}
	if read == 0 && failed > 0 {
		return "", 0, fail(ReasonUnreadable, "That PDF could not be read.")
	}
	return out.String(), read, nil
}
