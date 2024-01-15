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

package test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/cbor"
	cbordirect "k8s.io/apimachinery/pkg/runtime/serializer/cbor/direct"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/runtime/serializer/protobuf"
	utiljson "k8s.io/apimachinery/pkg/util/json"
)

func TestRawExtensionInteroperability(t *testing.T) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Version: "v1", Kind: "List"}, &metav1.List{})

	sjson := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme, scheme, json.SerializerOptions{})
	sprotobuf := protobuf.NewSerializer(scheme, scheme)
	scbor := cbor.NewSerializer(scheme, scheme)

	helloWorldMap := map[string]interface{}{"hello": "world"}

	var jsonRawExtension runtime.RawExtension
	if jsonHelloWorldMap, err := utiljson.Marshal(helloWorldMap); err != nil {
		t.Fatal(err)
	} else if err := utiljson.Unmarshal(jsonHelloWorldMap, &jsonRawExtension); err != nil {
		t.Fatal(err)
	}

	var cborRawExtension runtime.RawExtension
	if cborHelloWorldMap, err := cbordirect.Marshal(helloWorldMap); err != nil {
		t.Fatal(err)
	} else if err := cbordirect.Unmarshal(cborHelloWorldMap, &cborRawExtension); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name       string
		from       runtime.Object
		into       reflect.Type
		want       runtime.Object
		serializer runtime.Serializer
	}{
		{
			name: "anything-in-protobuf native-to-native",
			from: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{{Raw: []byte{0x00, '\n', 'z', 0xff, 0xff, 0xff, '#'}}},
			},
			into: reflect.TypeFor[metav1.List](),
			want: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{{Raw: []byte{0x00, '\n', 'z', 0xff, 0xff, 0xff, '#'}}},
			},
			serializer: sprotobuf,
		},
		{
			name: "json-in-json native-to-native",
			from: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{jsonRawExtension},
			},
			into: reflect.TypeFor[metav1.List](),
			want: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{jsonRawExtension},
			},
			serializer: sjson,
		},
		{
			name: "json-in-json native-to-unstructured",
			from: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{jsonRawExtension},
			},
			into: reflect.TypeFor[unstructured.Unstructured](),
			want: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "List",
				"metadata":   map[string]interface{}{},
				"items": []interface{}{
					map[string]interface{}{
						"hello": "world",
					},
				},
			}},
			serializer: sjson,
		},
		{
			name: "json-in-json unstructured-to-native",
			from: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "List",
				"metadata":   map[string]interface{}{},
				"items": []interface{}{
					map[string]interface{}{
						"hello": "world",
					},
				},
			}},
			into: reflect.TypeFor[metav1.List](),
			want: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{jsonRawExtension},
			},
			serializer: sjson,
		},
		{
			name: "json-in-json unstructured-to-unstructured",
			from: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "List",
				"metadata":   map[string]interface{}{},
				"items": []interface{}{
					map[string]interface{}{
						"hello": "world",
					},
				},
			}},
			into: reflect.TypeFor[unstructured.Unstructured](),
			want: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "List",
				"metadata":   map[string]interface{}{},
				"items": []interface{}{
					map[string]interface{}{
						"hello": "world",
					},
				},
			}},
			serializer: sjson,
		},
		{
			name: "cbor-in-cbor native-to-native",
			from: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{cborRawExtension},
			},
			into: reflect.TypeFor[metav1.List](),
			want: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{cborRawExtension},
			},
			serializer: scbor,
		},
		{
			name: "cbor-in-cbor native-to-unstructured",
			from: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{cborRawExtension},
			},
			into: reflect.TypeFor[unstructured.Unstructured](),
			want: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "List",
				"metadata":   map[string]interface{}{},
				"items": []interface{}{
					map[string]interface{}{
						"hello": "world",
					},
				},
			}}, serializer: scbor,
		},
		{
			name: "cbor-in-cbor unstructured-to-native",
			from: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "List",
				"metadata":   map[string]interface{}{},
				"items": []interface{}{
					map[string]interface{}{
						"hello": "world",
					},
				},
			}},
			into: reflect.TypeFor[metav1.List](),
			want: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{cborRawExtension},
			},
			serializer: scbor,
		},
		{
			name: "cbor-in-cbor unstructured-to-unstructured",
			from: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "List",
				"metadata":   map[string]interface{}{},
				"items": []interface{}{
					map[string]interface{}{
						"hello": "world",
					},
				},
			}},
			into: reflect.TypeFor[unstructured.Unstructured](),
			want: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "List",
				"metadata":   map[string]interface{}{},
				"items": []interface{}{
					map[string]interface{}{
						"hello": "world",
					},
				},
			}}, serializer: scbor,
		},
		{
			name: "json-in-cbor native-to-native",
			from: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{jsonRawExtension},
			},
			into: reflect.TypeFor[metav1.List](),
			want: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{jsonRawExtension},
			},
			serializer: scbor,
		},
		{
			name: "json-in-cbor native-to-unstructured",
			from: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{jsonRawExtension},
			},
			into: reflect.TypeFor[unstructured.Unstructured](),
			want: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "List",
				"metadata":   map[string]interface{}{},
				"items": []interface{}{
					map[string]interface{}{
						"hello": "world",
					},
				},
			}}, serializer: scbor,
		},
		{
			name: "cbor-in-json native-to-native",
			from: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{cborRawExtension},
			},
			into: reflect.TypeFor[metav1.List](),
			want: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{cborRawExtension},
			},
			serializer: sjson,
		},
		{
			name: "cbor-in-json native-to-unstructured",
			from: &metav1.List{
				TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "List"},
				Items:    []runtime.RawExtension{cborRawExtension},
			},
			into: reflect.TypeFor[unstructured.Unstructured](),
			want: &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "List",
				"metadata":   map[string]interface{}{},
				"items": []interface{}{
					map[string]interface{}{
						"hello": "world",
					},
				},
			}}, serializer: sjson,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buffer bytes.Buffer
			if err := tc.serializer.Encode(tc.from, &buffer); err != nil {
				t.Fatalf("encode error: %v", err)
			}

			into := reflect.New(tc.into)
			if _, _, err := tc.serializer.Decode(buffer.Bytes(), nil, into.Interface().(runtime.Object)); err != nil {
				t.Fatalf("decode error: %v", err)
			}

			if !equality.Semantic.DeepEqual(tc.want, into.Interface()) {
				t.Errorf("diff:\n%s", cmp.Diff(tc.want, into.Interface()))
			}
		})
	}
}
