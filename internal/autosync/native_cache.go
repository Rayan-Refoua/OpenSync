package autosync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

func pythonFloat(v float64) string {
	format := byte('e')
	if v == 0 || (math.Abs(v) >= 1e-4 && math.Abs(v) < 1e16) {
		format = 'f'
	}
	s := strconv.FormatFloat(v, format, -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// pythonJSON is a compatibility codec for existing cache identities only.
// It preserves parsed numeric tokens, spaces, and ASCII escaping used by the
// previous writer. No interpreter or legacy runtime is needed to resume.
func pythonJSON(value any) ([]byte, error) {
	var b bytes.Buffer
	var emit func(any) error
	quote := func(s string) string {
		var o strings.Builder
		o.WriteByte('"')
		for _, r := range s {
			switch r {
			case '"':
				o.WriteString("\\\"")
			case '\\':
				o.WriteString("\\\\")
			case '\b':
				o.WriteString("\\b")
			case '\f':
				o.WriteString("\\f")
			case '\n':
				o.WriteString("\\n")
			case '\r':
				o.WriteString("\\r")
			case '\t':
				o.WriteString("\\t")
			default:
				if r < 32 || r >= 127 {
					if r <= 0xffff {
						fmt.Fprintf(&o, "\\u%04x", r)
					} else {
						hi, lo := utf16.EncodeRune(r)
						fmt.Fprintf(&o, "\\u%04x\\u%04x", hi, lo)
					}
				} else {
					o.WriteRune(r)
				}
			}
		}
		o.WriteByte('"')
		return o.String()
	}
	emit = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			keys := []string{}
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(quote(k))
				b.WriteString(": ")
				if err := emit(x[k]); err != nil {
					return err
				}
			}
			b.WriteByte('}')
		case []any:
			b.WriteByte('[')
			for i, v := range x {
				if i > 0 {
					b.WriteString(", ")
				}
				if err := emit(v); err != nil {
					return err
				}
			}
			b.WriteByte(']')
		case string:
			b.WriteString(quote(x))
		case json.Number:
			b.WriteString(string(x))
		case float64:
			b.WriteString(pythonFloat(x))
		default:
			data, err := json.Marshal(v)
			if err != nil {
				return err
			}
			b.Write(data)
		}
		return nil
	}
	err := emit(value)
	return b.Bytes(), err
}
func rawObject(data []byte) (map[string]any, error) {
	var v map[string]any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	err := d.Decode(&v)
	return v, err
}
func shaBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func legacyPairDigest(pair NativePair, reference, query string) string {
	p := pair.raw
	if p == nil {
		candidates := []any{}
		for _, c := range pair.Candidates {
			m := map[string]any{"offset": c.Offset}
			if c.Votes != 0 {
				m["votes"] = c.Votes
			}
			if c.EnvelopeScore != 0 {
				m["envelope_score"] = c.EnvelopeScore
			}
			if c.Method != "" {
				m["method"] = c.Method
			}
			if c.AnchorTime != 0 {
				m["anchor_time"] = c.AnchorTime
			}
			if c.VerificationWindow > 0 {
				m["verification_window_seconds"] = c.VerificationWindow
				m["verification_max_windows"] = c.VerificationMaxWindows
			}
			candidates = append(candidates, m)
		}
		p = map[string]any{"reference": pair.Reference, "query": pair.Query, "candidates": candidates}
	}
	data, err := pythonJSON(map[string]any{"pair": p, "reference": reference, "query": query, "verifier": "v2"})
	if err != nil {
		return ""
	}
	return shaBytes(data)
}
func (p *NativePair) UnmarshalJSON(data []byte) error {
	type plain NativePair
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*p = NativePair(v)
	raw, err := rawObject(data)
	p.raw = raw
	return err
}
