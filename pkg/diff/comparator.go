package diff

import (
	"encoding/base64"
	"fmt"
	"math"
	"os"
	"reflect"
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// CompareDocuments compares two BSON documents and returns field-level diffs.
// Both documents must be non-nil. Key order is ignored (Rule 3).
func CompareDocuments(source, target bson.M) []FieldDiff {
	return compareDocuments(source, target, "")
}

// CompareDocumentsFiltered compares documents and filters out ignored field paths.
// ignoreFields should contain dot-notation paths (e.g. "__v", "meta.modified").
// A field is ignored if its path matches exactly or starts with an ignored prefix
// (e.g. ignoring "meta" also ignores "meta.created", "meta.modified").
func CompareDocumentsFiltered(source, target bson.M, ignoreFields []string) []FieldDiff {
	if len(ignoreFields) == 0 {
		return CompareDocuments(source, target)
	}
	diffs := CompareDocuments(source, target)
	filtered := make([]FieldDiff, 0, len(diffs))
	for _, d := range diffs {
		if !isIgnoredField(d.Path, ignoreFields) {
			filtered = append(filtered, d)
		}
	}
	return filtered
}

func isIgnoredField(path string, ignoreFields []string) bool {
	for _, ig := range ignoreFields {
		if path == ig || strings.HasPrefix(path, ig+".") || strings.HasPrefix(path, ig+"[") {
			return true
		}
	}
	return false
}

func compareDocuments(source, target bson.M, prefix string) []FieldDiff {
	var diffs []FieldDiff

	// Collect all keys from both documents, sorted for deterministic output
	keys := allKeys(source, target)

	for _, key := range keys {
		path := joinPath(prefix, key)
		sourceVal, sourceExists := source[key]
		targetVal, targetExists := target[key]

		switch {
		case sourceExists && !targetExists:
			// Field added in source (absent in target)
			diffs = append(diffs, FieldDiff{
				Path:     path,
				DiffType: Added,
				NewValue: sourceVal,
			})
		case !sourceExists && targetExists:
			// Field removed from source (present in target only)
			diffs = append(diffs, FieldDiff{
				Path:     path,
				DiffType: Removed,
				OldValue: targetVal,
			})
		default:
			// Both exist — compare values
			fieldDiffs := compareValues(sourceVal, targetVal, path)
			diffs = append(diffs, fieldDiffs...)
		}
	}

	return diffs
}

// compareValues compares two BSON values at a given path.
// Rule 1: Type check first. If types differ, it's a modification with type change.
func compareValues(source, target interface{}, path string) []FieldDiff {
	// Normalize bson.D to bson.M (both represent documents, D is ordered, M is unordered)
	source = normalizeValue(source)
	target = normalizeValue(target)

	// Rule 2: Handle null explicitly
	sourceIsNil := isNilValue(source)
	targetIsNil := isNilValue(target)

	if sourceIsNil && targetIsNil {
		return nil // both null, identical
	}
	if sourceIsNil != targetIsNil {
		return []FieldDiff{{
			Path:     path,
			DiffType: Modified,
			OldValue: target,
			NewValue: source,
		}}
	}

	// Rule 1: Type check first
	sourceType := reflect.TypeOf(source)
	targetType := reflect.TypeOf(target)
	if sourceType != targetType {
		return []FieldDiff{{
			Path:     path,
			DiffType: Modified,
			OldValue: target,
			NewValue: source,
		}}
	}

	// Same type — compare by specific BSON type
	switch sv := source.(type) {
	// Rule 3: Nested documents — recursive, key-order-independent
	case bson.M:
		tv := target.(bson.M)
		return compareDocuments(sv, tv, path)

	// Rule 4: Arrays — positional, index-by-index
	case bson.A:
		tv := target.(bson.A)
		return compareArrays(sv, tv, path)

	// Rule 6: ObjectId — 12-byte equality
	case bson.ObjectID:
		tv := target.(bson.ObjectID)
		if sv != tv {
			return []FieldDiff{{Path: path, DiffType: Modified, OldValue: target, NewValue: source}}
		}
		return nil

	// Rule 5: Dates — millisecond UTC equality
	case bson.DateTime:
		tv := target.(bson.DateTime)
		if sv != tv {
			return []FieldDiff{{Path: path, DiffType: Modified, OldValue: target, NewValue: source}}
		}
		return nil

	// Rule 7: Strings — exact byte equality
	case string:
		tv := target.(string)
		if sv != tv {
			return []FieldDiff{{Path: path, DiffType: Modified, OldValue: target, NewValue: source}}
		}
		return nil

	// Rule 8: Numbers — strict type comparison (int32)
	case int32:
		tv := target.(int32)
		if sv != tv {
			return []FieldDiff{{Path: path, DiffType: Modified, OldValue: target, NewValue: source}}
		}
		return nil

	// Rule 8: Numbers — strict type comparison (int64)
	case int64:
		tv := target.(int64)
		if sv != tv {
			return []FieldDiff{{Path: path, DiffType: Modified, OldValue: target, NewValue: source}}
		}
		return nil

	// Rule 8: Numbers — strict type comparison (double/float64)
	case float64:
		tv := target.(float64)
		// NaN == NaN is false in IEEE 754, but both being NaN means identical
		if sv != tv && !(math.IsNaN(sv) && math.IsNaN(tv)) {
			return []FieldDiff{{Path: path, DiffType: Modified, OldValue: target, NewValue: source}}
		}
		return nil

	// Rule 9: Booleans
	case bool:
		tv := target.(bool)
		if sv != tv {
			return []FieldDiff{{Path: path, DiffType: Modified, OldValue: target, NewValue: source}}
		}
		return nil

	// Rule 10: Decimal128 — string representation equality
	case bson.Decimal128:
		tv := target.(bson.Decimal128)
		if sv.String() != tv.String() {
			return []FieldDiff{{Path: path, DiffType: Modified, OldValue: target, NewValue: source}}
		}
		return nil

	// Rule 10: Binary — byte-for-byte comparison
	case bson.Binary:
		tv := target.(bson.Binary)
		if sv.Subtype != tv.Subtype || !bytesEqual(sv.Data, tv.Data) {
			return []FieldDiff{{Path: path, DiffType: Modified, OldValue: target, NewValue: source}}
		}
		return nil

	// Rule 10: Regex — pattern AND flags must both match
	case bson.Regex:
		tv := target.(bson.Regex)
		if sv.Pattern != tv.Pattern || sv.Options != tv.Options {
			return []FieldDiff{{Path: path, DiffType: Modified, OldValue: target, NewValue: source}}
		}
		return nil

	// Rule 10: Undefined — type equality only
	case bson.Undefined:
		return nil

	// Rule 10: MinKey — type equality only
	case bson.MinKey:
		return nil

	// Rule 10: MaxKey — type equality only
	case bson.MaxKey:
		return nil

	default:
		// Rule 11: Unknown type fallback with reflect.DeepEqual + warning
		fmt.Fprintf(os.Stderr, "Warning: Unknown BSON type encountered at path %q: reflect.Type=%v. Using reflect.DeepEqual fallback.\n", path, sourceType)
		if !reflect.DeepEqual(source, target) {
			return []FieldDiff{{Path: path, DiffType: Modified, OldValue: target, NewValue: source}}
		}
		return nil
	}
}

// compareArrays compares two BSON arrays positionally (Rule 4).
func compareArrays(source, target bson.A, path string) []FieldDiff {
	var diffs []FieldDiff

	minLen := len(source)
	if len(target) < minLen {
		minLen = len(target)
	}

	// Compare common indices
	for i := 0; i < minLen; i++ {
		elemPath := fmt.Sprintf("%s[%d]", path, i)
		elemDiffs := compareValues(source[i], target[i], elemPath)
		diffs = append(diffs, elemDiffs...)
	}

	// Extra elements in source (added)
	for i := minLen; i < len(source); i++ {
		elemPath := fmt.Sprintf("%s[%d]", path, i)
		diffs = append(diffs, FieldDiff{
			Path:     elemPath,
			DiffType: Added,
			NewValue: source[i],
		})
	}

	// Extra elements in target (removed)
	for i := minLen; i < len(target); i++ {
		elemPath := fmt.Sprintf("%s[%d]", path, i)
		diffs = append(diffs, FieldDiff{
			Path:     elemPath,
			DiffType: Removed,
			OldValue: target[i],
		})
	}

	return diffs
}

// allKeys returns a sorted, deduplicated list of keys from both maps.
func allKeys(a, b bson.M) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func isNilValue(v interface{}) bool {
	return v == nil
}

// normalizeValue converts bson.D (ordered document) to bson.M (unordered map)
// and recursively normalizes bson.A elements. This ensures consistent comparison
// regardless of whether the driver returns D or M for nested documents.
func normalizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case bson.D:
		m := make(bson.M, len(val))
		for _, elem := range val {
			m[elem.Key] = normalizeValue(elem.Value)
		}
		return m
	case bson.A:
		normalized := make(bson.A, len(val))
		for i, elem := range val {
			normalized[i] = normalizeValue(elem)
		}
		return normalized
	default:
		return v
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// FormatValue formats a BSON value for display in diff output.
func FormatValue(v interface{}) string {
	if v == nil {
		return "null"
	}

	switch val := v.(type) {
	case bson.ObjectID:
		return fmt.Sprintf("ObjectId(%q)", val.Hex())
	case bson.DateTime:
		return val.Time().UTC().Format("2006-01-02T15:04:05.000Z")
	case string:
		return fmt.Sprintf("%q", val)
	case int32:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case float64:
		return formatFloat(val)
	case bool:
		return fmt.Sprintf("%t", val)
	case bson.Decimal128:
		return fmt.Sprintf("Decimal128(%q)", val.String())
	case bson.Binary:
		encoded := base64.StdEncoding.EncodeToString(val.Data)
		if len(encoded) > 32 {
			encoded = encoded[:32] + "..."
		}
		return fmt.Sprintf("Binary(%s)", encoded)
	case bson.Regex:
		return fmt.Sprintf("/%s/%s", val.Pattern, val.Options)
	case bson.Undefined:
		return "undefined"
	case bson.MinKey:
		return "MinKey"
	case bson.MaxKey:
		return "MaxKey"
	case bson.M:
		return formatDocument(val)
	case bson.D:
		return formatDocument(dToM(val))
	case bson.A:
		return formatArray(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// BSONTypeName returns the BSON type name for display in type mismatch messages.
func BSONTypeName(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case bson.ObjectID:
		return "ObjectId"
	case bson.DateTime:
		return "DateTime"
	case string:
		return "string"
	case int32:
		return "int32"
	case int64:
		return "int64"
	case float64:
		return "double"
	case bool:
		return "bool"
	case bson.Decimal128:
		return "Decimal128"
	case bson.Binary:
		return "Binary"
	case bson.Regex:
		return "Regex"
	case bson.Undefined:
		return "undefined"
	case bson.MinKey:
		return "MinKey"
	case bson.MaxKey:
		return "MaxKey"
	case bson.M:
		return "document"
	case bson.D:
		return "document"
	case bson.A:
		return "array"
	default:
		return reflect.TypeOf(v).String()
	}
}

func dToM(d bson.D) bson.M {
	m := make(bson.M, len(d))
	for _, elem := range d {
		m[elem.Key] = elem.Value
	}
	return m
}

func formatFloat(f float64) string {
	s := fmt.Sprintf("%g", f)
	// Ensure there's a decimal point for clarity
	for _, c := range s {
		if c == '.' || c == 'e' || c == 'E' {
			return s
		}
	}
	return s + ".0"
}

func formatDocument(m bson.M) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := "{ "
	for i, k := range keys {
		if i > 0 {
			result += ", "
		}
		result += fmt.Sprintf("%s: %s", k, FormatValue(m[k]))
	}
	result += " }"
	return result
}

func formatArray(a bson.A) string {
	result := "["
	for i, v := range a {
		if i > 0 {
			result += ", "
		}
		result += FormatValue(v)
	}
	result += "]"
	return result
}
