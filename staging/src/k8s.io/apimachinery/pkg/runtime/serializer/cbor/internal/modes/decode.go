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

package modes

import (
	"reflect"

	"github.com/fxamacker/cbor/v2"
)

var Decode cbor.DecMode = func() cbor.DecMode {
	decode, err := cbor.DecOptions{
		// Maps with duplicate keys are well-formed but invalid according to the CBOR spec
		// and never acceptable. Unlike the JSON serializer, inputs containing duplicate map
		// keys are rejected outright and not surfaced as a strict decoding error.
		DupMapKey: cbor.DupMapKeyEnforcedAPF,

		// For JSON parity, decoding an RFC3339 string into time.Time needs to be accepted
		// with or without tagging. If a tag number is present, it must be valid.
		TimeTag: cbor.DecTagOptional,

		// Observed depth up to 16 in fuzzed batch/v1 CronJobList. JSON implementation limit
		// is 10000.
		MaxNestedLevels: 64,

		MaxArrayElements: 1024,
		MaxMapPairs:      1024,

		// Indefinite-length sequences aren't produced by this serializer, but other
		// implementations can.
		IndefLength: cbor.IndefLengthAllowed,

		// Accept inputs that contain CBOR tags.
		TagsMd: cbor.TagsAllowed,

		// Decode type 0 (unsigned integer) as int64.
		// TODO: IntDecConvertSigned errors on overflow, JSON will try to fall back to float64.
		IntDec: cbor.IntDecConvertSigned,

		// Disable producing map[cbor.ByteString]interface{}, which is not acceptable for
		// decodes into interface{}.
		MapKeyByteString: cbor.MapKeyByteStringForbidden,

		// Error on map keys that don't map to a field in the destination struct.
		ExtraReturnErrors: cbor.ExtraDecErrorUnknownField,

		// Decode maps into concrete type map[string]interface{} when the destination is an
		// interface{}.
		DefaultMapType: reflect.TypeOf(map[string]interface{}(nil)),

		// A CBOR text string whose content is not a valid UTF-8 sequence is well-formed but
		// invalid according to the CBOR spec. Reject invalid inputs. Encoders are
		// responsible for ensuring that all text strings they produce contain valid UTF-8
		// sequences and may use the byte string major type to encode strings that have not
		// been validated.
		UTF8: cbor.UTF8RejectInvalid,
	}.DecMode()
	if err != nil {
		panic(err)
	}
	return decode
}()

// DecodeLax is derived from Decode, but does not complain about unknown fields in the input.
var DecodeLax cbor.DecMode = func() cbor.DecMode {
	opts := Decode.DecOptions()
	opts.ExtraReturnErrors = opts.ExtraReturnErrors &^ cbor.ExtraDecErrorUnknownField
	opts.DefaultMapType = reflect.TypeOf(map[string]interface{}(nil)) // DefaultMapType isn't set properly by DecOptions, set it again here until the bugfix is adopted.
	dm, err := opts.DecMode()
	if err != nil {
		panic(err)
	}
	return dm
}()
