package resume_test

import (
	"strings"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/katana/internal/resume"
)

func TestAPDFIsReadPageByPageInOrder(t *testing.T) {
	data := pdfOf([]string{textStream("Priya Raman", "Staff Engineer"), textStream("Experience", "Lumen")}, "")

	got, err := resume.Extract(data)

	if err != nil || got.Format != resume.FormatPDF || got.Pages != 2 {
		t.Fatalf("err %v, format %s, pages %d", err, got.Format, got.Pages)
	}
	for _, want := range []string{"Priya Raman", "Staff Engineer", "Experience", "Lumen"} {
		if !strings.Contains(got.Text, want) {
			t.Errorf("text %q lacks %q", got.Text, want)
		}
	}
	if strings.Index(got.Text, "Priya Raman") > strings.Index(got.Text, "Experience") {
		t.Errorf("page two came before page one: %q", got.Text)
	}
}

func TestAPDFOfOnlyShapesLooksLikeAScan(t *testing.T) {
	_, err := resume.Extract(pdfOf([]string{shapesStream}, ""))

	if err == nil || reasonOf(t, err) != resume.ReasonNoText || !strings.Contains(err.Error(), "scan") {
		t.Fatalf("err = %v, want %s that says it looks like a scan", err, resume.ReasonNoText)
	}
}

func TestDamagedPDFsAreUnreadableAndNeverPanic(t *testing.T) {
	good := pdfOf([]string{textStream("Priya Raman")}, "")
	tests := map[string][]byte{
		"cut in half":        good[:len(good)/2],
		"cut before the end": good[:len(good)-30],
		"only a header":      []byte("%PDF-1.4\n"),
		"garbage after":      append([]byte("%PDF-1.4\n"), []byte("\x00\x01 not a pdf \xff\xfe")...),
		"no cross-reference": []byte(strings.Replace(string(good), "xref", "xxxx", 1)),
		"wrong offsets":      []byte(strings.ReplaceAll(string(good), "0000000009", "0000000999")),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := resume.Extract(data)

			if err == nil {
				t.Fatal("a damaged PDF was read as if it were fine")
			}
			if r := reasonOf(t, err); r != resume.ReasonUnreadable && r != resume.ReasonNoText {
				t.Fatalf("reason %s, want unreadable or no text", r)
			}
		})
	}
}

func TestAnEncryptedPDFIsUnreadableAndSaysToRemoveThePassword(t *testing.T) {
	owner, user := strings.Repeat("a", 32), strings.Repeat("b", 32)
	enc := "/Encrypt << /Filter /Standard /V 1 /R 2 /P -4 /O (" + owner + ") /U (" + user + ") >> /ID [<00112233445566778899aabbccddeeff> <00112233445566778899aabbccddeeff>]"

	_, err := resume.Extract(pdfOf([]string{textStream("Priya Raman")}, enc))

	if err == nil || reasonOf(t, err) != resume.ReasonUnreadable {
		t.Fatalf("err = %v, want %s", err, resume.ReasonUnreadable)
	}
}

func TestOnlyTheFirstFortyPagesAreRead(t *testing.T) {
	streams := make([]string, 45)
	for i := range streams {
		streams[i] = textStream("Page marker")
	}

	got, err := resume.Extract(pdfOf(streams, ""))

	if err != nil || got.Pages != 40 {
		t.Fatalf("err %v, pages %d, want 40", err, got.Pages)
	}
}

func TestAPDFReaderThatHangsIsGivenUpOn(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	restore := resume.StandInPDFReader(func([]byte) (string, int, error) {
		<-release
		return "", 0, nil
	}, 50*time.Millisecond)
	t.Cleanup(restore)

	start := time.Now()
	_, err := resume.Extract([]byte("%PDF-1.4\n"))

	if err == nil || reasonOf(t, err) != resume.ReasonUnreadable {
		t.Fatalf("err = %v, want %s", err, resume.ReasonUnreadable)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %v to give up", time.Since(start))
	}
}

func TestAPDFReaderThatPanicsIsAnUnreadableFile(t *testing.T) {
	restore := resume.StandInPDFReader(func([]byte) (string, int, error) { panic("boom") }, time.Second)
	t.Cleanup(restore)

	_, err := resume.Extract([]byte("%PDF-1.4\n"))

	if err == nil || reasonOf(t, err) != resume.ReasonUnreadable {
		t.Fatalf("err = %v, want %s", err, resume.ReasonUnreadable)
	}
}
