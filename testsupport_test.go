package qualflare

import (
	"encoding/base64"
	"sync"

	"github.com/Qualflare/qualflare-go/internal/constants"
	"github.com/Qualflare/qualflare-go/internal/sentinel"
	"github.com/Qualflare/qualflare-go/internal/textutil"
)

func onceReset() sync.Once { return sync.Once{} }

// stepTB is the fake Step is driven with. It satisfies the narrow `stepper`
// interface rather than testing.TB, which cannot be implemented outside the
// testing package -- that constraint is the reason step() exists separately
// from the exported Step().
type stepTB struct {
	f      *fakeTB
	failed bool
}

func (s stepTB) Failed() bool    { return s.failed }
func (s stepTB) Log(args ...any) { s.f.Log(args...) }
func (s stepTB) Helper()         { s.f.Helper() }

// Message constructors mirroring what each public call emits, so the table test
// can compare shapes without reaching through testing.TB (which cannot be
// implemented outside the testing package).
func labelMsg(n, v string) sentinel.Message {
	return sentinel.Message{Kind: sentinel.KindLabel, Name: n, Value: v}
}

func linkMsg(url, typ, name string) sentinel.Message {
	if typ == "" {
		typ = LinkCustom
	}
	return sentinel.Message{Kind: sentinel.KindLink, URL: url, Type: typ, Name: name}
}

func descMsg(t string) sentinel.Message {
	return sentinel.Message{Kind: sentinel.KindDescription, Text: t}
}

func prioMsg(v string) sentinel.Message {
	return sentinel.Message{Kind: sentinel.KindPriority, Value: v}
}

func paramMsg(n, v string) sentinel.Message {
	return sentinel.Message{Kind: sentinel.KindParameter, Name: n, Value: v}
}

func maskedMsg(n string) sentinel.Message {
	return sentinel.Message{Kind: sentinel.KindParameter, Name: n, Masked: true}
}

func tagMsg(tags ...string) sentinel.Message {
	clipped := make([]string, 0, len(tags))
	for _, t := range tags {
		clipped = append(clipped, textutil.Truncate(t, constants.MaxTagLength))
	}
	return sentinel.Message{Kind: sentinel.KindTag, Tags: clipped}
}

func attachMsg(name string, data []byte, mime string) sentinel.Message {
	return sentinel.Message{
		Kind: sentinel.KindAttachment, Name: name, MimeType: mime,
		Content: base64.StdEncoding.EncodeToString(data),
	}
}
