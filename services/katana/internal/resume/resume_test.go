package resume_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/0xHoaxen/shogun/services/katana/internal/resume"
)

func reasonOf(t *testing.T, err error) string {
	t.Helper()
	var e *resume.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v (%T), want a *resume.Error", err, err)
	}
	return e.Reason
}

func TestTextFilesAreReadInTheirEncodings(t *testing.T) {
	utf16le := append([]byte{0xFF, 0xFE}, le(utf16.Encode([]rune("Priya Raman\nGo")))...)
	utf16be := append([]byte{0xFE, 0xFF}, be(utf16.Encode([]rune("Priya Raman\nGo")))...)
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"utf-8", []byte("Priya Raman\nGo"), "Priya Raman\nGo"},
		{"utf-8 with a byte order mark", append([]byte{0xEF, 0xBB, 0xBF}, "Priya Raman\nGo"...), "Priya Raman\nGo"},
		{"utf-16 little endian", utf16le, "Priya Raman\nGo"},
		{"utf-16 big endian", utf16be, "Priya Raman\nGo"},
		{"windows line ends", []byte("a\r\nb\r\nc"), "a\nb\nc"},
		{"old mac line ends", []byte("a\rb\rc"), "a\nb\nc"},
		{"accents and another script", []byte("José Núñez\nअंजलि शर्मा"), "José Núñez\nअंजलि शर्मा"},
		{"extra space and blank lines are tidied", []byte("a    b  \n\n\n\n\nc\t\t d"), "a b\n\nc d"},
		{"control characters go", []byte("a\x07b\x1bc\u200bd"), "abcd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resume.Extract(tt.data)

			if err != nil || got.Text != tt.want || got.Format != resume.FormatText {
				t.Fatalf("got %q (%s), err %v; want %q", got.Text, got.Format, err, tt.want)
			}
		})
	}
}

func le(units []uint16) []byte {
	var out []byte
	for _, u := range units {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

func be(units []uint16) []byte {
	var out []byte
	for _, u := range units {
		out = append(out, byte(u>>8), byte(u))
	}
	return out
}

func TestNotTextIsRefusedInWordsThatDoNotRepeatTheFile(t *testing.T) {
	const secret = "SECRET-MARKER-7"
	tests := []struct {
		name   string
		data   []byte
		reason string
	}{
		{"binary", append([]byte(secret), 0x00, 0x01, 0x02), resume.ReasonUnsupported},
		{"invalid utf-8", append([]byte(secret), 0xC3, 0x28), resume.ReasonUnsupported},
		{"an old Word file", append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, secret...), resume.ReasonUnsupported},
		{"odd-length utf-16", []byte{0xFF, 0xFE, 'a'}, resume.ReasonUnreadable},
		{"a zip that is not Word", zipOf(t, map[string]string{"notes.txt": secret}), resume.ReasonUnsupported},
		{"a broken zip", append([]byte("PK\x03\x04"), secret...), resume.ReasonUnreadable},
		{"a broken pdf", append([]byte("%PDF-1.4\n"), secret...), resume.ReasonUnreadable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resume.Extract(tt.data)

			if err == nil || reasonOf(t, err) != tt.reason {
				t.Fatalf("err = %v, want reason %s", err, tt.reason)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("the message repeats the file: %v", err)
			}
		})
	}
}

func TestOldWordFilesAreToldToSaveAsDocxOrPdf(t *testing.T) {
	_, err := resume.Extract([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1, 0, 0})

	if err == nil || !strings.Contains(err.Error(), "(.docx) or PDF") {
		t.Fatalf("err = %v, want a hint to save as .docx or PDF", err)
	}
}

func TestEmptyFilesHaveNoText(t *testing.T) {
	for name, data := range map[string][]byte{"empty": {}, "only space": []byte(" \n\t \r\n "), "only a byte order mark": {0xEF, 0xBB, 0xBF}} {
		t.Run(name, func(t *testing.T) {
			_, err := resume.Extract(data)

			if err == nil || reasonOf(t, err) != resume.ReasonNoText {
				t.Fatalf("err = %v, want %s", err, resume.ReasonNoText)
			}
		})
	}
}

func TestTheSizeLimitIsTenMegabytesAndIsCheckedBeforeAnythingIsParsed(t *testing.T) {
	line := strings.Repeat("a", 79) + "\n"
	atLimit := []byte(strings.Repeat(line, resume.MaxBytes/len(line)+1)[:resume.MaxBytes])
	got, err := resume.Extract(atLimit)
	if err != nil || !got.Truncated || len([]rune(got.Text)) > resume.MaxChars {
		t.Fatalf("a 10 MB file: err %v, truncated %v, %d chars", err, got.Truncated, len([]rune(got.Text)))
	}

	// A PDF that is one byte over is refused without being parsed (its body would not parse).
	over := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("x"), resume.MaxBytes)...)
	_, err = resume.Extract(over)

	if err == nil || reasonOf(t, err) != resume.ReasonTooLarge {
		t.Fatalf("err = %v, want %s", err, resume.ReasonTooLarge)
	}
}

func TestLongTextIsCutAndSaysSo(t *testing.T) {
	text := strings.Repeat(strings.Repeat("word ", 15)+"\n", 5000) // about 380,000 characters

	got, err := resume.Extract([]byte(text))

	if err != nil || !got.Truncated || len([]rune(got.Text)) > resume.MaxChars || len([]rune(got.Text)) < resume.MaxChars-100 {
		t.Fatalf("err %v, truncated %v, %d chars; want cut to about %d", err, got.Truncated, len([]rune(got.Text)), resume.MaxChars)
	}
	short, _ := resume.Extract([]byte("a short resume"))
	if short.Truncated {
		t.Error("a short resume was marked cut")
	}
}
