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
	"fmt"
	"io"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/ugorji/go/codec"
)

var handle = func() codec.CborHandle {
	handle := codec.CborHandle{
		BasicHandle: codec.BasicHandle{
			TypeInfos: codec.NewTypeInfos([]string{"json"}),
			EncodeOptions: codec.EncodeOptions{
				Canonical:   true,
				StringToRaw: true,
				OptimumSize: true,
			},
			DecodeOptions: codec.DecodeOptions{
				MapType:         reflect.TypeOf(map[string]interface{}(nil)),
				SignedInteger:   true,
				RawToString:     true,
				ValidateUnicode: true,
			},
		},
		SkipUnexpectedTags: true,
	}

	return handle
}()

type metaFactory interface {
	// Interpret should return the version and kind of the wire-format of the object.
	Interpret(data []byte) (*schema.GroupVersionKind, error)
}

type defaultMetaFactory struct{}

func (mf *defaultMetaFactory) Interpret(data []byte) (*schema.GroupVersionKind, error) {
	var tm metav1.TypeMeta
	if err := codec.NewDecoderBytes(data, &handle).Decode(&tm); err != nil {
		return nil, fmt.Errorf("unable to determine group/version/kind: %w", err)
	}
	actual := tm.GetObjectKind().GroupVersionKind()
	return &actual, nil
}

type Serializer struct {
	metaFactory metaFactory
	typer       runtime.ObjectTyper
	creater     runtime.ObjectCreater
}

func NewSerializer(typer runtime.ObjectTyper, creater runtime.ObjectCreater) *Serializer {
	return newSerializer(&defaultMetaFactory{}, typer, creater)
}

func newSerializer(metaFactory metaFactory, typer runtime.ObjectTyper, creater runtime.ObjectCreater) *Serializer {
	return &Serializer{
		metaFactory: metaFactory,
		typer:       typer,
		creater:     creater,
	}
}

func (s *Serializer) Identifier() runtime.Identifier {
	return "cbor"
}

func (s *Serializer) Encode(obj runtime.Object, w io.Writer) error {
	// https://www.rfc-editor.org/rfc/rfc8949.html#name-self-described-cbor
	if _, err := w.Write([]byte{0xd9, 0xd9, 0xf7}); err != nil {
		return err
	}
	return codec.NewEncoder(w, &handle).Encode(obj)
}

// gvkWithDefaults returns group kind and version defaulting from provided default
func gvkWithDefaults(actual, defaultGVK schema.GroupVersionKind) schema.GroupVersionKind {
	if len(actual.Kind) == 0 {
		actual.Kind = defaultGVK.Kind
	}
	if len(actual.Version) == 0 && len(actual.Group) == 0 {
		actual.Group = defaultGVK.Group
		actual.Version = defaultGVK.Version
	}
	if len(actual.Version) == 0 && actual.Group == defaultGVK.Group {
		actual.Version = defaultGVK.Version
	}
	return actual
}

func (s *Serializer) Decode(data []byte, gvk *schema.GroupVersionKind, into runtime.Object) (runtime.Object, *schema.GroupVersionKind, error) {
	actual, err := s.metaFactory.Interpret(data)
	if err != nil {
		return nil, nil, err
	}

	if gvk != nil {
		*actual = gvkWithDefaults(*actual, *gvk)
	}

	if into != nil {
		types, _, err := s.typer.ObjectKinds(into)
		switch {
		case runtime.IsNotRegisteredError(err):
			if err := codec.NewDecoderBytes(data, &handle).Decode(into); err != nil {
				return nil, actual, err
			}
			return into, actual, nil
		case err != nil:
			return nil, actual, err
		default:
			*actual = gvkWithDefaults(*actual, types[0])
		}
	}

	if len(actual.Kind) == 0 {
		return nil, actual, runtime.NewMissingKindErr("<cbor>")
	}
	if len(actual.Version) == 0 {
		return nil, actual, runtime.NewMissingVersionErr("<cbor>")
	}

	obj, err := runtime.UseOrCreateObject(s.typer, s.creater, *actual, into)
	if err != nil {
		return nil, actual, err
	}

	if err := codec.NewDecoderBytes(data, &handle).Decode(obj); err != nil {
		return nil, actual, err
	}

	return obj, actual, nil
}

func (s *Serializer) RecognizesData(data []byte) (ok, unknown bool, err error) {
	// TODO: Return unknown on missing prefix to accept untagged CBOR?
	return bytes.HasPrefix(data, []byte{0xd9, 0xd9, 0xf7}), false, nil
}
