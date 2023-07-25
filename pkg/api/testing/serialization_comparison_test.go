package testing

import (
	"bytes"
	"fmt"
	"testing"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/protobuf"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	corev1 "k8s.io/kubernetes/pkg/apis/core/v1"
	"k8s.io/kubernetes/pkg/generated/openapi"
)

func BenchmarkSerialization(b *testing.B) {
	pods := benchmarkItems(b)
	width := len(pods)
	us := make([]runtime.Object, width)
	for i := range pods {
		content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&pods[i])
		if err != nil {
			b.Fatal(err)
		}
		us[i] = &unstructured.Unstructured{Object: content}
	}
	podobjs := make([]runtime.Object, width)
	for i := range pods {
		podobjs[i] = &pods[i]
	}

	checkRoundTrippable := func(b *testing.B, s runtime.Serializer, src runtime.Object) {
		var bb bytes.Buffer
		if err := s.Encode(src, &bb); err != nil {
			b.Fatal(err)
		}
		dst, _, err := s.Decode(bb.Bytes(), nil, nil)
		if err != nil {
			b.Fatal(err)
		}

		if !apiequality.Semantic.DeepEqual(src, dst) {
			//b.Error(cmp.Diff(src, dst))
		}
	}

	for _, tc := range []struct {
		s      runtime.Serializer
		corpus []runtime.Object
	}{
		{
			s:      NewCBORSerializer(),
			corpus: us,
		},
		{
			s: func() runtime.Serializer {
				codec, err := NewAvroCodecFromOpenAPIV3("k8s.io/api/core/v1.Pod", openapi.GetOpenAPIDefinitions)
				utilruntime.Must(err)
				return NewAvroSerializer(codec)
			}(),
			corpus: us,
		},
		{
			s:      unstructured.UnstructuredJSONScheme,
			corpus: us,
		},
		{
			s: func() runtime.Serializer {
				scheme := runtime.NewScheme()
				utilruntime.Must(corev1.AddToScheme(scheme))
				return protobuf.NewSerializer(scheme, scheme)
			}(),
			corpus: podobjs,
		},
	} {
		b.Run(fmt.Sprintf("%s/encode/%T", tc.s.Identifier(), tc.corpus[0]), func(b *testing.B) {
			for _, obj := range tc.corpus {
				checkRoundTrippable(b, tc.s, obj)
			}

			var buf bytes.Buffer
			sz := 0

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buf.Reset()
				err := tc.s.Encode(tc.corpus[i%len(tc.corpus)], &buf)
				if err != nil {
					b.Fatal(err)
				}
				sz += buf.Len()
			}
			b.ReportMetric(float64(sz/b.N), "B/object")
		})

		b.Run(fmt.Sprintf("%s/decode/%T", tc.s.Identifier(), tc.corpus[0]), func(b *testing.B) {
			for _, obj := range tc.corpus {
				checkRoundTrippable(b, tc.s, obj)
			}

			encoded := make([][]byte, len(tc.corpus))
			for i := range encoded {
				var buf bytes.Buffer
				var err error
				if err = tc.s.Encode(tc.corpus[i%len(tc.corpus)], &buf); err != nil {
					b.Fatal(err)
				}
				encoded[i] = buf.Bytes()
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := runtime.Decode(tc.s, encoded[i%len(tc.corpus)]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
