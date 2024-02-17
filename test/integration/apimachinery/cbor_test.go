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

package apimachinery

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	goruntime "runtime"

	"github.com/google/uuid"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/cbor"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/endpoints/openapi"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/options"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"k8s.io/kubernetes/test/integration/framework"
	"k8s.io/kubernetes/test/utils/ktesting"
	"k8s.io/sample-apiserver/pkg/apis/wardle"
	wardleinstall "k8s.io/sample-apiserver/pkg/apis/wardle/install"
	wardlev1alpha1 "k8s.io/sample-apiserver/pkg/apis/wardle/v1alpha1"
	sampleserver "k8s.io/sample-apiserver/pkg/apiserver"
	sampleopenapi "k8s.io/sample-apiserver/pkg/generated/openapi"
	"k8s.io/sample-apiserver/pkg/registry"
	fischerstorage "k8s.io/sample-apiserver/pkg/registry/wardle/fischer"
	flunderstorage "k8s.io/sample-apiserver/pkg/registry/wardle/flunder"
)

func TestServeCBOR1(t *testing.T) {
	ktesting.SetDefaultVerbosity(10)

	originalAllowedMediaTypes := server.AllowedMediaTypes
	server.AllowedMediaTypes = append(originalAllowedMediaTypes, "application/cbor")
	defer func() {
		server.AllowedMediaTypes = originalAllowedMediaTypes
	}()

	_, ctx := ktesting.NewTestContext(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	srv, err := StartTestServer(ctx, t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.TearDownFn)

	// The dynamic client is constructed this way for now to avoid stomping on ContentConfig.
	cfg := srv.ClientConfig
	hc, err := restclient.HTTPClientFor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg = dynamic.ConfigFor(cfg)
	cfg.AcceptContentTypes = "application/cbor"
	cfg.ContentType = "application/cbor"
	cfg.NegotiatedSerializer = basicNegotiatedSerializer{}
	cfg.GroupVersion = nil
	cfg.APIPath = "/if-you-see-this-search-for-the-break"
	rc, err := restclient.UnversionedRESTClientForConfigAndClient(cfg, hc)
	if err != nil {
		t.Fatal(err)
	}
	c := dynamic.New(rc)

	created, err := c.Resource(schema.GroupVersionResource{Group: "wardle.example.com", Version: "v1beta1", Resource: "flunders"}).Namespace("foo").Create(ctx, &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "wardle.example.com/v1beta1",
			"kind":       "Flunder",
			"metadata": map[string]interface{}{
				"name":      "test-flunder",
				"namespace": "foo",
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	<-time.After(30 * time.Second)
	t.Logf("ZZZ response object: %#v\n", created)
	t.Logf("creationtime: %v (%T)\n", created.Object["metadata"].(map[string]interface{})["creationTimestamp"], created.Object["metadata"].(map[string]interface{})["creationTimestamp"])
}

type negotiatedSerializerWithCBOR struct {
	runtime.NegotiatedSerializer
}

func (s negotiatedSerializerWithCBOR) SupportedMediaTypes() []runtime.SerializerInfo {
	return append(s.NegotiatedSerializer.SupportedMediaTypes(), runtime.SerializerInfo{
		MediaType:        "application/cbor",
		MediaTypeType:    "application",
		MediaTypeSubType: "cbor",
		Serializer:       cbor.NewSerializer(sampleserver.Scheme, sampleserver.Scheme),
		StrictSerializer: cbor.NewSerializer(sampleserver.Scheme, sampleserver.Scheme, cbor.Strict(true)),
		StreamSerializer: nil, // todo
	})
}

// Below copy-pasted from k8s.io/client-go/dynamic/scheme.go to add a SerializerInfo:

var basicScheme = runtime.NewScheme()
var parameterScheme = runtime.NewScheme()
var dynamicParameterCodec = runtime.NewParameterCodec(parameterScheme)

var versionV1 = schema.GroupVersion{Version: "v1"}

func init() {
	metav1.AddToGroupVersion(basicScheme, versionV1)
	metav1.AddToGroupVersion(parameterScheme, versionV1)
}

// basicNegotiatedSerializer is used to handle discovery and error handling serialization
type basicNegotiatedSerializer struct{}

func (s basicNegotiatedSerializer) SupportedMediaTypes() []runtime.SerializerInfo {
	return []runtime.SerializerInfo{
		{
			MediaType:        "application/json",
			MediaTypeType:    "application",
			MediaTypeSubType: "json",
			EncodesAsText:    true,
			Serializer:       json.NewSerializer(json.DefaultMetaFactory, unstructuredCreater{basicScheme}, unstructuredTyper{basicScheme}, false),
			PrettySerializer: json.NewSerializer(json.DefaultMetaFactory, unstructuredCreater{basicScheme}, unstructuredTyper{basicScheme}, true),
			StreamSerializer: &runtime.StreamSerializerInfo{
				EncodesAsText: true,
				Serializer:    json.NewSerializer(json.DefaultMetaFactory, basicScheme, basicScheme, false),
				Framer:        json.Framer,
			},
		},
		{
			MediaType:        "application/yaml",
			MediaTypeType:    "application",
			MediaTypeSubType: "yaml",
			EncodesAsText:    true,
			Serializer:       json.NewSerializer(json.DefaultMetaFactory, unstructuredCreater{basicScheme}, unstructuredTyper{basicScheme}, false),
			PrettySerializer: json.NewSerializer(json.DefaultMetaFactory, unstructuredCreater{basicScheme}, unstructuredTyper{basicScheme}, true),
			StreamSerializer: &runtime.StreamSerializerInfo{
				EncodesAsText: true,
				Serializer:    json.NewSerializer(json.DefaultMetaFactory, basicScheme, basicScheme, false),
				Framer:        json.Framer,
			},
		},
		{
			MediaType:        "application/cbor",
			MediaTypeType:    "application",
			MediaTypeSubType: "cbor",
			Serializer:       cbor.NewSerializer(unstructuredCreater{basicScheme}, unstructuredTyper{basicScheme}),
			StreamSerializer: nil, // TODO: Streaming not implemented yet
		},
	}
}

func (s basicNegotiatedSerializer) EncoderForVersion(encoder runtime.Encoder, gv runtime.GroupVersioner) runtime.Encoder {
	return runtime.WithVersionEncoder{
		Version:     gv,
		Encoder:     encoder,
		ObjectTyper: permissiveTyper{basicScheme},
	}
}

func (s basicNegotiatedSerializer) DecoderToVersion(decoder runtime.Decoder, gv runtime.GroupVersioner) runtime.Decoder {
	return decoder
}

type unstructuredCreater struct {
	nested runtime.ObjectCreater
}

func (c unstructuredCreater) New(kind schema.GroupVersionKind) (runtime.Object, error) {
	out, err := c.nested.New(kind)
	if err == nil {
		return out, nil
	}
	out = &unstructured.Unstructured{}
	out.GetObjectKind().SetGroupVersionKind(kind)
	return out, nil
}

type unstructuredTyper struct {
	nested runtime.ObjectTyper
}

func (t unstructuredTyper) ObjectKinds(obj runtime.Object) ([]schema.GroupVersionKind, bool, error) {
	kinds, unversioned, err := t.nested.ObjectKinds(obj)
	if err == nil {
		return kinds, unversioned, nil
	}
	if _, ok := obj.(runtime.Unstructured); ok && !obj.GetObjectKind().GroupVersionKind().Empty() {
		return []schema.GroupVersionKind{obj.GetObjectKind().GroupVersionKind()}, false, nil
	}
	return nil, false, err
}

func (t unstructuredTyper) Recognizes(gvk schema.GroupVersionKind) bool {
	return true
}

// The dynamic client formerly hardcoded object marshaling and unmarshaling and would allow
// Unstructured objects that were missing apiVersion and/or kind in requests.
type permissiveTyper struct {
	nested runtime.ObjectTyper
}

func (t permissiveTyper) ObjectKinds(obj runtime.Object) ([]schema.GroupVersionKind, bool, error) {
	kinds, unversioned, err := t.nested.ObjectKinds(obj)
	if err == nil {
		return kinds, unversioned, nil
	}
	if _, ok := obj.(runtime.Unstructured); ok {
		return []schema.GroupVersionKind{obj.GetObjectKind().GroupVersionKind()}, false, nil
	}
	return nil, false, err
}

func (t permissiveTyper) Recognizes(gvk schema.GroupVersionKind) bool {
	return true
}

///////

// TearDownFunc is to be called to tear down a test server.
type TearDownFunc func()

// TestServer return values supplied by kube-test-ApiServer
type TestServer struct {
	ClientConfig    *restclient.Config          // Rest client config
	ServerOpts      *options.RecommendedOptions // ServerOpts
	TearDownFn      TearDownFunc                // TearDown function
	TmpDir          string                      // Temp Dir used, by the apiserver
	CompletedConfig server.CompletedConfig
}

// Logger allows t.Testing and b.Testing to be passed to StartTestServer and StartTestServerOrDie
type Logger interface {
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
	Logf(format string, args ...interface{})
}

// StartTestServer starts a apiextensions-apiserver. A rest client config and a tear-down func,
// and location of the tmpdir are returned.
//
// Note: we return a tear-down func instead of a stop channel because the later will leak temporary
// files that because Golang testing's call to os.Exit will not give a stop channel go routine
// enough time to remove temporary files.
func StartTestServer(ctx context.Context, t Logger) (result TestServer, err error) {
	scheme := runtime.NewScheme()
	wardleinstall.Install(scheme)
	metav1.AddToGroupVersion(scheme, schema.GroupVersion{Version: "v1"})
	scheme.AddUnversionedTypes(schema.GroupVersion{Group: "", Version: "v1"},
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{})
	codecFactory := serializer.NewCodecFactory(scheme)

	// create kubeconfig which will not actually be used. But authz/authn needs it to startup.
	fakeKubeConfig, err := os.CreateTemp("", "kubeconfig")
	if err != nil {
		return result, err
	}
	fakeKubeConfig.WriteString(`
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: http://127.1.2.3:12345
  name: integration
contexts:
- context:
    cluster: integration
    user: test
  name: default-context
current-context: default-context
users:
- name: test
  user:
    password: test
    username: test
`)
	fakeKubeConfig.Close()

	stopCh := make(chan struct{})
	var errCh chan error
	tearDown := func() {
		// Closing stopCh is stopping apiextensions apiserver and its
		// delegates, which itself is cleaning up after itself,
		// including shutting down its storage layer.
		close(stopCh)

		// If the apiextensions apiserver was started, let's wait for
		// it to shutdown clearly.
		if errCh != nil {
			err, ok := <-errCh
			if ok && err != nil {
				klog.Errorf("Failed to shutdown test server clearly: %v", err)
			}
		}

		if len(result.TmpDir) != 0 {
			os.RemoveAll(result.TmpDir)
		}
	}
	defer func() {
		if result.TearDownFn == nil {
			tearDown()
		}
	}()

	result.TmpDir, err = os.MkdirTemp("", "apiextensions-apiserver")
	if err != nil {
		return result, fmt.Errorf("failed to create temp dir: %v", err)
	}

	fs := pflag.NewFlagSet("test", pflag.PanicOnError)

	s := options.NewRecommendedOptions("/registry/wardle.example.com", codecFactory.CodecForVersions(cbor.NewSerializer(scheme, scheme), cbor.NewSerializer(scheme, scheme), wardlev1alpha1.SchemeGroupVersion, wardlev1alpha1.SchemeGroupVersion))
	s.AddFlags(fs)

	s.SecureServing.Listener, s.SecureServing.BindPort, err = createLocalhostListenerOnFreePort()
	if err != nil {
		return result, fmt.Errorf("failed to create listener: %v", err)
	}
	s.SecureServing.ServerCert.CertDirectory = result.TmpDir
	s.SecureServing.ExternalAddress = s.SecureServing.Listener.Addr().(*net.TCPAddr).IP // use listener addr although it is a loopback device

	pkgPath, err := pkgPath(t)
	if err != nil {
		return result, err
	}
	s.SecureServing.ServerCert.FixtureDirectory = filepath.Join(pkgPath, "..", "..", "..", "staging", "src", "k8s.io", "apiextensions-apiserver", "pkg", "cmd", "server", "testing", "testdata")
	if err := fs.Parse([]string{
		"--authentication-kubeconfig", fakeKubeConfig.Name(),
		"--authorization-kubeconfig", fakeKubeConfig.Name(),
		"--kubeconfig", fakeKubeConfig.Name(),
		"--authentication-skip-lookup",
		"--etcd-servers", framework.GetEtcdURL(),
		"--etcd-prefix", uuid.New().String(),
		"--enable-priority-and-fairness=false",
		"--disable-admission-plugins", "NamespaceLifecycle,MutatingAdmissionWebhook,ValidatingAdmissionWebhook",
	}); err != nil {
		return result, err
	}

	if err := s.Validate(); len(err) > 0 {
		return result, fmt.Errorf("failed to validate options: %v", err)
	}

	t.Logf("Starting apiserver on port %d...", s.SecureServing.BindPort)

	config := server.NewRecommendedConfig(serializer.CodecFactory{}) //todo
	err = s.ApplyTo(config)
	if err != nil {
		return result, fmt.Errorf("failed to create config from options: %v", err)
	}

	config.OpenAPIV3Config = server.DefaultOpenAPIV3Config(sampleopenapi.GetOpenAPIDefinitions, openapi.NewDefinitionNamer(scheme))
	config.Serializer = negotiatedSerializerWithCBOR{codecFactory}

	completedConfig := config.Complete()
	srv, err := completedConfig.New("test-server", server.NewEmptyDelegate())
	if err != nil {
		return result, fmt.Errorf("failed to create server: %v", err)
	}

	apiGroupInfo := server.APIGroupInfo{
		PrioritizedVersions:          scheme.PrioritizedVersionsForGroup(wardle.GroupName),
		VersionedResourcesStorageMap: map[string]map[string]rest.Storage{},
		// TODO unhardcode this.  It was hardcoded before, but we need to re-evaluate
		OptionsExternalVersion: &schema.GroupVersion{Version: "v1"},
		Scheme:                 scheme,
		ParameterCodec:         metav1.ParameterCodec,
		NegotiatedSerializer:   config.Serializer,
	}

	v1alpha1storage := map[string]rest.Storage{}
	v1alpha1storage["flunders"] = registry.RESTInPeace(flunderstorage.NewREST(scheme, completedConfig.RESTOptionsGetter))
	v1alpha1storage["fischers"] = registry.RESTInPeace(fischerstorage.NewREST(scheme, completedConfig.RESTOptionsGetter))
	apiGroupInfo.VersionedResourcesStorageMap["v1alpha1"] = v1alpha1storage

	v1beta1storage := map[string]rest.Storage{}
	v1beta1storage["flunders"] = registry.RESTInPeace(flunderstorage.NewREST(scheme, completedConfig.RESTOptionsGetter))
	apiGroupInfo.VersionedResourcesStorageMap["v1beta1"] = v1beta1storage

	if err := srv.InstallAPIGroup(&apiGroupInfo); err != nil {
		return result, err
	}

	errCh = make(chan error)
	go func(stopCh <-chan struct{}) {
		defer close(errCh)

		if err := srv.PrepareRun().Run(stopCh); err != nil {
			errCh <- err
		}
	}(stopCh)

	t.Logf("Waiting for /healthz to be ok...")

	client, err := kubernetes.NewForConfig(srv.LoopbackClientConfig)
	if err != nil {
		return result, fmt.Errorf("failed to create a client: %v", err)
	}
	err = wait.Poll(100*time.Millisecond, time.Minute, func() (bool, error) {
		select {
		case err := <-errCh:
			return false, err
		default:
		}

		result := client.CoreV1().RESTClient().Get().AbsPath("/healthz").Do(context.TODO())
		status := 0
		result.StatusCode(&status)
		if status == 200 {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return result, fmt.Errorf("failed to wait for /healthz to return ok: %v", err)
	}

	// from here the caller must call tearDown
	result.ClientConfig = srv.LoopbackClientConfig
	result.ServerOpts = s
	result.TearDownFn = tearDown
	result.CompletedConfig = completedConfig

	return result, nil
}

func createLocalhostListenerOnFreePort() (net.Listener, int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}

	// get port
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		ln.Close()
		return nil, 0, fmt.Errorf("invalid listen address: %q", ln.Addr().String())
	}

	return ln, tcpAddr.Port, nil
}

// pkgPath returns the absolute file path to this package's directory. With go
// test, we can just look at the runtime call stack. However, bazel compiles go
// binaries with the -trimpath option so the simple approach fails however we
// can consult environment variables to derive the path.
//
// The approach taken here works for both go test and bazel on the assumption
// that if and only if trimpath is passed, we are running under bazel.
func pkgPath(t Logger) (string, error) {
	_, thisFile, _, ok := goruntime.Caller(0)
	if !ok {
		return "", fmt.Errorf("failed to get current file")
	}

	pkgPath := filepath.Dir(thisFile)

	// If we find bazel env variables, then -trimpath was passed so we need to
	// construct the path from the environment.
	if testSrcdir, testWorkspace := os.Getenv("TEST_SRCDIR"), os.Getenv("TEST_WORKSPACE"); testSrcdir != "" && testWorkspace != "" {
		t.Logf("Detected bazel env varaiables: TEST_SRCDIR=%q TEST_WORKSPACE=%q", testSrcdir, testWorkspace)
		pkgPath = filepath.Join(testSrcdir, testWorkspace, pkgPath)
	}

	// If the path is still not absolute, something other than bazel compiled
	// with -trimpath.
	if !filepath.IsAbs(pkgPath) {
		return "", fmt.Errorf("can't construct an absolute path from %q", pkgPath)
	}

	t.Logf("Resolved testserver package path to: %q", pkgPath)

	return pkgPath, nil
}
