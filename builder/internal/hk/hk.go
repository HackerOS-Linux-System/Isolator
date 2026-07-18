package hk

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// hk.go implements the .hk configuration format from scratch (no external
// YAML/JSON library involved), per the HackerOS spec:
// https://hackeros-linux-system.github.io/HackerOS-Website/tools-docs/hk.html
//
// Isolator uses .hk instead of JSON for every *local* config/state file
// (config.hk, installed.hk, snapshots.hk). The remote repository list stays
// JSON since it's a plain distribution/interchange format pulled over HTTP.
// ---------------------------------------------------------------------------

// HkKind identifies the dynamic type carried by an HkValue.
type HkKind int

const (
	HkString HkKind = iota
	HkNumber
	HkBool
	HkArray
	HkMapKind
)

// HkValue is a single dynamically-typed .hk value — a tagged union mirroring
// the "enum HkValue" described in the spec's Rust API, translated to Go.
type HkValue struct {
	Kind   HkKind
	Str    string
	Num    float64
	Bool   bool
	Arr    []HkValue
	MapVal *HkMap
}

// HkMap is an order-preserving string-keyed map (the Go equivalent of the
// spec's IndexMap<String, HkValue>).
type HkMap struct {
	keys   []string
	values map[string]HkValue
}

func NewHkMap() *HkMap {
	return &HkMap{values: map[string]HkValue{}}
}

func (m *HkMap) Set(key string, v HkValue) {
	if _, exists := m.values[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.values[key] = v
}

func (m *HkMap) Get(key string) (HkValue, bool) {
	v, ok := m.values[key]
	return v, ok
}

func (m *HkMap) Delete(key string) {
	if _, exists := m.values[key]; !exists {
		return
	}
	delete(m.values, key)
	for i, k := range m.keys {
		if k == key {
			m.keys = append(m.keys[:i], m.keys[i+1:]...)
			break
		}
	}
}

func (m *HkMap) Keys() []string { return m.keys }
func (m *HkMap) Len() int       { return len(m.keys) }

// HkDocument is a parsed .hk file: an ordered map of section name -> HkMap.
type HkDocument struct {
	Sections *HkMap
}

func NewHkDocument() *HkDocument {
	return &HkDocument{Sections: NewHkMap()}
}

func (d *HkDocument) Section(name string) *HkMap {
	v, ok := d.Sections.Get(name)
	if ok && v.Kind == HkMapKind {
		return v.MapVal
	}
	m := NewHkMap()
	d.Sections.Set(name, HkValue{Kind: HkMapKind, MapVal: m})
	return m
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

type HkError struct {
	Line, Column int
	Message      string
	Hint         string
}

func (e *HkError) Error() string {
	if e.Line > 0 {
		msg := fmt.Sprintf("hk parse error at %d:%d: %s", e.Line, e.Column, e.Message)
		if e.Hint != "" {
			msg += "\n  hint: " + e.Hint
		}
		return msg
	}
	return "hk error: " + e.Message
}

func errKeyConflict(line int, key string) *HkError {
	return &HkError{Line: line, Message: fmt.Sprintf("KeyConflict(%s): key already exists with a different shape", key)}
}

func errDepthJump(line, depth int) *HkError {
	return &HkError{Line: line, Message: fmt.Sprintf("unexpected nesting depth %d — you can only go one level deeper than the currently open map", depth),
		Hint: "Add intermediate '-> key' lines, or reduce the number of leading dashes."}
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

var (
	sectionRe = regexp.MustCompile(`^\[([^\]]+)\]\s*$`)
	// One or more leading dashes followed by '>' and whitespace, e.g. "->", "-->", "--->"
	keyLineRe = regexp.MustCompile(`^(-+)>\s*(.+)$`)
)

// ParseHK parses raw .hk source into an HkDocument. Comments ('!' lines),
// blank lines, sections, dash-depth nesting, dotted-key shorthand, inline
// submaps, and all five value types are supported. Interpolation is NOT
// resolved here — call ResolveInterpolations afterwards if you want
// ${...} references expanded.
func ParseHK(input string) (*HkDocument, error) {
	doc := NewHkDocument()
	lines := strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")

	var currentSection *HkMap
	// stack[0] is always the current section's root map. stack[i] (i>=1) is
	// the map opened by the most recent inline-submap-declaring line at
	// depth i.
	var stack []*HkMap

	for i, raw := range lines {
		lineNo := i + 1
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "!") {
			continue
		}

		if m := sectionRe.FindStringSubmatch(trimmed); m != nil {
			name := strings.TrimSpace(m[1])
			currentSection = doc.Section(name)
			stack = []*HkMap{currentSection}
			continue
		}

		if currentSection == nil {
			return nil, &HkError{Line: lineNo, Column: 1, Message: "content outside of any [section]",
				Hint: "Start the file with a section header, e.g. [metadata]"}
		}

		m := keyLineRe.FindStringSubmatch(line)
		if m == nil {
			return nil, &HkError{Line: lineNo, Column: 1, Message: "expected '! comment', '[section]', or '-> key => value'"}
		}
		depth := len(m[1])
		rest := m[2]

		if depth > len(stack) {
			return nil, errDepthJump(lineNo, depth)
		}
		// Truncate stack to exactly `depth` entries — this closes any
		// deeper maps that were open, and gives us the correct parent.
		stack = stack[:depth]
		parent := stack[depth-1]

		key, valueRaw, hasValue := splitKeyValue(rest)

		if !hasValue {
			// Inline submap declarator: "-> name" with no "=> value".
			newMap := NewHkMap()
			if err := setDotted(parent, key, HkValue{Kind: HkMapKind, MapVal: newMap}, lineNo); err != nil {
				return nil, err
			}
			stack = append(stack, newMap)
			continue
		}

		val, err := parseHkScalarOrArray(strings.TrimSpace(valueRaw), lineNo)
		if err != nil {
			return nil, err
		}
		if err := setDotted(parent, key, val, lineNo); err != nil {
			return nil, err
		}
	}

	return doc, nil
}

// LoadHKFile reads and parses a .hk file from disk.
func LoadHKFile(path string) (*HkDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseHK(string(data))
}

// splitKeyValue splits "key => value" into ("key", "value", true), or
// returns (rest, "", false) if there's no top-level "=>".
func splitKeyValue(s string) (string, string, bool) {
	idx := strings.Index(s, "=>")
	if idx == -1 {
		return strings.TrimSpace(s), "", false
	}
	key := strings.TrimSpace(s[:idx])
	val := s[idx+2:]
	return key, val, true
}

// setDotted resolves a (possibly dotted, possibly quoted) key inside
// parent and stores value there, creating intermediate maps as needed.
func setDotted(parent *HkMap, key string, value HkValue, lineNo int) error {
	if len(key) >= 2 && key[0] == '"' && key[len(key)-1] == '"' {
		literal := key[1 : len(key)-1]
		if existing, ok := parent.Get(literal); ok && !sameShape(existing, value) {
			return errKeyConflict(lineNo, literal)
		}
		parent.Set(literal, value)
		return nil
	}

	parts := strings.Split(key, ".")
	cur := parent
	for _, part := range parts[:len(parts)-1] {
		existing, ok := cur.Get(part)
		if ok {
			if existing.Kind != HkMapKind {
				return errKeyConflict(lineNo, part)
			}
			cur = existing.MapVal
			continue
		}
		newMap := NewHkMap()
		cur.Set(part, HkValue{Kind: HkMapKind, MapVal: newMap})
		cur = newMap
	}
	last := parts[len(parts)-1]
	if existing, ok := cur.Get(last); ok && !sameShape(existing, value) {
		return errKeyConflict(lineNo, last)
	}
	cur.Set(last, value)
	return nil
}

func sameShape(a, b HkValue) bool {
	// Only used to allow re-opening the same inline submap twice (rare but
	// harmless); anything else with a duplicate key is a real conflict.
	return a.Kind == HkMapKind && b.Kind == HkMapKind
}

// parseHkScalarOrArray parses a single value: array, quoted string, bool,
// number, or bare string, in that priority order.
func parseHkScalarOrArray(raw string, lineNo int) (HkValue, error) {
	if strings.HasPrefix(raw, "[") {
		if !strings.HasSuffix(raw, "]") {
			return HkValue{}, &HkError{Line: lineNo, Message: "array is missing closing ']'"}
		}
		inner := raw[1 : len(raw)-1]
		elems, err := splitTopLevelCommas(inner)
		if err != nil {
			return HkValue{}, &HkError{Line: lineNo, Message: err.Error()}
		}
		var arr []HkValue
		for _, e := range elems {
			e = strings.TrimSpace(e)
			if e == "" {
				continue
			}
			v, err := parseHkScalarOrArray(e, lineNo)
			if err != nil {
				return HkValue{}, err
			}
			arr = append(arr, v)
		}
		return HkValue{Kind: HkArray, Arr: arr}, nil
	}

	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		return HkValue{Kind: HkString, Str: unescapeHkString(raw[1 : len(raw)-1])}, nil
	}

	lower := strings.ToLower(raw)
	if lower == "true" {
		return HkValue{Kind: HkBool, Bool: true}, nil
	}
	if lower == "false" {
		return HkValue{Kind: HkBool, Bool: false}, nil
	}

	if n, err := strconv.ParseFloat(raw, 64); err == nil {
		return HkValue{Kind: HkNumber, Num: n}, nil
	}

	return HkValue{Kind: HkString, Str: raw}, nil
}

func unescapeHkString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case 't':
				b.WriteByte('\t')
				i++
				continue
			case 'r':
				b.WriteByte('\r')
				i++
				continue
			case '"':
				b.WriteByte('"')
				i++
				continue
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// splitTopLevelCommas splits array contents on commas that are not nested
// inside brackets or quotes.
func splitTopLevelCommas(s string) ([]string, error) {
	var parts []string
	depth := 0
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' && (i == 0 || s[i-1] != '\\'):
			inQuote = !inQuote
		case inQuote:
			// skip
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("unbalanced ']' in array")
			}
		case c == ',' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated string in array")
	}
	parts = append(parts, s[start:])
	return parts, nil
}

// ---------------------------------------------------------------------------
// Interpolation: ${section.key}, ${section.map.key}, ${section.arr[n]}, ${env:VAR}
// ---------------------------------------------------------------------------

var interpRe = regexp.MustCompile(`\$\{([^}]+)\}`)

// ResolveInterpolations expands every ${...} reference found in string
// values throughout doc, in place. Detects cycles and invalid references.
func ResolveInterpolations(doc *HkDocument) error {
	for _, secName := range doc.Sections.Keys() {
		sv, _ := doc.Sections.Get(secName)
		if sv.Kind != HkMapKind {
			continue
		}
		if err := resolveMap(doc, sv.MapVal, map[string]bool{}); err != nil {
			return err
		}
	}
	return nil
}

func resolveMap(doc *HkDocument, m *HkMap, visiting map[string]bool) error {
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		switch v.Kind {
		case HkString:
			resolved, err := resolveString(doc, v.Str, visiting)
			if err != nil {
				return err
			}
			v.Str = resolved
			m.Set(k, v)
		case HkArray:
			for i := range v.Arr {
				if v.Arr[i].Kind == HkString {
					resolved, err := resolveString(doc, v.Arr[i].Str, visiting)
					if err != nil {
						return err
					}
					v.Arr[i].Str = resolved
				}
			}
			m.Set(k, v)
		case HkMapKind:
			if err := resolveMap(doc, v.MapVal, visiting); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveString(doc *HkDocument, s string, visiting map[string]bool) (string, error) {
	// Loop so a value that resolves to another ${...} gets fully expanded.
	for i := 0; i < 32; i++ {
		matches := interpRe.FindAllStringSubmatchIndex(s, -1)
		if matches == nil {
			return s, nil
		}
		var b strings.Builder
		last := 0
		for _, mm := range matches {
			b.WriteString(s[last:mm[0]])
			ref := s[mm[2]:mm[3]]
			resolved, err := resolveRef(doc, ref, visiting)
			if err != nil {
				return "", err
			}
			b.WriteString(resolved)
			last = mm[1]
		}
		b.WriteString(s[last:])
		next := b.String()
		if next == s {
			return next, nil
		}
		s = next
	}
	return "", &HkError{Message: "interpolation did not converge after 32 passes (possible cycle)"}
}

func resolveRef(doc *HkDocument, ref string, visiting map[string]bool) (string, error) {
	if strings.HasPrefix(ref, "env:") {
		return os.Getenv(strings.TrimPrefix(ref, "env:")), nil
	}

	if visiting[ref] {
		return "", &HkError{Message: fmt.Sprintf("CyclicReference(%s)", ref)}
	}
	visiting[ref] = true
	defer delete(visiting, ref)

	val, err := lookupPath(doc, ref)
	if err != nil {
		return "", err
	}
	switch val.Kind {
	case HkString:
		return resolveString(doc, val.Str, visiting)
	case HkNumber:
		return formatHkNumber(val.Num), nil
	case HkBool:
		return strconv.FormatBool(val.Bool), nil
	default:
		return "", &HkError{Message: fmt.Sprintf("InvalidReference(%s): cannot interpolate a map or array", ref)}
	}
}

// lookupPath resolves "section.key.sub[0].leaf" against doc.
func lookupPath(doc *HkDocument, path string) (HkValue, error) {
	segs := strings.Split(path, ".")
	if len(segs) < 2 {
		return HkValue{}, &HkError{Message: fmt.Sprintf("InvalidReference(%s): need at least section.key", path)}
	}
	sec, ok := doc.Sections.Get(segs[0])
	if !ok || sec.Kind != HkMapKind {
		return HkValue{}, &HkError{Message: fmt.Sprintf("InvalidReference(%s): unknown section '%s'", path, segs[0])}
	}
	cur := sec
	for _, seg := range segs[1:] {
		key, idx, hasIdx := splitIndex(seg)
		if cur.Kind != HkMapKind {
			return HkValue{}, &HkError{Message: fmt.Sprintf("InvalidReference(%s): '%s' is not a map", path, key)}
		}
		next, ok := cur.MapVal.Get(key)
		if !ok {
			return HkValue{}, &HkError{Message: fmt.Sprintf("InvalidReference(%s): key '%s' not found", path, key)}
		}
		if hasIdx {
			if next.Kind != HkArray || idx < 0 || idx >= len(next.Arr) {
				return HkValue{}, &HkError{Message: fmt.Sprintf("InvalidReference(%s): index [%d] out of range", path, idx)}
			}
			next = next.Arr[idx]
		}
		cur = next
	}
	return cur, nil
}

var indexRe = regexp.MustCompile(`^(.+)\[(\d+)\]$`)

func splitIndex(seg string) (string, int, bool) {
	if m := indexRe.FindStringSubmatch(seg); m != nil {
		n, _ := strconv.Atoi(m[2])
		return m[1], n, true
	}
	return seg, 0, false
}

// ---------------------------------------------------------------------------
// Serialization
// ---------------------------------------------------------------------------

// SerializeHK writes doc back out in .hk syntax, preserving key order.
func SerializeHK(doc *HkDocument) string {
	var b strings.Builder
	sections := doc.Sections.Keys()
	for si, secName := range sections {
		if si > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[%s]\n", secName)
		sv, _ := doc.Sections.Get(secName)
		writeMapBody(&b, sv.MapVal, 1)
	}
	return b.String()
}

func writeMapBody(b *strings.Builder, m *HkMap, depth int) {
	prefix := strings.Repeat("-", depth) + ">"
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		key := serializeKey(k)
		if v.Kind == HkMapKind {
			fmt.Fprintf(b, "%s %s\n", prefix, key)
			writeMapBody(b, v.MapVal, depth+1)
			continue
		}
		fmt.Fprintf(b, "%s %s => %s\n", prefix, key, serializeValue(v))
	}
}

func serializeKey(k string) string {
	if strings.Contains(k, ".") {
		return `"` + k + `"`
	}
	return k
}

func serializeValue(v HkValue) string {
	switch v.Kind {
	case HkString:
		if needsQuoting(v.Str) {
			return `"` + escapeHkString(v.Str) + `"`
		}
		return v.Str
	case HkNumber:
		return formatHkNumber(v.Num)
	case HkBool:
		return strconv.FormatBool(v.Bool)
	case HkArray:
		parts := make([]string, len(v.Arr))
		for i, e := range v.Arr {
			parts[i] = serializeValue(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return ""
	}
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	if strings.ContainsAny(s, "\n\t\"\\") {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "true" || lower == "false" {
		return true
	}
	if _, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		return true
	}
	return false
}

func escapeHkString(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`, "\r", `\r`)
	return replacer.Replace(s)
}

func formatHkNumber(n float64) string {
	if n == float64(int64(n)) {
		return strconv.FormatInt(int64(n), 10)
	}
	return strconv.FormatFloat(n, 'g', -1, 64)
}

// WriteHKFile serializes doc and atomically writes it to path.
func WriteHKFile(path string, doc *HkDocument) error {
	data := SerializeHK(doc)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---------------------------------------------------------------------------
// HkValue convenience accessors — Go equivalents of the spec's .as_string()
// / .as_number() / .as_bool() / .as_array() / .as_map() methods.
// ---------------------------------------------------------------------------

func (v HkValue) AsString() (string, error) {
	switch v.Kind {
	case HkString:
		return v.Str, nil
	case HkNumber:
		return formatHkNumber(v.Num), nil
	case HkBool:
		return strconv.FormatBool(v.Bool), nil
	default:
		return "", &HkError{Message: "TypeMismatch: expected string-like, found array/map"}
	}
}

func (v HkValue) AsNumber() (float64, error) {
	if v.Kind != HkNumber {
		return 0, &HkError{Message: "TypeMismatch: expected number"}
	}
	return v.Num, nil
}

func (v HkValue) AsBool() (bool, error) {
	if v.Kind != HkBool {
		return false, &HkError{Message: "TypeMismatch: expected bool"}
	}
	return v.Bool, nil
}

func (v HkValue) AsArray() ([]HkValue, error) {
	if v.Kind != HkArray {
		return nil, &HkError{Message: "TypeMismatch: expected array"}
	}
	return v.Arr, nil
}

func (v HkValue) AsMap() (*HkMap, error) {
	if v.Kind != HkMapKind {
		return nil, &HkError{Message: "TypeMismatch: expected map"}
	}
	return v.MapVal, nil
}

// --- small typed helpers used by config.go / helpers_state.go / snapshot.go

func hkGetString(m *HkMap, key, def string) string {
	if v, ok := m.Get(key); ok {
		if s, err := v.AsString(); err == nil {
			return s
		}
	}
	return def
}

func hkGetBool(m *HkMap, key string, def bool) bool {
	if v, ok := m.Get(key); ok {
		if b, err := v.AsBool(); err == nil {
			return b
		}
	}
	return def
}

func hkStr(s string) HkValue  { return HkValue{Kind: HkString, Str: s} }
func hkBoolV(b bool) HkValue  { return HkValue{Kind: HkBool, Bool: b} }
func hkNum(n float64) HkValue { return HkValue{Kind: HkNumber, Num: n} }

// sortedKeysStable is a tiny helper kept here so callers that build
// deterministic output (e.g. tests) don't each need their own sort import.
func sortedKeysStable(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
