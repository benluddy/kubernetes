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
	"io"
	"reflect"

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
		},
	}

	return handle
}()

type Serializer struct{}

func NewSerializer() *Serializer {
	return &Serializer{}
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

func (s *Serializer) Decode(data []byte, gvk *schema.GroupVersionKind, into runtime.Object) (runtime.Object, *schema.GroupVersionKind, error) {
	panic("unimplemented")
}
