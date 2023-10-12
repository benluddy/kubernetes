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

package e2enode

import (
	"context"
	"os"
	"path/filepath"
	"reflect"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeletconfig "k8s.io/kubernetes/pkg/kubelet/apis/config"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e_node/services"
)

var _ = SIGDescribe("Kubelet Config [NodeFeature:KubeletConfigDropInDir]", func() {
	f := framework.NewDefaultFramework("kubelet-config-drop-in-dir-test")
	ginkgo.It("should merge kubelet configs correctly", func(ctx context.Context) {
		// Get the initial kubelet configuration
		initialConfig, err := getCurrentKubeletConfig(ctx)
		framework.ExpectNoError(err)

		ginkgo.By("Stopping the kubelet")
		restartKubelet := stopKubelet()

		// wait until the kubelet health check will fail
		gomega.Eventually(ctx, func() bool {
			return kubeletHealthCheck(kubeletHealthCheckURL)
		}, f.Timeouts.PodStart, f.Timeouts.Poll).Should(gomega.BeFalse())

		configDir := framework.TestContext.KubeletConfigDropinDir
		defer os.RemoveAll(configDir)

		err = services.WriteKubeletConfigFile(&kubeletconfig.KubeletConfiguration{
			TypeMeta: v1.TypeMeta{
				Kind:       "KubeletConfiguration",
				APIVersion: "kubelet.config.k8s.io/v1beta1",
			},
			Port:         int32(9090),
			ReadOnlyPort: int32(10255),
			SystemReserved: map[string]string{
				"memory": "1Gi",
			},
		}, filepath.Join(configDir, "10-kubelet.conf"))
		framework.ExpectNoError(err)

		err = services.WriteKubeletConfigFile(&kubeletconfig.KubeletConfiguration{
			TypeMeta: v1.TypeMeta{
				Kind:       "KubeletConfiguration",
				APIVersion: "kubelet.config.k8s.io/v1beta1",
			},
			Port:         int32(8080),
			ReadOnlyPort: int32(10257),
			SystemReserved: map[string]string{
				"memory": "2Gi",
			},
			ClusterDNS: []string{
				"192.168.1.1",
				"192.168.1.5",
				"192.168.1.8",
			},
		}, filepath.Join(configDir, "20-kubelet.conf"))
		framework.ExpectNoError(err)

		ginkgo.By("Restarting the kubelet")
		restartKubelet()
		// wait until the kubelet health check will succeed
		gomega.Eventually(ctx, func() bool {
			return kubeletHealthCheck(kubeletHealthCheckURL)
		}, f.Timeouts.PodStart, f.Timeouts.Poll).Should(gomega.BeTrue())

		mergedConfig, err := getCurrentKubeletConfig(ctx)
		framework.ExpectNoError(err)

		// Replace specific fields in the initial configuration with expectedConfig values
		initialConfig.Port = int32(8080)
		initialConfig.ReadOnlyPort = int32(10257)
		initialConfig.SystemReserved = map[string]string{
			"memory": "2Gi",
		}
		initialConfig.ClusterDNS = []string{"192.168.1.1", "192.168.1.5", "192.168.1.8"}
		// Compare the expected config with the merged config
		gomega.Expect(reflect.DeepEqual(initialConfig, mergedConfig)).To(gomega.BeTrue(), "Merged kubelet config does not match the expected configuration.")
	})
})
