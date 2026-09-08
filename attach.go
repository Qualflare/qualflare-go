package qualflare

import (
	"encoding/base64"
	"testing"

	"github.com/Qualflare/qualflare-go/internal/sentinel"
)

// Attach records in-memory bytes against the current test.
//
// The payload travels inline, base64-encoded, inside a single log line, so it
// is bounded: anything that will not fit is dropped with a warning rather than
// truncated, because half a base64 payload is not a usable file. For large
// artifacts, write the file and reference it from your own storage.
func Attach(tb testing.TB, name string, data []byte, mimeType string) {
	emit(tb, sentinel.Message{
		Kind:     sentinel.KindAttachment,
		Name:     name,
		MimeType: mimeType,
		Content:  base64.StdEncoding.EncodeToString(data),
	})
}

// AttachText records a string. The mime type defaults to text/plain.
func AttachText(tb testing.TB, name, text, mimeType string) {
	if mimeType == "" {
		mimeType = "text/plain"
	}
	Attach(tb, name, []byte(text), mimeType)
}
