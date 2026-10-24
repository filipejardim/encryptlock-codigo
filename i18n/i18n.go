// Package i18n holds every text EncryptLock shows to people, one JSON file per
// language in locales/ (English is the base). No user-facing text is written
// directly in the code: it is looked up here by key.
//
// To add a language, add locales/<code>.json with every key of en.json; the
// tests fail if a key or a %-placeholder is missing.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
)

// Base is the base language: any missing text falls back to it.
const Base = "en"

// EnvVar selects the language (e.g. LOCKKEEPER_LANG=pt-PT).
const EnvVar = "LOCKKEEPER_LANG"

//go:embed locales/*.json
var files embed.FS

var catalogs = load()

func load() map[string]map[string]string {
	entries, err := files.ReadDir("locales")
	if err != nil {
		panic(err)
	}
	out := map[string]map[string]string{}
	for _, e := range entries {
		bz, err := files.ReadFile(path.Join("locales", e.Name()))
		if err != nil {
			panic(err)
		}
		var texts map[string]string
		if err := json.Unmarshal(bz, &texts); err != nil {
			panic(fmt.Errorf("i18n: %s: %w", e.Name(), err))
		}
		out[strings.TrimSuffix(e.Name(), ".json")] = texts
	}
	return out
}

// Languages returns the available language codes, sorted.
func Languages() []string {
	codes := make([]string, 0, len(catalogs))
	for code := range catalogs {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// Translator returns texts in one language.
type Translator struct{ lang string }

// New returns a translator for lang ("pt-PT", "pt", "en", ...). An unknown or
// empty language uses the environment variable, then English.
func New(lang string) Translator {
	for _, candidate := range []string{lang, os.Getenv(EnvVar)} {
		if code := match(candidate); code != "" {
			return Translator{lang: code}
		}
	}
	return Translator{lang: Base}
}

// match finds the catalog for a language code, also accepting "pt" for
// "pt-PT" and "pt_PT" for "pt-PT".
func match(lang string) string {
	lang = strings.ReplaceAll(strings.TrimSpace(lang), "_", "-")
	if lang == "" {
		return ""
	}
	for code := range catalogs {
		if strings.EqualFold(code, lang) {
			return code
		}
	}
	for _, code := range Languages() {
		if strings.HasPrefix(strings.ToLower(code), strings.ToLower(lang)+"-") {
			return code
		}
	}
	return ""
}

// Lang returns the language in use.
func (t Translator) Lang() string { return t.lang }

// T returns the text for key, formatted with args (fmt.Sprintf).
func (t Translator) T(key string, args ...any) string {
	text, ok := catalogs[t.lang][key]
	if !ok {
		text, ok = catalogs[Base][key]
	}
	if !ok {
		return key
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}
