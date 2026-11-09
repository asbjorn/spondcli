package spondapi

import (
	"reflect"
	"strings"
	"unicode"
)

// CleanText removes control characters other than newline and tab. Text
// from the API is written by other users and ends up on the terminal; an
// escape sequence in a message could otherwise move the cursor, retitle
// the window or (on some terminals) write the clipboard.
func CleanText(s string) string {
	if strings.IndexFunc(s, isUnsafe) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		if isUnsafe(r) {
			return -1
		}
		return r
	}, s)
}

func isUnsafe(r rune) bool {
	return r != '\n' && r != '\t' && unicode.IsControl(r)
}

// Sanitize applies CleanText to every string reachable from v, which must
// be a pointer: struct fields, slice and array elements, map keys and
// values, and values held in interfaces. Unexported fields are left alone.
func Sanitize(v any) {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		sanitizeValue(rv.Elem())
	}
}

func sanitizeValue(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			if s := v.String(); CleanText(s) != s {
				v.SetString(CleanText(s))
			}
		}
	case reflect.Pointer:
		if !v.IsNil() {
			sanitizeValue(v.Elem())
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				sanitizeValue(f)
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return // raw bytes, e.g. json.RawMessage
		}
		for i := range v.Len() {
			sanitizeValue(v.Index(i))
		}
	case reflect.Map:
		if v.IsNil() {
			return
		}
		for _, k := range v.MapKeys() {
			val := reflect.New(v.Type().Elem()).Elem()
			val.Set(v.MapIndex(k))
			sanitizeValue(val)
			nk := k
			if k.Kind() == reflect.String && CleanText(k.String()) != k.String() {
				v.SetMapIndex(k, reflect.Value{})
				nk = reflect.ValueOf(CleanText(k.String())).Convert(k.Type())
			}
			v.SetMapIndex(nk, val)
		}
	case reflect.Interface:
		if v.IsNil() || !v.CanSet() {
			return
		}
		inner := reflect.New(v.Elem().Type()).Elem()
		inner.Set(v.Elem())
		sanitizeValue(inner)
		v.Set(inner)
	}
}
