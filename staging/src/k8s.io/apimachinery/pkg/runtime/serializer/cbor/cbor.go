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
	"fmt"
	"io"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/cbor/internal/modes"
	"k8s.io/apimachinery/pkg/runtime/serializer/recognizer"
	util "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/fxamacker/cbor/v2"
)

type metaFactory interface {
	// Interpret should return the version and kind of the wire-format of the object.
	Interpret(data []byte) (*schema.GroupVersionKind, error)
}

type defaultMetaFactory struct{}

func (mf *defaultMetaFactory) Interpret(data []byte) (*schema.GroupVersionKind, error) {
	var tm metav1.TypeMeta
	if err := modes.DecodeLax.Unmarshal(data, &tm); err != nil {
		return nil, fmt.Errorf("unable to determine group/version/kind: %w", err)
	}
	actual := tm.GetObjectKind().GroupVersionKind()
	return &actual, nil
}

type Serializer interface {
	runtime.Serializer
	recognizer.RecognizingDecoder
}

var _ Serializer = &serializer{}

type options struct {
	strict bool
}

type Option func(*options)

func Strict(s bool) Option {
	return func(opts *options) {
		opts.strict = s
	}
}

type serializer struct {
	metaFactory metaFactory
	creater     runtime.ObjectCreater
	typer       runtime.ObjectTyper
	options     options
}

func NewSerializer(creater runtime.ObjectCreater, typer runtime.ObjectTyper, options ...Option) Serializer {
	return newSerializer(&defaultMetaFactory{}, creater, typer, options...)
}

func newSerializer(metaFactory metaFactory, creater runtime.ObjectCreater, typer runtime.ObjectTyper, options ...Option) *serializer {
	s := &serializer{
		metaFactory: metaFactory,
		creater:     creater,
		typer:       typer,
	}
	for _, o := range options {
		o(&s.options)
	}
	return s
}

func (s *serializer) Identifier() runtime.Identifier {
	return "cbor"
}

func (s *serializer) Encode(obj runtime.Object, w io.Writer) error {
	// https://www.rfc-editor.org/rfc/rfc8949.html#name-self-described-cbor
	if _, err := w.Write([]byte("\xd9\xd9\xf7")); err != nil {
		return err
	}

	e := modes.Encode.NewEncoder(w)
	if u, ok := obj.(runtime.Unstructured); ok {
		return e.Encode(u.UnstructuredContent())
	}
	return e.Encode(obj)
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

// diagnose returns the diagnostic encoding of a well-formed CBOR data item.
func diagnose(data []byte) string {
	diag, err := modes.Diagnostic.Diagnose(data)
	if err != nil {
		// Since the input must already be well-formed CBOR, converting it to diagnostic
		// notation should not fail.
		util.HandleError(err)

		return hex.EncodeToString(data)
	}
	return diag
}

func (s *serializer) unmarshal(data []byte, into interface{}) (strict, lax error) {
	if u, ok := into.(runtime.Unstructured); ok {
		var content map[string]interface{}
		defer func() {
			// TODO: The UnstructuredList implementation of SetUnstructuredContent is
			// not identical to what unstructuredJSONScheme does: (1) it retains the
			// "items" key in its Object field, and (2) it does not infer a singular
			// Kind from the list's Kind and populate omitted apiVersion/kind for all
			// entries in Items.
			u.SetUnstructuredContent(content)
		}()
		into = &content
	}

	if !s.options.strict {
		return nil, modes.DecodeLax.Unmarshal(data, into)
	}

	err := modes.Decode.Unmarshal(data, into)
	// TODO: UnknownFieldError is ambiguous. It only provides the index of the first problematic
	// map entry encountered and does not indicate which map the index refers to.
	if unknownField, ok := err.(*cbor.UnknownFieldError); ok {
		// Unlike JSON, there are no strict errors in CBOR for duplicate map keys. CBOR maps
		// with duplicate keys are considered invalid according to the spec and are rejected
		// entirely.
		return runtime.NewStrictDecodingError([]error{unknownField}), modes.DecodeLax.Unmarshal(data, into)
	}
	return nil, err
}

func (s *serializer) Decode(data []byte, gvk *schema.GroupVersionKind, into runtime.Object) (runtime.Object, *schema.GroupVersionKind, error) {
	actual, err := s.metaFactory.Interpret(data)
	if err != nil {
		return nil, nil, err
	}

	if gvk != nil {
		*actual = gvkWithDefaults(*actual, *gvk)
	}

	if into != nil {
		// TODO: The GVK extracted from the initial pass over the input is only used in for
		// the "into Unstructured" case if the full decode fails. Consider moving this block
		// to the top.
		if _, ok := into.(runtime.Unstructured); ok {
			if _, err := s.unmarshal(data, into); err != nil {
				// Strict mode is irrelevant here because decoding into a map cannot
				// produce unknown field errors. Duplicate map key errors are
				// considered normal decode errors.
				return nil, actual, err
			}

			*actual = into.GetObjectKind().GroupVersionKind()
			if len(actual.Kind) == 0 {
				return nil, actual, runtime.NewMissingKindErr(diagnose(data))
			}
			if len(actual.Version) == 0 {
				return nil, actual, runtime.NewMissingVersionErr(diagnose(data))
			}

			return into, actual, nil
		}

		types, _, err := s.typer.ObjectKinds(into)
		if err != nil {
			return nil, actual, err
		}
		*actual = gvkWithDefaults(*actual, types[0])
	}

	if len(actual.Kind) == 0 {
		return nil, actual, runtime.NewMissingKindErr(diagnose(data))
	}
	if len(actual.Version) == 0 {
		return nil, actual, runtime.NewMissingVersionErr(diagnose(data))
	}

	obj, err := runtime.UseOrCreateObject(s.typer, s.creater, *actual, into)
	if err != nil {
		return nil, actual, err
	}

	strict, err := s.unmarshal(data, obj)
	if err != nil {
		return nil, actual, err
	}
	return obj, actual, strict
}

func (s *serializer) RecognizesData(data []byte) (ok, unknown bool, err error) {
	// https://www.rfc-editor.org/rfc/rfc8949.html#name-self-described-cbor
	return bytes.HasPrefix(data, []byte("\xd9\xd9\xf7")), false, nil
}
