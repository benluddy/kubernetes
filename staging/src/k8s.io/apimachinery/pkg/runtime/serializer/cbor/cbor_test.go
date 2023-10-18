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
	"fmt"
	"math"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ugorji/go/codec"
)

// passthrough wraps arbitrary concrete values to be encoded or decoded.
type passthrough struct {
	value interface{}
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
			in:       map[interface{}]interface{}{1: 2, 3: 4},
			expected: "a201020304",
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
			in:       map[interface{}]interface{}{"a": "A", "b": "B", "c": "C", "d": "D", "e": "E"},
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
			s := NewSerializer()
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
