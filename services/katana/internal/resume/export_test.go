package resume

import "time"

// StandInPDFReader swaps the PDF reader and its time limit for a test, and
// returns a function that puts them back. Tests that use it must not run in
// parallel.
func StandInPDFReader(read func([]byte) (string, int, error), limit time.Duration) (restore func()) {
	oldRead, oldLimit := pdfText, pdfLimit
	pdfText, pdfLimit = read, limit
	return func() { pdfText, pdfLimit = oldRead, oldLimit }
}
