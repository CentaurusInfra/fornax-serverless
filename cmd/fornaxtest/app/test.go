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

package app

import (
	"context"
	"fmt"
	"os"
	"time"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxclient "centaurusinfra.io/fornax-serverless/pkg/client/clientset/versioned"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	"github.com/google/uuid"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	_ "k8s.io/component-base/logs/json/register" // for JSON log format registration
	"k8s.io/klog/v2"
)

var (
	SessionWrapperEchoServerSpec = &fornaxv1.ApplicationSpec{
		Containers: []v1.Container{{
			Name:  "echoserver",
			Image: "centaurusinfra.io/fornax-serverless/session-wrapper:v0.1.0",
			Ports: []v1.ContainerPort{{
				Name:          "echoserver",
				ContainerPort: 80,
			}},
			Env: []v1.EnvVar{{
				Name:  "SESSION_WRAPPER_OPEN_SESSION_CMD",
				Value: "/opt/bin/sessionwrapper-echoserver",
			}},
			Resources: v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					"memory": util.ResourceQuantity(50*1024*1024, v1.ResourceMemory),
					"cpu":    util.ResourceQuantity(0.5*1000, v1.ResourceMemory),
				},
				Requests: map[v1.ResourceName]resource.Quantity{
					"memory": util.ResourceQuantity(50*1024*1024, v1.ResourceMemory),
					"cpu":    util.ResourceQuantity(0.5*1000, v1.ResourceMemory),
				},
			},
		}},
		ConfigData: map[string]string{},
		ScalingPolicy: fornaxv1.ScalingPolicy{
			MinimumInstance:   20,
			MaximumInstance:   500,
			Burst:             50,
			ScalingPolicyType: "idle_session_number",
			IdleSessionNumThreshold: &fornaxv1.IdelSessionNumThreshold{
				HighWaterMark: 10,
				LowWaterMark:  3,
			},
		},
	}

	CloseGracePeriodSeconds             = uint16(10)
	SessionWrapperEchoServerSessionSpec = &fornaxv1.ApplicationSessionSpec{
		ApplicationName:               "",
		SessionData:                   "session-data",
		KillInstanceWhenSessionClosed: false,
		CloseGracePeriodSeconds:       &CloseGracePeriodSeconds,
		OpenTimeoutSeconds:            10,
	}
)

func runAppFullCycleTest(namespace, appName string, numOfSession int) {
	defer cleanupAppFullCycleTest(namespace, appName)
	apiServerClient := getApiServerClient()
	application, err := describeApplication(apiServerClient, namespace, appName)
	if err != nil {
		klog.ErrorS(err, "Failed to find application", "name", appName)
		return
	}
	if application == nil {
		application, err = createApplication(apiServerClient, namespace, appName, SessionWrapperEchoServerSpec)
		if err != nil {
			klog.ErrorS(err, "Failed to create application", "name", appName)
			return
		}
		waitForAppSetup(namespace, appName, int(SessionWrapperEchoServerSpec.ScalingPolicy.MinimumInstance))
	}

	sessionBaseName := uuid.New().String()
	sessions := []string{}
	for i := 0; i < numOfSession; i++ {
		sessName := fmt.Sprintf("%s-session-%s-%d", appName, sessionBaseName, i)
		sess, err := describeSession(apiServerClient, namespace, sessName)
		if err != nil {
			klog.ErrorS(err, "Failed to find session", "app", appName, "name", sessName)
			continue
		}
		if sess != nil {
			continue
		}
		applicationKey := util.Name(application)
		_, err = createSession(apiServerClient, namespace, sessName, applicationKey, SessionWrapperEchoServerSessionSpec)
		if err != nil {
			klog.ErrorS(err, "Failed to create session", "app", appName, "name", sessName)
			continue
		} else {
			sessions = append(sessions, sessName)
		}
	}

	waitForSessionSetup(namespace, appName, sessions)
}

func cleanupAppFullCycleTest(namespace, appName string) {
	apiServerClient := getApiServerClient()
	delTime := time.Now()
	application, _ := describeApplication(apiServerClient, namespace, appName)
	instanceNum := application.Status.TotalInstances
	deleteApplication(apiServerClient, namespace, appName)
	for {
		time.Sleep(500 * time.Millisecond)
		appl, err := describeApplication(apiServerClient, namespace, appName)
		if err == nil && appl == nil {
			fmt.Printf("Application: %s took %d micro second to teardown %d instances\n", appName, time.Now().Sub(delTime).Microseconds(), instanceNum)
			break
		}
		continue
	}
}

func waitForAppSetup(namespace, appName string, numOfInstance int) {
	apiServerClient := getApiServerClient()
	for {
		time.Sleep(500 * time.Millisecond)
		application, err := describeApplication(apiServerClient, namespace, appName)
		if err != nil {
			continue
		}

		if int(application.Status.ReadyInstances) >= numOfInstance {
			fmt.Printf("Application: %s took %d micro second to setup %d instances\n", appName, time.Now().Sub(application.CreationTimestamp.Time).Microseconds(), application.Status.ReadyInstances)
			break
		}
		continue
	}
}

func waitForSessionTearDown(namespace, appName string, sessions []string) {
	apiServerClient := getApiServerClient()
	for {
		time.Sleep(500 * time.Millisecond)
		allTeardown := true
		for _, sessName := range sessions {
			sess, err := describeSession(apiServerClient, namespace, sessName)

			if err != nil {
				allTeardown = false
				break
			}
			if sess == nil {
				continue
			}

			if !util.SessionInTerminalState(sess) {
				allTeardown = false
				break
			}
		}

		if allTeardown {
			break
		}
	}
}

func waitForSessionSetup(namespace, appName string, sessions []string) {
	apiServerClient := getApiServerClient()
	for {
		time.Sleep(500 * time.Millisecond)
		allSetup := true
		for _, sessName := range sessions {
			sess, err := describeSession(apiServerClient, namespace, sessName)
			if err != nil {
				allSetup = false
				break
			}
			if sess == nil {
				continue
			}

			switch sess.Status.SessionStatus {
			case fornaxv1.SessionStatusAvailable:
				fmt.Printf("Session: %s took %d micro second to setup\n", sessName, sess.Status.AvailableTime.Time.Sub(sess.CreationTimestamp.Time).Microseconds())
			case fornaxv1.SessionStatusTimeout:
				fmt.Printf("Session: %s timeout\n", sessName)
			case fornaxv1.SessionStatusClosed:
				fmt.Printf("Session: %s closed\n", sessName)
			default:
				allSetup = false
				break
			}
		}

		if allSetup {
			break
		}
	}
}

func runSessionFullSycleTest(namespace, appName string, numOfSession int) {
	sessions := []string{}
	defer cleanupSessionFullCycleTest(namespace, appName, sessions)
	apiServerClient := getApiServerClient()
	application, err := describeApplication(apiServerClient, namespace, appName)
	if err != nil {
		klog.ErrorS(err, "Failed to find application", "name", appName)
		return
	}
	if application == nil {
		application, err = createApplication(apiServerClient, namespace, appName, SessionWrapperEchoServerSpec)
		if err != nil {
			klog.ErrorS(err, "Failed to create application", "name", appName)
			return
		}
		waitForAppSetup(namespace, appName, int(SessionWrapperEchoServerSpec.ScalingPolicy.MinimumInstance))
	}

	sessionBaseName := uuid.New().String()
	for i := 0; i < numOfSession; i++ {
		sessName := fmt.Sprintf("%s-session-%s-%d", appName, sessionBaseName, i)
		sess, err := describeSession(apiServerClient, namespace, sessName)
		if err != nil {
			klog.ErrorS(err, "Failed to find session", "app", appName, "name", sessName)
			continue
		}
		if sess != nil {
			continue
		}
		applicationKey := util.Name(application)
		_, err = createSession(apiServerClient, namespace, sessName, applicationKey, SessionWrapperEchoServerSessionSpec)
		if err != nil {
			klog.ErrorS(err, "Failed to create session", "app", appName, "name", sessName)
			continue
		} else {
			sessions = append(sessions, sessName)
		}
	}
	waitForSessionSetup(namespace, appName, sessions)
}

func cleanupSessionFullCycleTest(namespace, appName string, sessions []string) {
	apiServerClient := getApiServerClient()
	for _, sessName := range sessions {
		deleteSession(apiServerClient, namespace, sessName)
	}
}

func createApplication(client fornaxclient.Interface, namespace, name string, appSpec *fornaxv1.ApplicationSpec) (*fornaxv1.Application, error) {
	appClient := client.CoreV1().Applications(namespace)
	application := &fornaxv1.Application{
		TypeMeta: metav1.TypeMeta{
			Kind:       fornaxv1.ApplicationKind.Kind,
			APIVersion: fornaxv1.ApplicationKind.Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:         name,
			GenerateName: name,
			Namespace:    namespace,
			Labels:       map[string]string{fornaxv1.LabelFornaxCoreApplication: name},
		},
		Spec:   *appSpec.DeepCopy(),
		Status: fornaxv1.ApplicationStatus{},
	}
	klog.InfoS("Application created", "application", application)
	return appClient.Create(context.Background(), application, metav1.CreateOptions{})
}

func createSession(client fornaxclient.Interface, namespace, name, application string, sessionSpec *fornaxv1.ApplicationSessionSpec) (*fornaxv1.ApplicationSession, error) {
	appClient := client.CoreV1().ApplicationSessions(namespace)
	spec := *sessionSpec.DeepCopy()
	spec.ApplicationName = application
	session := &fornaxv1.ApplicationSession{
		TypeMeta: metav1.TypeMeta{
			Kind:       fornaxv1.ApplicationSessionKind.Kind,
			APIVersion: fornaxv1.ApplicationSessionKind.Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:         name,
			GenerateName: name,
			Namespace:    namespace,
		},
		Spec:   spec,
		Status: fornaxv1.ApplicationSessionStatus{},
	}
	klog.InfoS("Session created", "application", application, "session", name)
	return appClient.Create(context.Background(), session, metav1.CreateOptions{})
}

func deleteApplication(client fornaxclient.Interface, namespace, name string) error {
	appClient := client.CoreV1().Applications(namespace)
	klog.InfoS("Applications deleted", "namespace", namespace, "app", name)
	return appClient.Delete(context.Background(), name, metav1.DeleteOptions{})
}

func deleteSession(client fornaxclient.Interface, namespace, name string) error {
	appClient := client.CoreV1().ApplicationSessions(namespace)
	return appClient.Delete(context.Background(), name, metav1.DeleteOptions{})
}

func describeApplication(client fornaxclient.Interface, namespace, name string) (*fornaxv1.Application, error) {
	appClient := client.CoreV1().Applications(namespace)
	apps, err := appClient.Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return apps, err
}
func describeSession(client fornaxclient.Interface, namespace, name string) (*fornaxv1.ApplicationSession, error) {
	appClient := client.CoreV1().ApplicationSessions(namespace)
	sess, err := appClient.Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return sess, err
}

func getApiServerClient() *fornaxclient.Clientset {
	if root, err := os.Getwd(); err == nil {
		kubeconfigPath := root + "/kubeconfig"
		if kubeconfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath); err != nil {
			klog.ErrorS(err, "Failed to construct kube rest config")
			os.Exit(-1)
		} else {
			return fornaxclient.NewForConfigOrDie(kubeconfig)
		}
	} else {
		klog.ErrorS(err, "Failed to get working dir")
		os.Exit(-1)
	}

	return nil
}
