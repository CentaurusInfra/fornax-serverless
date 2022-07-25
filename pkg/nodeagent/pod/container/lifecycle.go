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

package container

import (
	"fmt"
	"net"
	"net/http"
	"strconv"

	"centaurusinfra.io/fornax-serverless/pkg/nodeagent/types"
	v1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	utilio "k8s.io/utils/io"
)

const (
	maxRespBodyLength = 10 * 1 << 10 // 10KB
)

func (pl *PodContainerActor) runLifecycleHandler(pod *types.FornaxPod, container *types.Container, handler *v1.LifecycleHandler) (string, error) {
	switch {
	case handler.Exec != nil:
		var msg string
		stdout, stderr, err := pl.dependencies.CRIRuntimeService.ExecCommand(container.RuntimeContainer.Id, handler.Exec.Command, 0)
		if err != nil {
			klog.ErrorS(err, "Exec lifecycle hook for Container in Pod failed",
				"execCommand", handler.Exec.Command,
				"containerName", container.ContainerSpec.Name,
				"pod", pod.Pod.GetName(),
				"errMsg", string(stderr))
			msg = string(stderr)
		} else {
			msg = string(stdout)
		}

		return string(msg), err
	case handler.HTTPGet != nil:
		msg, err := pl.runHTTPHandler(pod, container, handler)
		if err != nil {
			klog.ErrorS(err, "HTTP lifecycle hook for Container in Pod failed",
				"path", handler.HTTPGet.Path,
				"containerName", container.ContainerSpec.Name,
				"pod", pod.Pod.Name,
				"errMsg", msg)
		}
		return msg, err
	default:
		err := fmt.Errorf("unknown handler: %v", handler)
		msg := "Cannot run lifecycle handler as handler is unknown"
		klog.ErrorS(err, msg)
		return msg, err
	}
}

func (pl *PodContainerActor) runHTTPHandler(pod *types.FornaxPod, container *types.Container, handler *v1.LifecycleHandler) (string, error) {
	host := handler.HTTPGet.Host
	if len(host) == 0 {
		if len(pod.RuntimePod.IPs) == 0 {
			return "", fmt.Errorf("failed to find container ip: %v", pod.RuntimePod.IPs)
		}
		host = pod.RuntimePod.IPs[0]
	}
	var port int
	if handler.HTTPGet.Port.Type == intstr.String && len(handler.HTTPGet.Port.StrVal) == 0 {
		port = 80
	} else {
		var err error
		port, err = resolvePort(handler.HTTPGet.Port, container.ContainerSpec)
		if err != nil {
			return "", err
		}
	}
	url := fmt.Sprintf("http://%s/%s", net.JoinHostPort(host, strconv.Itoa(port)), handler.HTTPGet.Path)
	resp, err := http.Get(url)
	return getHTTPRespBody(resp), err
}

func resolvePort(portReference intstr.IntOrString, container *v1.Container) (int, error) {
	if portReference.Type == intstr.Int {
		return portReference.IntValue(), nil
	}
	portName := portReference.StrVal
	port, err := strconv.Atoi(portName)
	if err == nil {
		return port, nil
	}
	for _, portSpec := range container.Ports {
		if portSpec.Name == portName {
			return int(portSpec.ContainerPort), nil
		}
	}
	return -1, fmt.Errorf("couldn't find port: %v in %v", portReference, container)
}

func getHTTPRespBody(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	defer resp.Body.Close()
	bytes, err := utilio.ReadAtMost(resp.Body, maxRespBodyLength)
	if err == nil || err == utilio.ErrLimitReached {
		return string(bytes)
	}
	return ""
}
