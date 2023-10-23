/*
Copyright 2023 The Kubernetes Authors.

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
	"math"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/google/go-cmp/cmp"
	"github.com/ugorji/go/codec"
)

// passthrough wraps arbitrary concrete values to be encoded or decoded.
type passthrough struct {
	Value interface{}
}

func (p passthrough) GetObjectKind() schema.ObjectKind {
	return p
}

func (passthrough) SetGroupVersionKind(schema.GroupVersionKind) {
	panic("unimplemented")
}

func (passthrough) GroupVersionKind() schema.GroupVersionKind {
	panic("unimplemented")
}

func (passthrough) DeepCopyObject() runtime.Object {
	panic("unimplemented")
}

func (o passthrough) CodecEncodeSelf(e *codec.Encoder) {
	e.MustEncode(o.Value)
}

func (o *passthrough) CodecDecodeSelf(d *codec.Decoder) {
	d.MustDecode(&o.Value)
}

func TestEncode(t *testing.T) {
	for _, tc := range []struct {
		in       interface{}
		expected string
	}{
		{
			in:       int64(0),
			expected: "00",
		},
		{
			in:       int64(1),
			expected: "01",
		},
		{
			in:       int64(10),
			expected: "0a",
		},
		{
			in:       int64(23),
			expected: "17",
		},
		{
			in:       int64(24),
			expected: "1818",
		},
		{
			in:       int64(25),
			expected: "1819",
		},
		{
			in:       int64(100),
			expected: "1864",
		},
		{
			in:       int64(1000),
			expected: "1903e8",
		},
		{
			in:       int64(1000000),
			expected: "1a000f4240",
		},
		{
			in:       int64(1000000000000),
			expected: "1b000000e8d4a51000",
		},
		{
			in:       int64(-1),
			expected: "20",
		},
		{
			in:       int64(-10),
			expected: "29",
		},
		{
			in:       int64(-100),
			expected: "3863",
		},
		{
			in:       int64(-1000),
			expected: "3903e7",
		},
		{
			in:       0.0,
			expected: "f90000",
		},
		{
			in:       math.Copysign(0, -1),
			expected: "f98000",
		},
		{
			in:       1.0,
			expected: "f93c00",
		},
		{
			in:       1.1,
			expected: "fb3ff199999999999a",
		},
		{
			in:       1.5,
			expected: "f93e00",
		},
		{
			in:       65504.0,
			expected: "f97bff",
		},
		{
			in:       100000.0,
			expected: "fa47c35000",
		},
		{
			in:       3.4028234663852886e+38,
			expected: "fa7f7fffff",
		},
		{
			in:       1.0e+300,
			expected: "fb7e37e43c8800759c",
		},
		{
			in:       5.960464477539063e-8,
			expected: "f90001",
		},
		{
			in:       0.00006103515625,
			expected: "f90400",
		},
		{
			in:       -4.0,
			expected: "f9c400",
		},
		{
			in:       -4.1,
			expected: "fbc010666666666666",
		},
		// TODO: Should Inf/-Inf/NaN be supported? Current Protobuf will encode this, but
		// JSON will produce an error.  This is less than ideal -- we can't transcode
		// everything to JSON.
		{
			in:       math.Inf(1),
			expected: "f97c00",
		},
		{
			in:       math.Float64frombits(0x7ff8000000000001), // NaN
			expected: "fb7ff8000000000001",
		},
		{
			// RFC 8949: "For NaN values, a shorter encoding is preferred if
			// zero-padding the shorter significand towards the right reconstitutes the
			// original NaN value (for many applications, the single NaN encoding
			// 0xf97e00 will suffice)."
			//
			// The preferred half-precision encoding isn't currently implemented. It
			// encodes as double precision, which should be okay as long as the
			// half-precision encoding can be decoded.
			in:       math.Float64frombits(0x7ff8000000000000), // NaN
			expected: "fb7ff8000000000000",
		},
		{
			in:       math.Inf(-1),
			expected: "f9fc00",
		},
		{
			in:       false,
			expected: "f4",
		},
		{
			in:       true,
			expected: "f5",
		},
		{
			in:       nil,
			expected: "f6",
		},
		{
			in:       "",
			expected: "40",
		},
		{
			in:       "A",
			expected: "4141",
		},
		{
			in:       "\x01\x02\x03\x04",
			expected: "4401020304",
		},
		{
			in:       "IETF",
			expected: "4449455446",
		},
		{
			in:       "\"\\",
			expected: "42225c",
		},
		{
			in:       []interface{}{},
			expected: "80",
		},
		{
			in:       []interface{}{1, 2, 3},
			expected: "83010203",
		},
		{
			in:       []interface{}{1, []interface{}{2, 3}, []interface{}{4, 5}},
			expected: "8301820203820405",
		},
		{
			in:       []interface{}{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25},
			expected: "98190102030405060708090a0b0c0d0e0f101112131415161718181819",
		},
		{
			in:       map[string]interface{}{},
			expected: "a0",
		},
		{
			in:       map[string]interface{}{"a": 1, "b": []interface{}{2, 3}},
			expected: "a24161014162820203",
		},
		{
			in:       []interface{}{"A", map[interface{}]interface{}{"B": "C"}},
			expected: "824141a141424143",
		},
		{
			in:       map[string]interface{}{"a": "A", "b": "B", "c": "C", "d": "D", "e": "E"},
			expected: "a54161414141624142416341434164414441654145",
		},
		{
			in:       "ü",
			expected: "42c3bc",
		},
		{
			in:       "水",
			expected: "43e6b0b4",
		},
		{
			in:       "𐅑",
			expected: "44f0908591",
		},
	} {
		t.Run(fmt.Sprintf("%T(%v)", tc.in, tc.in), func(t *testing.T) {
			s := NewSerializer(nil, nil)
			var buf bytes.Buffer
			err := s.Encode(passthrough{tc.in}, &buf)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			expected, err := hex.DecodeString("d9d9f7" + tc.expected)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(expected, buf.Bytes()) {
				t.Errorf("expected: %x\nactual: %x", expected, buf.Bytes())
			}
		})
	}
}

func TestRecognizesData(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{
		{
			in: "",
			ok: false,
		},
		{
			in: "d9",
			ok: false,
		},
		{
			in: "d9d9",
			ok: false,
		},
		{
			in: "d9d9f7",
			ok: true,
		},
		{
			in: "ffffff",
			ok: false,
		},
		{
			in: "d9d9f7000102030405060708090a0b0c0d0e0f",
			ok: true,
		},
		{
			in: "ffffff000102030405060708090a0b0c0d0e0f",
			ok: false,
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			in, err := hex.DecodeString(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			s := NewSerializer(nil, nil)
			actual, unknown, err := s.RecognizesData(in)
			if actual != tc.ok {
				t.Errorf("expected recognized to be %t, got %t", tc.ok, actual)
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

func TestDecode(t *testing.T) {
	eq := conversion.EqualitiesOrDie(
		// NaN float64 values are always inequal and have multiple representations. For the
		// purposes of comparing a decoded float64 with the expected float64, treat two NaNs
		// as equal if they are represented by an identical sequence of bits.
		func(a float64, b float64) bool {
			if !math.IsNaN(a) || !math.IsNaN(b) {
				return a == b
			}
			return math.Float64bits(a) == math.Float64bits(b)
		},
	)

	for _, tc := range []struct {
		data        string
		metaFactory metaFactory
		typer       runtime.ObjectTyper
		into        runtime.Object
		expected    runtime.Object
		fail        bool
	}{
		{
			data:        "00",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(0)},
		},
		{
			data:        "01",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(1)},
		},
		{
			data:        "0a",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(10)},
		},
		{
			data:        "17",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(23)},
		},
		{
			data:        "1818",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(24)},
		},
		{
			data:        "1819",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(25)},
		},
		{
			data:        "1864",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(100)},
		},
		{
			data:        "1903e8",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(1000)},
		},
		{
			data:        "1a000f4240",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(1000000)},
		},
		{
			data:        "1b000000e8d4a51000",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(1000000000000)},
		},
		{
			data:        "20",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(-1)},
		},
		{
			data:        "29",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(-10)},
		},
		{
			data:        "3863",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(-100)},
		},
		{
			data:        "3903e7",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{int64(-1000)},
		},
		{
			data:        "f90000",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{0.0},
		},
		{
			data:        "f98000",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{math.Copysign(0, -1)},
		},
		{
			data:        "f93c00",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{1.0},
		},
		{
			data:        "fb3ff199999999999a",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{1.1},
		},
		{
			data:        "f93e00",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{1.5},
		},
		{
			data:        "f97bff",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{65504.0},
		},
		{
			data:        "fa47c35000",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{100000.0},
		},
		{
			data:        "fa7f7fffff",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{3.4028234663852886e+38},
		},
		{
			data:        "fb7e37e43c8800759c",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{1.0e+300},
		},
		{
			data:        "f90001",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{5.960464477539063e-8},
		},
		{
			data:        "f90400",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{0.00006103515625},
		},
		{
			data:        "f9c400",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{-4.0},
		},
		{
			data:        "fbc010666666666666",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{-4.1},
		},
		{
			data:        "f97c00",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{math.Inf(1)},
		},
		{
			data:        "fb7ff8000000000001",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{math.Float64frombits(0x7ff8000000000001)},
		},
		{
			data:        "fb7ff8000000000000",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{math.Float64frombits(0x7ff8000000000000)},
		},
		{
			data:        "f9fc00",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{math.Inf(-1)},
		},
		{
			data:        "f4",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{false},
		},
		{
			data:        "f5",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{true},
		},
		{
			data:        "f6",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{},
		},
		{
			data:        "40",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{""},
		},
		{
			data:        "4141",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{"A"},
		},
		{
			data:        "4401020304",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{"\x01\x02\x03\x04"},
		},
		{
			data:        "4449455446",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{"IETF"},
		},
		{
			data:        "42225c",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{"\"\\"},
		},
		{
			data:        "80",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{[]interface{}{}},
		},
		{
			data:        "83010203",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{[]interface{}{int64(1), int64(2), int64(3)}},
		},
		{
			data:        "8301820203820405",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{[]interface{}{int64(1), []interface{}{int64(2), int64(3)}, []interface{}{int64(4), int64(5)}}},
		},
		{
			data:        "98190102030405060708090a0b0c0d0e0f101112131415161718181819",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{[]interface{}{int64(1), int64(2), int64(3), int64(4), int64(5), int64(6), int64(7), int64(8), int64(9), int64(10), int64(11), int64(12), int64(13), int64(14), int64(15), int64(16), int64(17), int64(18), int64(19), int64(20), int64(21), int64(22), int64(23), int64(24), int64(25)}},
		},
		{
			data:        "a0",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{map[string]interface{}{}},
		},
		{
			data:        "a24161014162820203",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{map[string]interface{}{"a": int64(1), "b": []interface{}{int64(2), int64(3)}}},
		},
		{
			data:        "824141a141424143",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{[]interface{}{"A", map[string]interface{}{"B": "C"}}},
		},
		{
			data:        "a54161414141624142416341434164414441654145",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{map[string]interface{}{"a": "A", "b": "B", "c": "C", "d": "D", "e": "E"}},
		},
		{
			data:        "42c3bc",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{"ü"},
		},
		{
			data:        "43e6b0b4",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{"水"},
		},
		{
			data:        "44f0908591",
			metaFactory: stubMetaFactory{gvk: &schema.GroupVersionKind{}},
			typer:       notRegisteredTyper{},
			into:        &passthrough{},
			expected:    &passthrough{"𐅑"},
		},
		{
			data:        "",
			metaFactory: stubMetaFactory{err: errors.New("test")},
			fail:        true,
		},
	} {
		t.Run(tc.data, func(t *testing.T) {
			data, err := hex.DecodeString(tc.data)
			if err != nil {
				t.Fatal(err)
			}

			s := newSerializer(tc.metaFactory, tc.typer, nil)
			actual, _, err := s.Decode(data, nil, tc.into)
			if err != nil && !tc.fail {
				t.Fatalf("unexpected error: %v", err)
			} else if err == nil && tc.fail {
				t.Fatal("expected non-nil error")
			}

			if !eq.DeepEqual(tc.expected, actual) {
				t.Error(cmp.Diff(tc.expected, actual))
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
