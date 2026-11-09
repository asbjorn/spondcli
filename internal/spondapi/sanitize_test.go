package spondapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCleanText(t *testing.T) {
	for in, want := range map[string]string{
		"plain":                    "plain",
		"line\nnext\ttab":          "line\nnext\ttab",
		"\x1b]52;c;aGk=\x07evil":   "]52;c;aGk=evil",
		"\x1b[2J\x1b[Hfake prompt": "[2J[Hfake prompt",
		"c1 \u009b31m csi":         "c1 31m csi",
		"emoji 👍 and æøå stay":     "emoji 👍 and æøå stay",
		"‮RTL override is kept":    "‮RTL override is kept", // not a control (Cf)
	} {
		if got := CleanText(in); got != want {
			t.Errorf("CleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeNested(t *testing.T) {
	type inner struct {
		Text string
		Any  any
	}
	v := struct {
		Name  string
		Ptr   *inner
		List  []inner
		Map   map[string]map[string]int64
		Raw   []byte
		local string
	}{
		Name:  "a\x1bb",
		Ptr:   &inner{Text: "c\x1bd", Any: map[string]any{"k": "e\x1bf", "n": []any{"g\x1bh"}}},
		List:  []inner{{Text: "i\x1bj"}},
		Map:   map[string]map[string]int64{"👍\x1b": {"p": 1}},
		Raw:   []byte("\x1b"),
		local: "\x1b",
	}
	Sanitize(&v)
	if v.Name != "ab" || v.Ptr.Text != "cd" || v.List[0].Text != "ij" {
		t.Errorf("strings not cleaned: %+v", v)
	}
	m := v.Ptr.Any.(map[string]any)
	if m["k"] != "ef" || m["n"].([]any)[0] != "gh" {
		t.Errorf("interface values not cleaned: %#v", m)
	}
	if _, ok := v.Map["👍"]; !ok || len(v.Map) != 1 {
		t.Errorf("map key not cleaned: %#v", v.Map)
	}
	if string(v.Raw) != "\x1b" || v.local != "\x1b" {
		t.Error("raw bytes and unexported fields must be left alone")
	}
}

func TestResponsesAreSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"P1","title":"hi\u001b]0;pwned\u0007","body":"x\u001b[31m"}`))
	}))
	defer srv.Close()
	c := NewClient()
	c.BaseURL = srv.URL + "/"
	p, err := c.Post(context.Background(), "P1")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "hi]0;pwned" || p.Body != "x[31m" {
		t.Errorf("post not sanitized: title %q body %q", p.Title, p.Body)
	}
}
