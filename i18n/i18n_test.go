package i18n

import (
	"regexp"
	"testing"
)

var verbs = regexp.MustCompile(`%[-+# 0]*[0-9]*(\.[0-9]+)?[a-zA-Z%]`)

// Every language must have exactly the keys of the base language, with the
// same %-placeholders in the same order.
func TestEveryLanguageHasEveryText(t *testing.T) {
	base := catalogs[Base]
	if len(base) == 0 {
		t.Fatal("base language has no texts")
	}
	for _, lang := range Languages() {
		texts := catalogs[lang]
		for key, baseText := range base {
			text, ok := texts[key]
			if !ok {
				t.Errorf("%s: missing text %q", lang, key)
				continue
			}
			if text == "" {
				t.Errorf("%s: empty text %q", lang, key)
			}
			want, got := verbs.FindAllString(baseText, -1), verbs.FindAllString(text, -1)
			if len(want) != len(got) {
				t.Errorf("%s: %q has placeholders %v, base has %v", lang, key, got, want)
				continue
			}
			for i := range want {
				if want[i] != got[i] {
					t.Errorf("%s: %q placeholder %d is %s, base has %s", lang, key, i, got[i], want[i])
				}
			}
		}
		for key := range texts {
			if _, ok := base[key]; !ok {
				t.Errorf("%s: text %q does not exist in the base language", lang, key)
			}
		}
	}
}

func TestLanguageSelection(t *testing.T) {
	t.Setenv(EnvVar, "")
	cases := map[string]string{"pt-PT": "pt-PT", "pt": "pt-PT", "pt_PT": "pt-PT", "PT-pt": "pt-PT", "en": "en", "": "en", "xx": "en"}
	for in, want := range cases {
		if got := New(in).Lang(); got != want {
			t.Errorf("New(%q) = %s, want %s", in, got, want)
		}
	}
	t.Setenv(EnvVar, "pt-PT")
	if got := New("").Lang(); got != "pt-PT" {
		t.Errorf("with %s=pt-PT, New(\"\") = %s", EnvVar, got)
	}
}

func TestTextsAreFormatted(t *testing.T) {
	tr := New("pt-PT")
	if got := tr.T("accept.done", 3, "ana"); got != "Transferência #3 aceite por ana." {
		t.Errorf("got %q", got)
	}
	if got := tr.T("no.such.key"); got != "no.such.key" {
		t.Errorf("missing key gave %q", got)
	}
}
