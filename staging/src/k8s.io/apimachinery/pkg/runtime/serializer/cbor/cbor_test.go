/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cbor

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/cbor/internal/modes"

	"github.com/google/go-cmp/cmp"
)

// anyObject wraps arbitrary concrete values to be encoded or decoded.
type anyObject struct {
	Value interface{}
}

func (p anyObject) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind
}

func (anyObject) DeepCopyObject() runtime.Object {
	panic("unimplemented")
}

func (p anyObject) MarshalCBOR() ([]byte, error) {
	return modes.Encode.Marshal(p.Value)
}

func (p *anyObject) UnmarshalCBOR(in []byte) error {
	return modes.Decode.Unmarshal(in, &p.Value)
}

// TestAppendixA roundtrips the examples of encoded CBOR data items in RFC 8949 Appendix A. For
// completeness, appendix entries that can't be processed are included with an explanation.
func TestAppendixA(t *testing.T) {
	hex := func(h string) []byte {
		b, err := hex.DecodeString(h)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	eq := conversion.EqualitiesOrDie(
		// NaN float64 values are always inequal and have multiple representations. RFC 8949
		// Section 4.2.2 recommends protocols not supporting NaN payloads or signaling NaNs
		// choose a single representation for all NaN values. For the purposes of this test,
		// all NaN representations are equivalent.
		func(a float64, b float64) bool {
			if math.IsNaN(a) && math.IsNaN(b) {
				return true
			}
			return math.Float64bits(a) == math.Float64bits(b)
		},
	)

	const (
		reasonArrayFixedLength  = "indefinite-length arrays are re-encoded with fixed length"
		reasonByteString        = "strings are encoded as the byte string major type"
		reasonFloatPacked       = "floats are packed into the smallest value-preserving width"
		reasonNaN               = "all NaN values are represented with a single encoding"
		reasonMapFixedLength    = "indefinite-length maps are re-encoded with fixed length"
		reasonMapSorted         = "map entries are sorted"
		reasonStringFixedLength = "indefinite-length strings are re-encoded with fixed length"
		reasonTagIgnored        = "unrecognized tag numbers are ignored"
		reasonTimeToInterface   = "times decode to interface{} as RFC3339 timestamps for JSON interoperability"
	)

	for _, tc := range []struct {
		example []byte // example data item
		decoded interface{}
		reject  string   // reason the decoder rejects the example
		encoded []byte   // re-encoded object if different from example encoding
		reasons []string // reasons for re-encode difference

		fixme string // what is required to fix this case
	}{
		{
			example: hex("00"),
			decoded: int64(0),
		},
		{
			example: hex("01"),
			decoded: int64(1),
		},
		{
			example: hex("0a"),
			decoded: int64(10),
		},
		{
			example: hex("17"),
			decoded: int64(23),
		},
		{
			example: hex("1818"),
			decoded: int64(24),
		},
		{
			example: hex("1819"),
			decoded: int64(25),
		},
		{
			example: hex("1864"),
			decoded: int64(100),
		},
		{
			example: hex("1903e8"),
			decoded: int64(1000),
		},
		{
			example: hex("1a000f4240"),
			decoded: int64(1000000),
		},
		{
			example: hex("1b000000e8d4a51000"),
			decoded: int64(1000000000000),
		},
		{
			example: hex("1bffffffffffffffff"),
			reject:  "2^64-1 overflows int64 and falling back to float64 (as with JSON) loses distinction between float and integer",
		},
		{
			example: hex("c249010000000000000000"),
			reject:  "decoding tagged positive bigint value to interface{} can't reproduce this value without losing distinction between float and integer",
			fixme:   "decoding bigint to interface{} must never produce math/big.Int",
		},
		{
			example: hex("3bffffffffffffffff"),
			reject:  "-2^64-1 overflows int64 and falling back to float64 (as with JSON) loses distinction between float and integer",
			fixme:   "decoding integers that overflow int64 must not produce math/big.Int",
		},
		{
			example: hex("c349010000000000000000"),
			reject:  "-18446744073709551617 overflows int64 and falling back to float64 (as with JSON) loses distinction between float and integer",
			fixme:   "decoding negative bigint to interface{} must never produce math/big.Int",
		},
		{
			example: hex("20"),
			decoded: int64(-1),
		},
		{
			example: hex("29"),
			decoded: int64(-10),
		},
		{
			example: hex("3863"),
			decoded: int64(-100),
		},
		{
			example: hex("3903e7"),
			decoded: int64(-1000),
		},
		{
			example: hex("f90000"),
			decoded: 0.0,
		},
		{
			example: hex("f98000"),
			decoded: math.Copysign(0, -1),
		},
		{
			example: hex("f93c00"),
			decoded: 1.0,
		},
		{
			example: hex("fb3ff199999999999a"),
			decoded: 1.1,
		},
		{
			example: hex("f93e00"),
			decoded: 1.5,
		},
		{
			example: hex("f97bff"),
			decoded: 65504.0,
		},
		{
			example: hex("fa47c35000"),
			decoded: 100000.0,
		},
		{
			example: hex("fa7f7fffff"),
			decoded: 3.4028234663852886e+38,
		},
		{
			example: hex("fb7e37e43c8800759c"),
			decoded: 1.0e+300,
		},
		{
			example: hex("f90001"),
			decoded: 5.960464477539063e-8,
		},
		{
			example: hex("f90400"),
			decoded: 0.00006103515625,
		},
		{
			example: hex("f9c400"),
			decoded: -4.0,
		},
		{
			example: hex("fbc010666666666666"),
			decoded: -4.1,
		},
		// TODO: Should Inf/-Inf/NaN be supported? Current Protobuf will encode this, but
		// JSON will produce an error.  This is less than ideal -- we can't transcode
		// everything to JSON.
		{
			example: hex("f97c00"),
			decoded: math.Inf(1),
		},
		{
			example: hex("f97e00"),
			decoded: math.Float64frombits(0x7ff8000000000000),
		},
		{
			example: hex("f9fc00"),
			decoded: math.Inf(-1),
		},
		{
			example: hex("fa7f800000"),
			decoded: math.Inf(1),
			encoded: hex("f97c00"),
			reasons: []string{
				reasonFloatPacked,
			},
		},
		{
			example: hex("fa7fc00000"),
			decoded: math.NaN(),
			encoded: hex("f97e00"),
			reasons: []string{
				reasonNaN,
			},
		},
		{
			example: hex("faff800000"),
			decoded: math.Inf(-1),
			encoded: hex("f9fc00"),
			reasons: []string{
				reasonFloatPacked,
			},
		},
		{
			example: hex("fb7ff0000000000000"),
			decoded: math.Inf(1),
			encoded: hex("f97c00"),
			reasons: []string{
				reasonFloatPacked,
			},
		},
		{
			example: hex("fb7ff8000000000000"),
			decoded: math.NaN(),
			encoded: hex("f97e00"),
			reasons: []string{
				reasonNaN,
			},
		},
		{
			example: hex("fbfff0000000000000"),
			decoded: math.Inf(-1),
			encoded: hex("f9fc00"),
			reasons: []string{
				reasonFloatPacked,
			},
		},
		{
			example: hex("f4"),
			decoded: false,
		},
		{
			example: hex("f5"),
			decoded: true,
		},
		{
			example: hex("f6"),
			decoded: nil,
		},
		{
			example: hex("f7"),
			reject:  "only simple values false, true, and null have a clear analog",
			fixme:   "the undefined simple value should not successfully decode as nil",
		},
		{
			example: hex("f0"),
			reject:  "only simple values false, true, and null have a clear analog",
			fixme:   "simple values other than false, true, and null should be rejected",
		},
		{
			example: hex("f8ff"),
			reject:  "only simple values false, true, and null have a clear analog",
			fixme:   "simple values other than false, true, and null should be rejected",
		},
		{
			example: hex("c074323031332d30332d32315432303a30343a30305a"),
			decoded: "2013-03-21T20:04:00Z",
			encoded: hex("54323031332d30332d32315432303a30343a30305a"),
			reasons: []string{
				reasonByteString,
				reasonTimeToInterface,
			},
			fixme: "decoding of tagged time into interface{} must produce RFC3339 timestamp compatible with JSON, not time.Time",
		},
		{
			example: hex("c11a514b67b0"),
			decoded: "2013-03-21T16:04:00Z",
			encoded: hex("54323031332d30332d32315431363a30343a30305a"),
			reasons: []string{
				reasonByteString,
				reasonTimeToInterface,
			},
			fixme: "decoding of tagged time into interface{} must produce RFC3339 timestamp compatible with JSON, not time.Time",
		},
		{
			example: hex("c1fb41d452d9ec200000"),
			decoded: "2013-03-21T20:04:00.5Z",
			encoded: hex("56323031332d30332d32315432303a30343a30302e355a"),
			reasons: []string{
				reasonByteString,
				reasonTimeToInterface,
			},
			fixme: "decoding of tagged time into interface{} must produce RFC3339 timestamp compatible with JSON, not time.Time",
		},
		{
			example: hex("d74401020304"),
			decoded: "\x01\x02\x03\x04",
			encoded: hex("4401020304"),
			reasons: []string{
				reasonTagIgnored,
			},
			fixme: "unrecognized tags should not decode as cbor.Tag",
		},
		{
			example: hex("d818456449455446"),
			decoded: "dIETF",
			encoded: hex("456449455446"),
			reasons: []string{
				reasonTagIgnored,
			},
			fixme: "unrecognized tags should not decode as cbor.Tag",
		},
		{
			example: hex("d82076687474703a2f2f7777772e6578616d706c652e636f6d"),
			decoded: "http://www.example.com",
			encoded: hex("56687474703a2f2f7777772e6578616d706c652e636f6d"),
			reasons: []string{
				reasonByteString,
				reasonTagIgnored,
			},
			fixme: "unrecognized tags should not decode as cbor.Tag",
		},
		{
			example: hex("40"),
			decoded: "",
			fixme:   "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("4401020304"),
			decoded: "\x01\x02\x03\x04",
			fixme:   "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("60"),
			decoded: "",
			encoded: hex("40"),
			reasons: []string{
				reasonByteString,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("6161"),
			decoded: "a",
			encoded: hex("4161"),
			reasons: []string{
				reasonByteString,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("6449455446"),
			decoded: "IETF",
			encoded: hex("4449455446"),
			reasons: []string{
				reasonByteString,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("62225c"),
			decoded: "\"\\",
			encoded: hex("42225c"),
			reasons: []string{
				reasonByteString,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("62c3bc"),
			decoded: "ü",
			encoded: hex("42c3bc"),
			reasons: []string{
				reasonByteString,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("63e6b0b4"),
			decoded: "水",
			encoded: hex("43e6b0b4"),
			reasons: []string{
				reasonByteString,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("64f0908591"),
			decoded: "𐅑",
			encoded: hex("44f0908591"),
			reasons: []string{
				reasonByteString,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("80"),
			decoded: []interface{}{},
		},
		{
			example: hex("83010203"),
			decoded: []interface{}{int64(1), int64(2), int64(3)},
		},
		{
			example: hex("8301820203820405"),
			decoded: []interface{}{int64(1), []interface{}{int64(2), int64(3)}, []interface{}{int64(4), int64(5)}},
		},
		{
			example: hex("98190102030405060708090a0b0c0d0e0f101112131415161718181819"),
			decoded: []interface{}{int64(1), int64(2), int64(3), int64(4), int64(5), int64(6), int64(7), int64(8), int64(9), int64(10), int64(11), int64(12), int64(13), int64(14), int64(15), int64(16), int64(17), int64(18), int64(19), int64(20), int64(21), int64(22), int64(23), int64(24), int64(25)},
		},
		{
			example: hex("a0"),
			decoded: map[string]interface{}{},
		},
		{
			example: hex("a201020304"),
			reject:  "integer map keys don't correspond with field names or unstructured keys",
		},
		{
			example: hex("a26161016162820203"),
			decoded: map[string]interface{}{
				"a": int64(1),
				"b": []interface{}{int64(2), int64(3)},
			},
			encoded: hex("a24161014162820203"),
			reasons: []string{
				reasonByteString,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("826161a161626163"),
			decoded: []interface{}{
				"a",
				map[string]interface{}{"b": "c"},
			},
			encoded: hex("824161a141624163"),
			reasons: []string{
				reasonByteString,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("a56161614161626142616361436164614461656145"),
			decoded: map[string]interface{}{
				"a": "A",
				"b": "B",
				"c": "C",
				"d": "D",
				"e": "E",
			},
			encoded: hex("a54161414141624142416341434164414441654145"),
			reasons: []string{
				reasonByteString,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("5f42010243030405ff"),
			decoded: "\x01\x02\x03\x04\x05",
			encoded: hex("450102030405"),
			reasons: []string{
				reasonStringFixedLength,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("7f657374726561646d696e67ff"),
			decoded: "streaming",
			encoded: hex("4973747265616d696e67"),
			reasons: []string{
				reasonByteString,
				reasonStringFixedLength,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("9fff"),
			decoded: []interface{}{},
			encoded: hex("80"),
			reasons: []string{
				reasonArrayFixedLength,
			},
		},
		{
			example: hex("9f018202039f0405ffff"),
			decoded: []interface{}{
				int64(1),
				[]interface{}{int64(2), int64(3)},
				[]interface{}{int64(4), int64(5)},
			},
			encoded: hex("8301820203820405"),
			reasons: []string{
				reasonArrayFixedLength,
			},
		},
		{
			example: hex("9f01820203820405ff"),
			decoded: []interface{}{
				int64(1),
				[]interface{}{int64(2), int64(3)},
				[]interface{}{int64(4), int64(5)},
			},
			encoded: hex("8301820203820405"),
			reasons: []string{
				reasonArrayFixedLength,
			},
		},
		{
			example: hex("83018202039f0405ff"),
			decoded: []interface{}{
				int64(1),
				[]interface{}{int64(2), int64(3)},
				[]interface{}{int64(4), int64(5)},
			},
			encoded: hex("8301820203820405"),
			reasons: []string{
				reasonArrayFixedLength,
			},
		},
		{
			example: hex("83019f0203ff820405"),
			decoded: []interface{}{
				int64(1),
				[]interface{}{int64(2), int64(3)},
				[]interface{}{int64(4), int64(5)},
			},
			encoded: hex("8301820203820405"),
			reasons: []string{
				reasonArrayFixedLength,
			},
		},
		{
			example: hex("9f0102030405060708090a0b0c0d0e0f101112131415161718181819ff"),
			decoded: []interface{}{
				int64(1), int64(2), int64(3), int64(4), int64(5),
				int64(6), int64(7), int64(8), int64(9), int64(10),
				int64(11), int64(12), int64(13), int64(14), int64(15),
				int64(16), int64(17), int64(18), int64(19), int64(20),
				int64(21), int64(22), int64(23), int64(24), int64(25),
			},
			encoded: hex("98190102030405060708090a0b0c0d0e0f101112131415161718181819"),
			reasons: []string{
				reasonArrayFixedLength,
			},
		},
		{
			example: hex("bf61610161629f0203ffff"),
			decoded: map[string]interface{}{
				"a": int64(1),
				"b": []interface{}{int64(2), int64(3)},
			},
			encoded: hex("a24161014162820203"),
			reasons: []string{
				reasonArrayFixedLength,
				reasonByteString,
				reasonMapFixedLength,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("826161bf61626163ff"),
			decoded: []interface{}{"a", map[string]interface{}{"b": "c"}},
			encoded: hex("824161a141624163"),
			reasons: []string{
				reasonByteString,
				reasonMapFixedLength,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
		{
			example: hex("bf6346756ef563416d7421ff"),
			decoded: map[string]interface{}{
				"Amt": int64(-2),
				"Fun": true,
			},
			encoded: hex("a24346756ef543416d7421"),
			reasons: []string{
				reasonByteString,
				reasonMapFixedLength,
				reasonMapSorted,
			},
			fixme: "strings must encode to the byte string type to avoid producing invalid text strings",
		},
	} {
		t.Run(fmt.Sprintf("%x", tc.example), func(t *testing.T) {
			if tc.fixme != "" {
				t.Skip(tc.fixme) // TODO: Remove once all cases are fixed.
			}

			// The entries of Appendix A are generic data items, not Kubernetes objects,
			// so the expectations for decoding a Kubernetes object are all stubbed out
			// in this test.
			s := newSerializer(stubMetaFactory{gvk: &schema.GroupVersionKind{Kind: "Foo", Version: "v7"}}, stubCreater{obj: &anyObject{}}, nil)

			decoded, _, err := s.Decode(tc.example, nil, nil)
			if err != nil {
				if tc.reject != "" {
					t.Logf("decode failure expected (%s): %v", tc.reject, err)
					return
				}
				t.Fatalf("decode failed: %v", err)
			} else if tc.reject != "" {
				t.Fatalf("expected decode error (%v) did not occur", tc.reject)
			}

			var value interface{}
			if asAny, ok := decoded.(*anyObject); ok {
				value = asAny.Value
			} else {
				t.Fatalf("expected decoded object to have type %T, got %T", &anyObject{}, decoded)
			}

			if !eq.DeepEqual(tc.decoded, value) {
				t.Fatal(cmp.Diff(tc.decoded, value))
			}

			var buf bytes.Buffer
			if err := s.Encode(decoded, &buf); err != nil {
				t.Fatal(err)
			}
			actual := buf.Bytes()
			if !bytes.HasPrefix(actual, []byte{0xd9, 0xd9, 0xf7}) {
				t.Fatalf("0x%x does not begin with the self-described CBOR tag", actual)
			}
			actual = actual[3:] // strip self-described CBOR tag

			expected := tc.example
			if tc.encoded != nil {
				expected = tc.encoded
				if len(tc.reasons) == 0 {
					t.Fatal("invalid test case: missing reasons for difference between the example encoding and the actual encoding")
				}
				diff := cmp.Diff(tc.example, tc.encoded)
				if diff == "" {
					t.Fatal("actual encoding expected to differ from the example encoding, but does not")
				}
				t.Logf("actual encoding has expected differences from the example encoding:\n%s", diff)
				t.Logf("reasons for encoding differences:")
				for _, reason := range tc.reasons {
					t.Logf("- %s", reason)
				}

			}

			if diff := cmp.Diff(expected, actual); diff != "" {
				t.Errorf("re-encoded object differs from expected:\n%s", diff)
			}
		})
	}
}

func TestRecognizesData(t *testing.T) {
	for _, tc := range []struct {
		in         []byte
		recognizes bool
	}{
		{
			in:         nil,
			recognizes: false,
		},
		{
			in:         []byte{0xd9},
			recognizes: false,
		},
		{
			in:         []byte{0xd9, 0xd9},
			recognizes: false,
		},
		{
			in:         []byte{0xd9, 0xd9, 0xf7},
			recognizes: true,
		},
		{
			in:         []byte{0xff, 0xff, 0xff},
			recognizes: false,
		},
		{
			in:         []byte{0xd9, 0xd9, 0xf7, 0x01, 0x02, 0x03},
			recognizes: true,
		},
		{
			in:         []byte{0xff, 0xff, 0xff, 0x01, 0x02, 0x03},
			recognizes: false,
		},
	} {
		t.Run(hex.EncodeToString(tc.in), func(t *testing.T) {
			s := NewSerializer(nil, nil)
			recognizes, unknown, err := s.RecognizesData(tc.in)
			if recognizes != tc.recognizes {
				t.Errorf("expected recognized to be %t, got %t", tc.recognizes, recognizes)
			}
			if unknown {
				t.Error("expected unknown to be false, got true")
			}
			if err != nil {
				t.Errorf("expected nil error, got: %v", err)
			}
		})
	}
}

type stubWriter struct {
	n   int
	err error
}

func (w stubWriter) Write([]byte) (int, error) {
	return w.n, w.err
}

func TestEncode(t *testing.T) {
	for _, tc := range []struct {
		name          string
		in            runtime.Object
		w             io.Writer
		assertOnError func(*testing.T, error)
	}{
		{
			name: "io error writing self described cbor tag",
			w:    stubWriter{err: io.ErrShortWrite},
			assertOnError: func(t *testing.T, err error) {
				if err != io.ErrShortWrite {
					t.Errorf("expected io.ErrShortWrite, got: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSerializer(nil, nil)
			err := s.Encode(tc.in, tc.w)
			tc.assertOnError(t, err)
		})
	}
}

func TestDecode(t *testing.T) {
	for _, tc := range []struct {
		name          string
		options       []Option
		data          []byte
		gvk           *schema.GroupVersionKind
		metaFactory   metaFactory
		typer         runtime.ObjectTyper
		creater       runtime.ObjectCreater
		into          runtime.Object
		expectedObj   runtime.Object
		expectedGVK   *schema.GroupVersionKind
		assertOnError func(*testing.T, error)
	}{
		{
			name:        "error determining gvk",
			metaFactory: stubMetaFactory{err: errors.New("test")},
			assertOnError: func(t *testing.T, err error) {
				if err == nil || err.Error() != "test" {
					t.Errorf("expected error \"test\", got: %v", err)
				}
			},
		},
		{
			name:        "typer does not recognize into",
			gvk:         &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &anyObject{},
			expectedObj: nil,
			expectedGVK: &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			assertOnError: func(t *testing.T, err error) {
				if !runtime.IsNotRegisteredError(err) {
					t.Errorf("expected NotRegisteredError, got: %v", err)
				}
			},
		},
		{
			name:        "gvk from type of into",
			data:        []byte{0xf6},
			gvk:         &schema.GroupVersionKind{},
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       stubTyper{gvks: []schema.GroupVersionKind{{Group: "x", Version: "y", Kind: "z"}}},
			into:        &anyObject{},
			expectedObj: &anyObject{},
			expectedGVK: &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			assertOnError: func(t *testing.T, err error) {
				if err != nil {
					t.Errorf("expected nil error, got: %v", err)
				}
			},
		},
		{
			name:        "strict mode strict error",
			options:     []Option{Strict(true)},
			data:        []byte{0xa1, 0x61, 'z', 0x01}, // {'z': 1}
			gvk:         &schema.GroupVersionKind{},
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       stubTyper{gvks: []schema.GroupVersionKind{{Group: "x", Version: "y", Kind: "z"}}},
			into:        &metav1.PartialObjectMetadata{},
			expectedObj: &metav1.PartialObjectMetadata{},
			expectedGVK: &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			assertOnError: func(t *testing.T, err error) {
				if !runtime.IsStrictDecodingError(err) {
					t.Errorf("expected StrictDecodingError, got: %v", err)
				}
			},
		},
		{
			name:        "no strict mode no strict error",
			data:        []byte{0xa1, 0x61, 'z', 0x01}, // {'z': 1}
			gvk:         &schema.GroupVersionKind{},
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       stubTyper{gvks: []schema.GroupVersionKind{{Group: "x", Version: "y", Kind: "z"}}},
			into:        &metav1.PartialObjectMetadata{},
			expectedObj: &metav1.PartialObjectMetadata{},
			expectedGVK: &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			assertOnError: func(t *testing.T, err error) {
				if err != nil {
					t.Errorf("expected nil error, got: %v", err)
				}
			},
		},
		{
			name:        "unknown error from typer on into",
			gvk:         &schema.GroupVersionKind{},
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       stubTyper{err: errors.New("test")},
			into:        &anyObject{},
			expectedObj: nil,
			expectedGVK: &schema.GroupVersionKind{},
			assertOnError: func(t *testing.T, err error) {
				if err == nil || err.Error() != "test" {
					t.Errorf("expected error \"test\", got: %v", err)
				}
			},
		},
		{
			name:        "missing kind",
			gvk:         &schema.GroupVersionKind{Version: "v"},
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			expectedObj: nil,
			expectedGVK: &schema.GroupVersionKind{Version: "v"},
			assertOnError: func(t *testing.T, err error) {
				if !runtime.IsMissingKind(err) {
					t.Errorf("expected MissingKind, got: %v", err)
				}
			},
		},
		{
			name:        "missing version",
			gvk:         &schema.GroupVersionKind{Kind: "k"},
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			expectedObj: nil,
			expectedGVK: &schema.GroupVersionKind{Kind: "k"},
			assertOnError: func(t *testing.T, err error) {
				if !runtime.IsMissingVersion(err) {
					t.Errorf("expected MissingVersion, got: %v", err)
				}
			},
		},
		{
			name:        "creater error",
			gvk:         &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			creater:     stubCreater{err: errors.New("test")},
			expectedObj: nil,
			expectedGVK: &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			assertOnError: func(t *testing.T, err error) {
				if err == nil || err.Error() != "test" {
					t.Errorf("expected error \"test\", got: %v", err)
				}
			},
		},
		{
			name:        "unmarshal error",
			data:        nil, // EOF
			gvk:         &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			creater:     stubCreater{obj: &anyObject{}},
			expectedObj: nil,
			expectedGVK: &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			assertOnError: func(t *testing.T, err error) {
				if err != io.EOF {
					t.Errorf("expected EOF, got: %v", err)
				}
			},
		},
		{
			name:        "strict mode unmarshal error",
			options:     []Option{Strict(true)},
			data:        nil, // EOF
			gvk:         &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			creater:     stubCreater{obj: &anyObject{}},
			expectedObj: nil,
			expectedGVK: &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			assertOnError: func(t *testing.T, err error) {
				if err != io.EOF {
					t.Errorf("expected EOF, got: %v", err)
				}
			},
		},
		{
			name:        "into unstructured unmarshal error",
			data:        nil, // EOF
			gvk:         &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			into:        &unstructured.Unstructured{},
			expectedObj: nil,
			expectedGVK: &schema.GroupVersionKind{Group: "x", Version: "y", Kind: "z"},
			assertOnError: func(t *testing.T, err error) {
				if err != io.EOF {
					t.Errorf("expected EOF, got: %v", err)
				}
			},
		},
		{
			name:        "into unstructured missing kind",
			data:        []byte("\xa1\x6aapiVersion\x61v"),
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			into:        &unstructured.Unstructured{},
			expectedObj: nil,
			expectedGVK: &schema.GroupVersionKind{Version: "v"},
			assertOnError: func(t *testing.T, err error) {
				if !runtime.IsMissingKind(err) {
					t.Errorf("expected MissingKind, got: %v", err)
				}
			},
		},
		{
			name:        "into unstructured missing version",
			data:        []byte("\xa1\x64kind\x61k"),
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			into:        &unstructured.Unstructured{},
			expectedObj: nil,
			expectedGVK: &schema.GroupVersionKind{Kind: "k"},
			assertOnError: func(t *testing.T, err error) {
				if !runtime.IsMissingVersion(err) {
					t.Errorf("expected MissingVersion, got: %v", err)
				}
			},
		},
		{
			name:        "into unstructured",
			data:        []byte("\xa2\x6aapiVersion\x61v\x64kind\x61k"),
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			into:        &unstructured.Unstructured{},
			expectedObj: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v",
				"kind":       "k",
			}},
			expectedGVK: &schema.GroupVersionKind{Version: "v", Kind: "k"},
			assertOnError: func(t *testing.T, err error) {
				if err != nil {
					t.Errorf("expected nil error, got: %v", err)
				}
			},
		},
		{
			name:        "using unstructured creater",
			data:        []byte("\xa2\x6aapiVersion\x61v\x64kind\x61k"),
			metaFactory: &defaultMetaFactory{},
			creater:     stubCreater{obj: &unstructured.Unstructured{}},
			expectedObj: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v",
				"kind":       "k",
			}},
			expectedGVK: &schema.GroupVersionKind{Version: "v", Kind: "k"},
			assertOnError: func(t *testing.T, err error) {
				if err != nil {
					t.Errorf("expected nil error, got: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSerializer(tc.metaFactory, tc.creater, tc.typer, tc.options...)

			actualObj, actualGVK, err := s.Decode(tc.data, tc.gvk, tc.into)
			tc.assertOnError(t, err)

			if !reflect.DeepEqual(tc.expectedObj, actualObj) {
				t.Error(cmp.Diff(tc.expectedObj, actualObj))
			}

			if diff := cmp.Diff(tc.expectedGVK, actualGVK); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestMetaFactoryInterpret(t *testing.T) {
	mf := &defaultMetaFactory{}
	_, err := mf.Interpret(nil)
	if err == nil {
		t.Error("expected non-nil error")
	}
	gvk, err := mf.Interpret([]byte("\xa2\x6aapiVersion\x63a/b\x64kind\x61c"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff := cmp.Diff(&schema.GroupVersionKind{Group: "a", Version: "b", Kind: "c"}, gvk); diff != "" {
		t.Error(diff)
	}
}

type stubTyper struct {
	gvks        []schema.GroupVersionKind
	unversioned bool
	err         error
}

func (t stubTyper) ObjectKinds(obj runtime.Object) ([]schema.GroupVersionKind, bool, error) {
	return t.gvks, t.unversioned, t.err
}

func (stubTyper) Recognizes(schema.GroupVersionKind) bool {
	return false
}

type stubCreater struct {
	obj runtime.Object
	err error
}

func (c stubCreater) New(gvk schema.GroupVersionKind) (runtime.Object, error) {
	return c.obj, c.err
}

type notRegisteredTyper struct{}

func (notRegisteredTyper) ObjectKinds(obj runtime.Object) ([]schema.GroupVersionKind, bool, error) {
	return nil, false, runtime.NewNotRegisteredErrForType("test", reflect.TypeOf(obj))
}

func (notRegisteredTyper) Recognizes(schema.GroupVersionKind) bool {
	return false
}

type stubMetaFactory struct {
	gvk *schema.GroupVersionKind
	err error
}

func (mf stubMetaFactory) Interpret([]byte) (*schema.GroupVersionKind, error) {
	return mf.gvk, mf.err
}
