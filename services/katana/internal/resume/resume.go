// Package resume turns an uploaded resume into plain text, in memory. The file
// is never written anywhere: it arrives as bytes and leaves as a string, which
// is what the owner's decision about resumes (D4) requires. It reads PDF, Word
// (.docx) and text files.
package resume

import (
	"bytes"
	"fmt"
)

const (
	// MaxBytes is the largest file Extract reads.
	MaxBytes = 10 << 20
	// MaxChars is the most text Extract returns. A longer resume is cut and
	// Result.Truncated says so.
	MaxChars = 200_000
)

// Format is the kind of file a resume turned out to be.
type Format string

// The formats Extract reads.
const (
	FormatPDF  Format = "pdf"
	FormatDOCX Format = "docx"
	FormatText Format = "text"
)

// Reasons of an *Error, stable so a screen can switch on them.
const (
	ReasonTooLarge    = "RESUME_TOO_LARGE"
	ReasonUnsupported = "RESUME_UNSUPPORTED"
	ReasonUnreadable  = "RESUME_UNREADABLE"
	ReasonNoText      = "RESUME_NO_TEXT"
)

// Error is a file that could not be read, with words for the owner. Its message
// never holds any of the file's own text.
type Error struct {
	Reason string
	Msg    string
}

func (e *Error) Error() string { return e.Reason + ": " + e.Msg }

func fail(reason, format string, args ...any) *Error {
	return &Error{Reason: reason, Msg: fmt.Sprintf(format, args...)}
}

// Result is the text of a resume.
type Result struct {
	Text   string
	Format Format
	// Pages is how many pages of a PDF were read; zero for other formats.
	Pages int
	// Truncated is true when the text was cut at MaxChars.
	Truncated bool
}

var (
	pdfMagic  = []byte("%PDF-")
	zipMagic  = []byte("PK\x03\x04")
	oleMagic  = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
	bomUTF8   = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16B = []byte{0xFE, 0xFF}
	bomUTF16L = []byte{0xFF, 0xFE}
)

// Extract reads data as a PDF, Word or text file, deciding which from the bytes
// and not from any file name, and returns its text. A failure is an *Error.
func Extract(data []byte) (Result, error) {
	if len(data) > MaxBytes {
		return Result{}, fail(ReasonTooLarge, "That file is larger than %d MB.", MaxBytes>>20)
	}
	var (
		text   string
		format Format
		pages  int
		err    error
	)
	switch {
	case bytes.HasPrefix(data, pdfMagic):
		format = FormatPDF
		text, pages, err = readPDF(data)
	case bytes.HasPrefix(data, zipMagic):
		format = FormatDOCX
		text, err = readDOCX(data)
	case bytes.HasPrefix(data, oleMagic):
		return Result{}, fail(ReasonUnsupported, "That is an old Word file (.doc). Save it as Word (.docx) or PDF and try again.")
	default:
		format = FormatText
		text, err = readText(data)
	}
	if err != nil {
		return Result{}, err
	}
	text, truncated := clean(text)
	if text == "" {
		return Result{}, noText(format)
	}
	return Result{Text: text, Format: format, Pages: pages, Truncated: truncated}, nil
}

func noText(format Format) error {
	if format == FormatPDF {
		return fail(ReasonNoText, "This looks like a scan. Upload a PDF or Word file with selectable text.")
	}
	return fail(ReasonNoText, "There is no text in that file.")
}
