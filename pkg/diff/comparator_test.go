package diff

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestIdenticalDocuments(t *testing.T) {
	doc := bson.M{
		"name":  "Alice",
		"age":   int32(30),
		"email": "alice@example.com",
	}
	diffs := CompareDocuments(doc, doc)
	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical documents, got %d", len(diffs))
	}
}

func TestIdenticalDocumentsKeyOrder(t *testing.T) {
	// Rule 3: key order is ignored
	source := bson.M{"a": int32(1), "b": int32(2), "c": int32(3)}
	target := bson.M{"c": int32(3), "a": int32(1), "b": int32(2)}
	diffs := CompareDocuments(source, target)
	if len(diffs) != 0 {
		t.Errorf("expected no diffs for key-order-independent comparison, got %d", len(diffs))
	}
}

func TestAddedField(t *testing.T) {
	source := bson.M{"name": "Alice", "role": "admin"}
	target := bson.M{"name": "Alice"}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].Path != "role" || diffs[0].DiffType != Added {
		t.Errorf("expected added field 'role', got %+v", diffs[0])
	}
	if diffs[0].NewValue != "admin" {
		t.Errorf("expected new value 'admin', got %v", diffs[0].NewValue)
	}
}

func TestRemovedField(t *testing.T) {
	source := bson.M{"name": "Alice"}
	target := bson.M{"name": "Alice", "role": "admin"}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].Path != "role" || diffs[0].DiffType != Removed {
		t.Errorf("expected removed field 'role', got %+v", diffs[0])
	}
}

func TestModifiedFieldSameType(t *testing.T) {
	source := bson.M{"status": "active"}
	target := bson.M{"status": "suspended"}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].DiffType != Modified {
		t.Errorf("expected modified, got %s", diffs[0].DiffType)
	}
	if diffs[0].OldValue != "suspended" || diffs[0].NewValue != "active" {
		t.Errorf("expected old='suspended', new='active', got old=%v, new=%v",
			diffs[0].OldValue, diffs[0].NewValue)
	}
}

// Rule 8: Strict type comparison
func TestTypeMismatchInt32VsInt64(t *testing.T) {
	source := bson.M{"count": int32(3)}
	target := bson.M{"count": int64(3)}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for int32 vs int64, got %d", len(diffs))
	}
	if diffs[0].DiffType != Modified {
		t.Errorf("expected modified for type mismatch, got %s", diffs[0].DiffType)
	}
}

func TestTypeMismatchInt32VsDouble(t *testing.T) {
	source := bson.M{"count": int32(3)}
	target := bson.M{"count": float64(3.0)}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for int32 vs double, got %d", len(diffs))
	}
}

func TestTypeMismatchInt64VsInt32(t *testing.T) {
	source := bson.M{"count": int64(0)}
	target := bson.M{"count": int32(0)}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for int64 vs int32, got %d", len(diffs))
	}
}

// Rule 2: Null vs absent vs empty
func TestNullVsAbsent(t *testing.T) {
	source := bson.M{"name": "Alice"}
	target := bson.M{"name": "Alice", "lastName": nil}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].Path != "lastName" || diffs[0].DiffType != Removed {
		t.Errorf("expected removed field 'lastName', got %+v", diffs[0])
	}
}

func TestAbsentVsNull(t *testing.T) {
	source := bson.M{"name": "Alice", "lastName": nil}
	target := bson.M{"name": "Alice"}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].Path != "lastName" || diffs[0].DiffType != Added {
		t.Errorf("expected added field 'lastName', got %+v", diffs[0])
	}
}

func TestNullVsEmptyArray(t *testing.T) {
	source := bson.M{"hobbies": bson.A{}}
	target := bson.M{"hobbies": nil}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].DiffType != Modified {
		t.Errorf("expected modified, got %s", diffs[0].DiffType)
	}
}

func TestBothNull(t *testing.T) {
	source := bson.M{"lastName": nil}
	target := bson.M{"lastName": nil}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 0 {
		t.Errorf("expected no diffs for both null, got %d", len(diffs))
	}
}

func TestAbsentVsEmptyString(t *testing.T) {
	source := bson.M{"name": ""}
	target := bson.M{}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].DiffType != Added {
		t.Errorf("expected added, got %s", diffs[0].DiffType)
	}
}

// Rule 3: Nested documents
func TestNestedDocumentDiff(t *testing.T) {
	source := bson.M{
		"address": bson.M{
			"city":  "New York",
			"state": "NY",
			"zip":   "10001",
		},
	}
	target := bson.M{
		"address": bson.M{
			"city":  "Boston",
			"state": "MA",
			"zip":   "10001",
		},
	}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d: %+v", len(diffs), diffs)
	}

	// Diffs should be sorted alphabetically
	if diffs[0].Path != "address.city" {
		t.Errorf("expected path 'address.city', got %s", diffs[0].Path)
	}
	if diffs[1].Path != "address.state" {
		t.Errorf("expected path 'address.state', got %s", diffs[1].Path)
	}
}

func TestDeeplyNestedDocuments(t *testing.T) {
	source := bson.M{
		"a": bson.M{
			"b": bson.M{
				"c": bson.M{
					"d": "deep",
				},
			},
		},
	}
	target := bson.M{
		"a": bson.M{
			"b": bson.M{
				"c": bson.M{
					"d": "shallow",
				},
			},
		},
	}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].Path != "a.b.c.d" {
		t.Errorf("expected path 'a.b.c.d', got %s", diffs[0].Path)
	}
}

// Rule 4: Arrays
func TestArrayIdentical(t *testing.T) {
	source := bson.M{"tags": bson.A{"a", "b", "c"}}
	target := bson.M{"tags": bson.A{"a", "b", "c"}}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical arrays, got %d", len(diffs))
	}
}

func TestArrayElementModified(t *testing.T) {
	// ["b", "a"] vs ["a", "b"] — positional comparison
	source := bson.M{"tags": bson.A{"b", "a"}}
	target := bson.M{"tags": bson.A{"a", "b"}}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs for swapped array, got %d", len(diffs))
	}
}

func TestArrayElementAdded(t *testing.T) {
	source := bson.M{"tags": bson.A{"a", "b", "c"}}
	target := bson.M{"tags": bson.A{"a", "b"}}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].Path != "tags[2]" || diffs[0].DiffType != Added {
		t.Errorf("expected added at tags[2], got %+v", diffs[0])
	}
}

func TestArrayElementRemoved(t *testing.T) {
	source := bson.M{"tags": bson.A{"a"}}
	target := bson.M{"tags": bson.A{"a", "b", "c"}}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d", len(diffs))
	}
	if diffs[0].Path != "tags[1]" || diffs[0].DiffType != Removed {
		t.Errorf("expected removed at tags[1], got %+v", diffs[0])
	}
	if diffs[1].Path != "tags[2]" || diffs[1].DiffType != Removed {
		t.Errorf("expected removed at tags[2], got %+v", diffs[1])
	}
}

func TestArrayOfNestedDocuments(t *testing.T) {
	source := bson.M{
		"items": bson.A{
			bson.M{"name": "Widget", "price": int32(10)},
		},
	}
	target := bson.M{
		"items": bson.A{
			bson.M{"name": "Widget", "price": int32(15)},
		},
	}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].Path != "items[0].price" {
		t.Errorf("expected path 'items[0].price', got %s", diffs[0].Path)
	}
}

// Rule 5: Dates
func TestDateTimeEqual(t *testing.T) {
	ts := bson.DateTime(1709719661734) // some UTC timestamp
	source := bson.M{"createdAt": ts}
	target := bson.M{"createdAt": ts}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical dates, got %d", len(diffs))
	}
}

func TestDateTimeDifferent(t *testing.T) {
	source := bson.M{"createdAt": bson.DateTime(1709719661734)}
	target := bson.M{"createdAt": bson.DateTime(1709719661735)}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
}

// Rule 6: ObjectId
func TestObjectIdEqual(t *testing.T) {
	id := bson.ObjectID{0x66, 0x5a, 0x1b, 0x2c, 0x3d, 0x4e, 0x5f, 0x6a, 0x7b, 0x8c, 0x9d, 0x0e}
	source := bson.M{"ref": id}
	target := bson.M{"ref": id}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical ObjectIds, got %d", len(diffs))
	}
}

func TestObjectIdDifferent(t *testing.T) {
	id1 := bson.ObjectID{0x66, 0x5a, 0x1b, 0x2c, 0x3d, 0x4e, 0x5f, 0x6a, 0x7b, 0x8c, 0x9d, 0x0e}
	id2 := bson.ObjectID{0x66, 0x5a, 0x1b, 0x2c, 0x3d, 0x4e, 0x5f, 0x6a, 0x7b, 0x8c, 0x9d, 0x0f}
	source := bson.M{"ref": id1}
	target := bson.M{"ref": id2}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
}

// Rule 7: Strings
func TestStringCaseSensitive(t *testing.T) {
	source := bson.M{"name": "demo"}
	target := bson.M{"name": "Demo"}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for case difference, got %d", len(diffs))
	}
}

func TestStringWhitespace(t *testing.T) {
	source := bson.M{"name": "demo "}
	target := bson.M{"name": "demo"}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for whitespace difference, got %d", len(diffs))
	}
}

// Rule 9: Booleans
func TestBooleansIdentical(t *testing.T) {
	source := bson.M{"active": true}
	target := bson.M{"active": true}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 0 {
		t.Errorf("expected no diffs, got %d", len(diffs))
	}
}

func TestBooleansDifferent(t *testing.T) {
	source := bson.M{"active": true}
	target := bson.M{"active": false}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
}

// Rule 10: Special types
func TestRegexEqual(t *testing.T) {
	source := bson.M{"pattern": bson.Regex{Pattern: "^test", Options: "i"}}
	target := bson.M{"pattern": bson.Regex{Pattern: "^test", Options: "i"}}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical regex, got %d", len(diffs))
	}
}

func TestRegexDifferentOptions(t *testing.T) {
	source := bson.M{"pattern": bson.Regex{Pattern: "^test", Options: "i"}}
	target := bson.M{"pattern": bson.Regex{Pattern: "^test", Options: ""}}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for regex options difference, got %d", len(diffs))
	}
}

func TestDecimal128Equal(t *testing.T) {
	d1, _ := bson.ParseDecimal128("123.456")
	d2, _ := bson.ParseDecimal128("123.456")
	source := bson.M{"price": d1}
	target := bson.M{"price": d2}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical Decimal128, got %d", len(diffs))
	}
}

func TestDecimal128Different(t *testing.T) {
	d1, _ := bson.ParseDecimal128("123.456")
	d2, _ := bson.ParseDecimal128("789.012")
	source := bson.M{"price": d1}
	target := bson.M{"price": d2}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
}

func TestBinaryEqual(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	source := bson.M{"data": bson.Binary{Subtype: 0x00, Data: data}}
	target := bson.M{"data": bson.Binary{Subtype: 0x00, Data: data}}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical binary, got %d", len(diffs))
	}
}

func TestBinaryDifferent(t *testing.T) {
	source := bson.M{"data": bson.Binary{Subtype: 0x00, Data: []byte{0x01}}}
	target := bson.M{"data": bson.Binary{Subtype: 0x00, Data: []byte{0x02}}}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
}

func TestMinKeyMaxKeyUndefined(t *testing.T) {
	source := bson.M{
		"min": bson.MinKey{},
		"max": bson.MaxKey{},
		"und": bson.Undefined{},
	}
	target := bson.M{
		"min": bson.MinKey{},
		"max": bson.MaxKey{},
		"und": bson.Undefined{},
	}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical special types, got %d", len(diffs))
	}
}

// Type mismatch between different BSON value types
func TestTypeMismatchStringVsInt(t *testing.T) {
	source := bson.M{"value": "3"}
	target := bson.M{"value": int32(3)}
	diffs := CompareDocuments(source, target)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for string vs int type mismatch, got %d", len(diffs))
	}
	if diffs[0].DiffType != Modified {
		t.Errorf("expected modified, got %s", diffs[0].DiffType)
	}
}

// Multiple fields changed
func TestMultipleFieldChanges(t *testing.T) {
	source := bson.M{
		"name":   "Alice",
		"role":   "admin",
		"active": true,
		"new":    "field",
	}
	target := bson.M{
		"name":    "Alice",
		"role":    "user",
		"active":  false,
		"removed": "field",
	}
	diffs := CompareDocuments(source, target)

	// active: modified, new: added, removed: removed, role: modified = 4 diffs
	if len(diffs) != 4 {
		t.Fatalf("expected 4 diffs, got %d: %+v", len(diffs), diffs)
	}
}

// FormatValue tests
func TestFormatValueObjectId(t *testing.T) {
	id := bson.ObjectID{0x66, 0x5a, 0x1b, 0x2c, 0x3d, 0x4e, 0x5f, 0x6a, 0x7b, 0x8c, 0x9d, 0x0e}
	result := FormatValue(id)
	expected := `ObjectId("665a1b2c3d4e5f6a7b8c9d0e")`
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestFormatValueNull(t *testing.T) {
	if FormatValue(nil) != "null" {
		t.Errorf("expected 'null', got %s", FormatValue(nil))
	}
}

func TestFormatValueFloat(t *testing.T) {
	if FormatValue(float64(3)) != "3.0" {
		t.Errorf("expected '3.0', got %s", FormatValue(float64(3)))
	}
	if FormatValue(float64(3.14)) != "3.14" {
		t.Errorf("expected '3.14', got %s", FormatValue(float64(3.14)))
	}
}

func TestBSONTypeName(t *testing.T) {
	tests := map[interface{}]string{
		int32(1):       "int32",
		int64(1):       "int64",
		float64(1):     "double",
		"hello":        "string",
		true:           "bool",
		nil:            "null",
		bson.MinKey{}:  "MinKey",
		bson.MaxKey{}:  "MaxKey",
	}
	for val, expected := range tests {
		got := BSONTypeName(val)
		if got != expected {
			t.Errorf("BSONTypeName(%v) = %s, want %s", val, got, expected)
		}
	}
}
