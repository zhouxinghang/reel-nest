package store

import (
	"database/sql/driver"
	"encoding/hex"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/text/cases"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
	"modernc.org/sqlite"
)

// Version 1: x/text v0.30.0 Chinese collation, natural numbers, case insensitive.
// Collators/casers have mutable buffers: never share one between query readers.
var collators = sync.Pool{New: func() any { return collate.New(language.SimplifiedChinese, collate.Numeric, collate.IgnoreCase) }}

func titleKey(s string) string {
	c := collators.Get().(*collate.Collator)
	defer collators.Put(c)
	var b collate.Buffer
	return hex.EncodeToString(c.KeyFromString(&b, norm.NFC.String(s)))
}
func searchKey(s string) string { return cases.Fold().String(norm.NFC.String(s)) }
func displayTitle(s string) string {
	r := []rune(s)
	if len(r) > 512 {
		return string(r[:512])
	}
	return s
}
func init() {
	for name, f := range map[string]func(string) string{
		"rn_title_key_v1": titleKey, "rn_search_v1": searchKey,
		"rn_filename":    func(s string) string { return filepath.Base(s) },
		"rn_local_title": func(s string) string { return displayTitle(strings.TrimSuffix(filepath.Base(s), filepath.Ext(s))) },
	} {
		sqlite.MustRegisterDeterministicScalarFunction(name, 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			s, _ := args[0].(string)
			return f(s), nil
		})
	}
}
