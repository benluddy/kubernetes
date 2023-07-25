package testing

import (
	"fmt"
	"io"
	"reflect"
	"sync"

	"github.com/ugorji/go/codec"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ runtime.Serializer = &CBORSerializer{}

type CBORSerializer struct {
	// encoders and decoders are not safe for concurrent use
	encoderPool sync.Pool
	decoderPool sync.Pool
}

func NewCBORSerializer() *CBORSerializer {
	hnd := codec.CborHandle{
		BasicHandle: codec.BasicHandle{
			DecodeOptions: codec.DecodeOptions{
				MapType:       reflect.TypeOf(map[string]interface{}(nil)),
				SignedInteger: true,
				InternString:  true,
			},
			EncodeOptions: codec.EncodeOptions{},
		},
	}
	return &CBORSerializer{
		encoderPool: sync.Pool{
			New: func() any {
				return codec.NewEncoder(nil, &hnd)
			},
		},
		decoderPool: sync.Pool{
			New: func() any {
				return codec.NewDecoder(nil, &hnd)
			},
		},
	}
}

func (*CBORSerializer) Identifier() runtime.Identifier {
	return runtime.Identifier("cbor")
}

func (s *CBORSerializer) Encode(obj runtime.Object, w io.Writer) error {
	// prefix with magic number
	u, ok := obj.(runtime.Unstructured)
	if !ok {
		return fmt.Errorf("object of type %T does not implement runtime.Unstructured", obj)
	}

	enc := s.encoderPool.Get().(*codec.Encoder)
	enc.Reset(w)
	defer s.encoderPool.Put(enc)

	return enc.Encode(u.UnstructuredContent())
}

func (s *CBORSerializer) Decode(data []byte, defaults *schema.GroupVersionKind, into runtime.Object) (runtime.Object, *schema.GroupVersionKind, error) {
	var u unstructured.Unstructured

	dec := s.decoderPool.Get().(*codec.Decoder)
	dec.ResetBytes(data)
	defer s.decoderPool.Put(dec)

	err := dec.Decode(&u.Object)
	if err != nil {
		return nil, nil, err
	}
	gvk := u.GroupVersionKind()
	return &u, &gvk, nil
}
