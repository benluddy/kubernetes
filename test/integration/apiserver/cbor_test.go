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

package apiserver

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/cbor"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/kubernetes"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	"k8s.io/kubernetes/test/integration/framework"
	"k8s.io/kubernetes/test/utils/ktesting"
)

func EnableCBORDuringTest(tb testing.TB, scheme *runtime.Scheme, codecs *serializer.CodecFactory) {
	tb.Helper()

	WithCBORSerializer := serializer.WithSerializer(func(scheme *runtime.Scheme) runtime.SerializerInfo {
		return runtime.SerializerInfo{
			MediaType:        runtime.ContentTypeCBOR,
			Serializer:       cbor.NewSerializer(scheme, scheme),
			StrictSerializer: cbor.NewSerializer(scheme, scheme, cbor.Strict(true)),
			StreamSerializer: &runtime.StreamSerializerInfo{
				Framer:     cbor.NewFramer(),
				Serializer: cbor.NewSerializer(scheme, scheme, cbor.Transcode(false)),
			},
		}
	})

	original := *codecs
	tb.Cleanup(func() {
		tb.Helper()
		*codecs = original
	})

	*codecs = serializer.NewCodecFactory(scheme, WithCBORSerializer)
}

func TestCBORWithGeneratedClient(t *testing.T) {
	ktesting.SetDefaultVerbosity(10) // todo

	EnableCBORDuringTest(t, legacyscheme.Scheme, &legacyscheme.Codecs)
	EnableCBORDuringTest(t, clientscheme.Scheme, &clientscheme.Codecs)

	{
		originalAllowedMediaTypes := server.AllowedMediaTypes
		defer func() {
			server.AllowedMediaTypes = originalAllowedMediaTypes
		}()
		server.AllowedMediaTypes = append(server.AllowedMediaTypes, "application/cbor")
	}

	server := kubeapiservertesting.StartTestServerOrDie(t, nil, framework.DefaultTestServerFlags(), framework.SharedEtcd())
	defer server.TearDownFn()

	const TestNamespace = "test-cbor"

	{
		clientset, err := kubernetes.NewForConfig(server.ClientConfig)
		if err != nil {
			t.Fatal(err)
		}

		defer func() {
			if err := clientset.CoreV1().Namespaces().Delete(context.TODO(), TestNamespace, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
				t.Fatal(err)
			}
		}()

		if _, err := clientset.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: TestNamespace}}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	config := rest.CopyConfig(server.ClientConfig)
	config.ContentType = "application/cbor"
	config.AcceptContentTypes = "application/cbor"
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}

	s, err := clientset.CoreV1().Secrets(TestNamespace).Create(context.TODO(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cbor",
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	w, err := clientset.CoreV1().Secrets(TestNamespace).Watch(context.TODO(), metav1.ListOptions{ResourceVersion: s.ResourceVersion, FieldSelector: "metadata.name=test-cbor"})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	if s.Annotations == nil {
		s.Annotations = map[string]string{}
	}
	s.Annotations["foo"] = "bar"

	s, err = clientset.CoreV1().Secrets(TestNamespace).Update(context.TODO(), s, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var seen bool
	timeout := time.After(5 * time.Second)
	for !seen {
		select {
		case e, ok := <-w.ResultChan():
			if !ok {
				t.Fatal("watch closed without receiving expected event")
			}

			if e.Type == watch.Error {
				t.Fatalf("watch received unexpected error event: %v", errors.FromObject(e.Object))
			}

			accessor, err := meta.Accessor(e.Object)
			if err != nil {
				t.Fatal(err)
			}

			if rv := accessor.GetResourceVersion(); rv == s.ResourceVersion {
				seen = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for event")
		}
	}

	t.Log("done")
}
