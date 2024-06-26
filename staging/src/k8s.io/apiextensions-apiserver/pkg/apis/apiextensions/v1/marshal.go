/*
Copyright 2019 The Kubernetes Authors.

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

package v1

import (
	"bytes"
	"errors"

	cbor "k8s.io/apimachinery/pkg/runtime/serializer/cbor/direct"
	"k8s.io/apimachinery/pkg/util/json"
)

var jsTrue = []byte("true")
var jsFalse = []byte("false")

func (s JSONSchemaPropsOrBool) MarshalJSON() ([]byte, error) {
	if s.Schema != nil {
		return json.Marshal(s.Schema)
	}

	if s.Schema == nil && !s.Allows {
		return jsFalse, nil
	}
	return jsTrue, nil
}

func (s *JSONSchemaPropsOrBool) UnmarshalJSON(data []byte) error {
	var nw JSONSchemaPropsOrBool
	switch {
	case len(data) == 0:
	case data[0] == '{':
		var sch JSONSchemaProps
		if err := json.Unmarshal(data, &sch); err != nil {
			return err
		}
		nw.Allows = true
		nw.Schema = &sch
	case len(data) == 4 && string(data) == "true":
		nw.Allows = true
	case len(data) == 5 && string(data) == "false":
		nw.Allows = false
	default:
		return errors.New("boolean or JSON schema expected")
	}
	*s = nw
	return nil
}

func (s JSONSchemaPropsOrBool) MarshalCBOR() ([]byte, error) {
	if s.Schema != nil {
		return cbor.Marshal(s.Schema)
	}
	return cbor.Marshal(s.Allows)
}

func (s *JSONSchemaPropsOrBool) UnmarshalCBOR(data []byte) error {
	var b bool
	if err := cbor.Unmarshal(data, &b); err == nil {
		*s = JSONSchemaPropsOrBool{Allows: b}
		return nil
	}
	var p JSONSchemaProps
	if err := cbor.Unmarshal(data, &p); err != nil {
		return err
	}
	*s = JSONSchemaPropsOrBool{Allows: true, Schema: &p}
	return nil
}

func (s JSONSchemaPropsOrStringArray) MarshalJSON() ([]byte, error) {
	if len(s.Property) > 0 {
		return json.Marshal(s.Property)
	}
	if s.Schema != nil {
		return json.Marshal(s.Schema)
	}
	return []byte("null"), nil
}

func (s *JSONSchemaPropsOrStringArray) UnmarshalJSON(data []byte) error {
	var first byte
	if len(data) > 1 {
		first = data[0]
	}
	var nw JSONSchemaPropsOrStringArray
	if first == '{' {
		var sch JSONSchemaProps
		if err := json.Unmarshal(data, &sch); err != nil {
			return err
		}
		nw.Schema = &sch
	}
	if first == '[' {
		if err := json.Unmarshal(data, &nw.Property); err != nil {
			return err
		}
	}
	*s = nw
	return nil
}

func (s JSONSchemaPropsOrStringArray) MarshalCBOR() ([]byte, error) {
	if len(s.Property) > 0 {
		return cbor.Marshal(s.Property)
	}
	if s.Schema != nil {
		return cbor.Marshal(s.Schema)
	}
	return cbor.Marshal(nil)
}

func (s *JSONSchemaPropsOrStringArray) UnmarshalCBOR(data []byte) error {
	var a []string
	if err := cbor.Unmarshal(data, &a); err == nil {
		*s = JSONSchemaPropsOrStringArray{Property: a}
		return nil
	}
	var p JSONSchemaProps
	if err := cbor.Unmarshal(data, &p); err != nil {
		return err
	}
	*s = JSONSchemaPropsOrStringArray{Schema: &p}
	return nil
}

func (s JSONSchemaPropsOrArray) MarshalJSON() ([]byte, error) {
	if len(s.JSONSchemas) > 0 {
		return json.Marshal(s.JSONSchemas)
	}
	return json.Marshal(s.Schema)
}

func (s *JSONSchemaPropsOrArray) UnmarshalJSON(data []byte) error {
	var nw JSONSchemaPropsOrArray
	var first byte
	if len(data) > 1 {
		first = data[0]
	}
	if first == '{' {
		var sch JSONSchemaProps
		if err := json.Unmarshal(data, &sch); err != nil {
			return err
		}
		nw.Schema = &sch
	}
	if first == '[' {
		if err := json.Unmarshal(data, &nw.JSONSchemas); err != nil {
			return err
		}
	}
	*s = nw
	return nil
}

func (s JSONSchemaPropsOrArray) MarshalCBOR() ([]byte, error) {
	if len(s.JSONSchemas) > 0 {
		return cbor.Marshal(s.JSONSchemas)
	}
	return cbor.Marshal(s.Schema)
}

func (s *JSONSchemaPropsOrArray) UnmarshalCBOR(data []byte) error {
	var p JSONSchemaProps
	if err := cbor.Unmarshal(data, &p); err == nil {
		*s = JSONSchemaPropsOrArray{Schema: &p}
		return nil
	}
	var a []JSONSchemaProps
	if err := cbor.Unmarshal(data, &a); err != nil {
		return err
	}
	*s = JSONSchemaPropsOrArray{JSONSchemas: a}
	return nil
}

func (s JSON) MarshalJSON() ([]byte, error) {
	if len(s.Raw) > 0 {
		return s.Raw, nil
	}
	return []byte("null"), nil

}

func (s *JSON) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && !bytes.Equal(data, nullLiteral) {
		s.Raw = data
	}
	return nil
}

func (s JSON) MarshalCBOR() ([]byte, error) {
	if len(s.Raw) == 0 {
		return cbor.Marshal(nil)
	}
	var u any
	if err := json.Unmarshal(s.Raw, &u); err != nil {
		return nil, err
	}
	return cbor.Marshal(u)
}

var cborNull = []byte{0xf6}

func (s *JSON) UnmarshalCBOR(data []byte) error {
	if bytes.Equal(data, cborNull) {
		return nil
	}
	var u any
	if err := cbor.Unmarshal(data, &u); err != nil {
		return err
	}
	raw, err := json.Marshal(u)
	if err != nil {
		return err
	}
	s.Raw = raw
	return nil
}
