package testing

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/linkedin/goavro/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

var _ runtime.Serializer = &AvroSerializer{}

type AvroSerializer struct {
	codec   *goavro.Codec
	buffers sync.Pool
}

func NewAvroSerializer(codec *goavro.Codec) *AvroSerializer {
	return &AvroSerializer{
		codec: codec,
		buffers: sync.Pool{
			New: func() any {
				return make([]byte, 0, 256)
			},
		},
	}
}

func (*AvroSerializer) Identifier() runtime.Identifier {
	return runtime.Identifier("avro")
}

func (s *AvroSerializer) Encode(obj runtime.Object, w io.Writer) error {
	// prefix with magic number + schema fingerprint
	u, ok := obj.(runtime.Unstructured)
	if !ok {
		panic("not implemented")
	}

	buf := s.buffers.Get().([]byte)
	defer func() {
		s.buffers.Put(buf[:0])
	}()

	var err error
	buf, err = s.codec.BinaryFromNative(buf, u.UnstructuredContent())
	if err != nil {
		return err
	}

	_, err = io.Copy(w, bytes.NewReader(buf))
	return err
}

// Decode implements runtime.Serializer
func (s *AvroSerializer) Decode(data []byte, defaults *schema.GroupVersionKind, into runtime.Object) (runtime.Object, *schema.GroupVersionKind, error) {
	native, _, err := s.codec.NativeFromBinary(data)
	if err != nil {
		return nil, nil, err
	}

	return &unstructured.Unstructured{Object: native.(map[string]interface{})}, nil, nil
}

func NewAvroCodecFromOpenAPIV3(root string, getOpenAPIDefinitions common.GetOpenAPIDefinitions) (*goavro.Codec, error) {
	defs := getOpenAPIDefinitions(spec.MustCreateRef)

	seen := sets.NewString()
	unseen := []string{root}
	for len(unseen) > 0 {
		var head string
		head, unseen = unseen[0], unseen[1:]
		def, ok := defs[head]
		if !ok {
			return nil, fmt.Errorf("could not find def %q", head)
		}
		seen.Insert(head)
		for _, dep := range def.Dependencies {
			if seen.Has(dep) {
				continue
			}
			unseen = append(unseen, dep)
		}
	}
	_ = seen.List()

	def := defs[root]
	avsc, err := openapiSchemaToAvroSchema(root, &def.Schema, defs)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(avsc); err != nil {
		return nil, err
	}

	return goavro.NewCodec(buf.String())
}

// avro always encodes all fields
// simulate optional fields via union of a named fieldless record and the field type
// fieldless records take no space in the wire format but there is the overhead of a union discriminator
var absentSchema = map[string]interface{}{
	"name":   "io.k8s.avro.Absent",
	"type":   "record",
	"fields": []interface{}{},
}

// hack in enough to roundtrip v1.Pod for benchmark against generated proto
func openapiSchemaToAvroSchema(name string, oas *spec.Schema, defs map[string]common.OpenAPIDefinition) (result interface{}, err error) {
	name = strings.ReplaceAll(name, "/", ".") //todo

	if ref := oas.Ref.String(); ref != "" {
		def, ok := defs[ref]
		if ok {
			resolved, err := openapiSchemaToAvroSchema(name, &def.Schema, defs)
			if err != nil {
				return nil, err
			}
			return resolved, nil
		}
	}

	if len(oas.OneOf) > 0 {
		types := []interface{}{}
		for _, subschema := range oas.OneOf {
			subavsc, err := openapiSchemaToAvroSchema(name, &subschema, defs)
			if err != nil {
				return nil, err
			}
			types = append(types, subavsc)
		}
		return types, nil
	}

	if len(oas.Type) != 1 {
		return nil, fmt.Errorf("todo: len(type)==%d in: %s", len(oas.Type), name)
	}

	switch oas.Type[0] {
	case "string":
		return "string", nil
	case "integer":
		return "long", nil
	case "number":
		return "double", nil
	case "boolean":
		return "boolean", nil
	case "null":
		return "null", nil
	case "object":
		if addls := oas.AdditionalProperties; addls != nil && addls.Allows {
			if addls.Schema == nil {
				return nil, fmt.Errorf("addlprops without schema not implemented")
			}
			values, err := openapiSchemaToAvroSchema(name+"Value", addls.Schema, defs)
			if err != nil {
				return nil, err
			}
			return []interface{}{
				"null", // present but nil
				map[string]interface{}{
					"type":   "map",
					"name":   name,
					"values": values,
				},
			}, nil
		} else if len(oas.Properties) > 0 {
			fields := []interface{}{}
			for pname, pschema := range oas.Properties {
				if pname == "secretRef" {
					continue // hack: todo
				}
				fieldtype, err := openapiSchemaToAvroSchema(pname, &pschema, defs)
				if err != nil {
					return nil, err
				}

				field := map[string]interface{}{
					"name": pname,
				}

				var required bool
				for _, rname := range oas.Required { // this is fine
					if rname == pname {
						required = true
					}
				}

				if required {
					field["type"] = fieldtype
				} else if union, ok := fieldtype.([]interface{}); ok {
					// avoid nested union, prepend absent to union
					field["type"] = append([]interface{}{absentSchema}, union...)
					field["default"] = map[string]interface{}{}
				} else {
					field["type"] = []interface{}{absentSchema, fieldtype}
					field["default"] = map[string]interface{}{}
				}

				fields = append(fields, field)
			}
			return map[string]interface{}{
				"type":   "record",
				"name":   name,
				"fields": fields,
			}, nil
		} else {
			// no properties or additionalProperties
			return map[string]interface{}{
				"type": "map",
				"name": name,
				"values": []interface{}{
					"string",
				},
				"default": map[string]interface{}{},
			}, nil
		}
	case "array":
		if oas.Items.Len() != 1 {
			panic("todo: len(items)!=1 in " + name)
		}
		items := oas.Items.Schema
		if items == nil {
			items = &oas.Items.Schemas[0]
		}

		suboas, err := openapiSchemaToAvroSchema(name+"_items", items, defs)
		if err != nil {
			return nil, err
		}

		return []interface{}{
			"null",
			map[string]interface{}{
				"type":    "array",
				"name":    name,
				"items":   suboas,
				"default": []interface{}{},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unhandled type in %q: %v", name, oas.Type[0])
	}
}
