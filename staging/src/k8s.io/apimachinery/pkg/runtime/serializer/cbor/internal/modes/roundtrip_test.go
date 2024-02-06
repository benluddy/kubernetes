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

package modes_test

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

func nilPointerFor[T interface{}]() *T {
	return nil
}

func TestRoundtrip(t *testing.T) {
	type modePair struct {
		enc cbor.EncMode
		dec cbor.DecMode
	}

	for _, tc := range []struct {
		name      string
		modePairs []modePair
		obj       interface{}
	}{
		{
			name: "nil slice",
			obj:  []interface{}(nil),
		},
		{
			name: "nil map",
			obj:  map[string]interface{}(nil),
		},
		{
			name: "empty slice",
			obj:  []interface{}{},
		},
		{
			name: "empty map",
			obj:  map[string]interface{}{},
		},
		{
			name: "nil pointer to slice",
			obj:  nilPointerFor[[]interface{}](),
		},
		{
			name: "nil pointer to map",
			obj:  nilPointerFor[map[string]interface{}](),
		},
		{
			name: "nonempty string",
			obj:  "hello world",
		},
		{
			name: "empty string",
			obj:  "",
		},
		{
			name: "true",
			obj:  true,
		},
		{
			name: "false",
			obj:  false,
		},
		{
			name: "int64",
			obj:  int64(5),
		},
		{
			name: "int64 max",
			obj:  math.MaxInt64,
		},
		{
			name: "int64 min",
			obj:  math.MinInt64,
		},
		{
			name: "float64",
			obj:  float64(2.71),
		},
		{
			name: "float64 max",
			obj:  math.MaxFloat64,
		},
		{
			name: "float64 no fractional component",
			obj:  float64(5),
		},
		{
			name: "time.Time",
			obj:  time.Date(2222, time.May, 4, 12, 13, 14, 123, time.UTC),
		},
		{
			name: "int64 omitempty",
			obj: struct {
				V int64 `json:"v,omitempty"`
			}{},
		},
		{
			name: "float64 omitempty",
			obj: struct {
				V float64 `json:"v,omitempty"`
			}{},
		},
		{
			name: "string omitempty",
			obj: struct {
				V string `json:"v,omitempty"`
			}{},
		},
		{
			name: "bool omitempty",
			obj: struct {
				V bool `json:"v,omitempty"`
			}{},
		},
		{
			name: "nil pointer omitempty",
			obj: struct {
				V *struct{} `json:"v,omitempty"`
			}{},
		},
		{
			name: "nil pointer to slice as struct field",
			obj: struct {
				V *[]interface{} `json:"v"`
			}{},
		},
		{
			name: "nil pointer to slice as struct field with omitempty",
			obj: struct {
				V *[]interface{} `json:"v,omitempty"`
			}{},
		},
		{
			name: "nil pointer to map as struct field",
			obj: struct {
				V *map[string]interface{} `json:"v"`
			}{},
		},
		{
			name: "nil pointer to map as struct field with omitempty",
			obj: struct {
				V *map[string]interface{} `json:"v,omitempty"`
			}{},
		},
	} {
		mps := tc.modePairs
		if len(mps) == 0 {
			// Default is all modes to all modes.
			mps = []modePair{}
			for em := range encModeNames {
				for dm := range decModeNames {
					mps = append(mps, modePair{enc: em, dec: dm})
				}
			}
		}

		for _, mp := range mps {
			encModeName, ok := encModeNames[mp.enc]
			if !ok {
				t.Fatal("test case configured to run against unrecognized encode mode")
			}

			decModeName, ok := decModeNames[mp.dec]
			if !ok {
				t.Fatal("test case configured to run against unrecognized decode mode")
			}

			t.Run(fmt.Sprintf("enc=%s/dec=%s/%s", encModeName, decModeName, tc.name), func(t *testing.T) {
				original := tc.obj

				b, err := mp.enc.Marshal(original)
				if err != nil {
					t.Fatalf("unexpected error from Marshal: %v", err)
				}

				final := reflect.New(reflect.TypeOf(original))
				err = mp.dec.Unmarshal(b, final.Interface())
				if err != nil {
					t.Fatalf("unexpected error from Unmarshal: %v", err)
				}
				if !reflect.DeepEqual(original, final.Elem().Interface()) {
					t.Errorf("roundtrip difference:\nwant: %#v\ngot: %#v", original, final)
				}
			})
		}
	}
}
