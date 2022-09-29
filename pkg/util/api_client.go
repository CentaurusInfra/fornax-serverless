/*
Copyright 2022.

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

package util

import (
	"os"

	fornaxclient "centaurusinfra.io/fornax-serverless/pkg/client/clientset/versioned"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

func GetFornaxCoreKubeConfig() *rest.Config {
	var kubeconfig *rest.Config
	if root, err := os.Getwd(); err == nil {
		kubeconfigPath := root + "/kubeconfig"
		if kubeconfig, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath); err != nil {
			klog.ErrorS(err, "Failed to construct kube rest config")
			os.Exit(-1)
		}
	} else {
		klog.ErrorS(err, "Failed to get working dir")
		os.Exit(-1)
	}
	return kubeconfig
}
func GetFornaxCoreApiClient(kubeconfig *rest.Config) *fornaxclient.Clientset {
	apiServerClient := fornaxclient.NewForConfigOrDie(kubeconfig)
	return apiServerClient
}
