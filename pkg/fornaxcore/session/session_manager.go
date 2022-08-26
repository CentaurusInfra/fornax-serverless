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

package session

import (
	"context"
	"fmt"
	"reflect"

	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	fornaxclient "centaurusinfra.io/fornax-serverless/pkg/client/clientset/versioned"
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ ie.SessionInterface = &sessionManager{}

type sessionManager struct {
	ctx             context.Context
	nodeAgentClient nodeagent.NodeAgentClient
	apiServerClient fornaxclient.Interface
	podManager      ie.PodManager
}

func NewSessionManager(ctx context.Context, nodeAgentProxy nodeagent.NodeAgentClient, podManager ie.PodManager, apiServerClient fornaxclient.Interface) *sessionManager {
	mgr := &sessionManager{
		ctx:             ctx,
		apiServerClient: apiServerClient,
		nodeAgentClient: nodeAgentProxy,
		podManager:      podManager,
	}
	return mgr
}

// treat node as authority for opened session status, session status from node could be Starting, Available, Closed,
// use status from node always, then session will be closed if deletion is requested
func (sm *sessionManager) UpdateSessionStatusFromNode(nodeId string, pod *v1.Pod, sessions []*fornaxv1.ApplicationSession) error {
	errs := []error{}
	for _, session := range sessions {
		client := sm.apiServerClient.CoreV1().ApplicationSessions(session.Namespace)
		oldCopy, err := client.Get(context.Background(), session.Name, metav1.GetOptions{})
		if err != nil {
			// session should be closed as fornax core does not have it anymore
			if apierrors.IsNotFound(err) && util.SessionIsOpen(session) {
				err = sm.CloseSession(pod, session)
			}

			errs = append(errs, err)
		}
		newStatus := session.Status.DeepCopy()
		if session.Spec.KillInstanceWhenSessionClosed && newStatus.SessionStatus == fornaxv1.SessionStatusClosed && util.PodNotTerminated(pod) {
			klog.InfoS("Terminate a pod as KillInstanceWhenSessionClosed is true", "pod", util.Name(pod), "session", util.Name(session))
			if _, err = sm.podManager.TerminatePod(pod); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		err = sm.UpdateSessionStatus(oldCopy, newStatus)
		if err != nil {
			errs = append(errs, err)
		}

	}
	if len(errs) > 0 {
		return errors.NewAggregate(errs)
	}

	return nil
}

func (sm *sessionManager) CloseSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	if nodeName, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreNode]; found {
		return sm.nodeAgentClient.CloseSession(nodeName, pod, session)
	} else {
		return fmt.Errorf("Can not find which node this pod is on, %s", util.Name(pod))
	}
}

func (sm *sessionManager) OpenSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	if nodeName, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreNode]; found {
		return sm.nodeAgentClient.OpenSession(nodeName, pod, session)
	} else {
		return fmt.Errorf("Can not find which node session is on, %s", util.Name(session))
	}
}

// updateApplicationSessionStatus attempts to update the Status of the given Application Session
func (sm *sessionManager) UpdateSessionStatus(session *fornaxv1.ApplicationSession, newStatus *fornaxv1.ApplicationSessionStatus) error {
	if reflect.DeepEqual(session.Status, *newStatus) {
		return nil
	}

	var getErr, updateErr error
	updatedSession := session.DeepCopy()
	updatedSession.Status = *newStatus
	client := sm.apiServerClient.CoreV1().ApplicationSessions(session.Namespace)
	for i := 0; i <= 3; i++ {
		updatedSession, updateErr = client.UpdateStatus(context.TODO(), updatedSession, metav1.UpdateOptions{})
		if updateErr == nil {
			break
		}
	}
	if updateErr != nil {
		return updateErr
	}

	if len(newStatus.ClientSessions) == 0 && !util.SessionIsOpen(updatedSession) {
		util.RemoveFinalizer(&updatedSession.ObjectMeta, fornaxv1.FinalizerOpenSession)
	} else {
		util.AddFinalizer(&updatedSession.ObjectMeta, fornaxv1.FinalizerOpenSession)
	}

	if len(session.Finalizers) != len(updatedSession.Finalizers) {
		for i := 0; i <= 3; i++ {
			updatedSession, updateErr = client.Update(context.TODO(), updatedSession, metav1.UpdateOptions{})
			if updateErr == nil {
				break
			}
		}
	}

	if updatedSession, getErr = client.Get(context.TODO(), updatedSession.Name, metav1.GetOptions{}); getErr != nil {
		return getErr
	} else {
		return nil
	}
}
