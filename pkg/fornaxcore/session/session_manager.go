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
	"centaurusinfra.io/fornax-serverless/pkg/fornaxcore/grpc/nodeagent"
	ie "centaurusinfra.io/fornax-serverless/pkg/fornaxcore/internal"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

var _ ie.SessionManagerInterface = &sessionManager{}

type sessionManager struct {
	ctx             context.Context
	nodeAgentClient nodeagent.NodeAgentClient
	podManager      ie.PodManagerInterface
	watchers        []chan<- interface{}
}

func NewSessionManager(ctx context.Context, nodeAgentProxy nodeagent.NodeAgentClient, podManager ie.PodManagerInterface) *sessionManager {
	mgr := &sessionManager{
		ctx:             ctx,
		nodeAgentClient: nodeAgentProxy,
		podManager:      podManager,
	}
	return mgr
}

func (sm *sessionManager) Watch(receiver chan<- interface{}) {
	sm.watchers = append(sm.watchers, receiver)
}

// forward to who are interested in session event
func (sm *sessionManager) UpdateSessionStatusFromNode(nodeId string, pod *v1.Pod, sessions []*fornaxv1.ApplicationSession) {
	for _, session := range sessions {
		for _, v := range sm.watchers {
			v <- &ie.SessionEvent{
				NodeId:  nodeId,
				Pod:     pod,
				Session: session,
				Type:    ie.SessionEventTypeUpdate,
			}
		}
	}
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

	apiServerClient := util.GetFornaxCoreApiClient()
	var updateErr error
	updatedSession := session.DeepCopy()
	updatedSession.Status = *newStatus
	client := apiServerClient.CoreV1().ApplicationSessions(session.Namespace)
	for i := 0; i <= 3; i++ {
		updatedSession, updateErr = client.UpdateStatus(context.TODO(), updatedSession, metav1.UpdateOptions{})
		if updateErr == nil {
			break
		} else {
			updatedSession = session.DeepCopy()
			updatedSession.Status = *newStatus
		}
	}
	if updateErr != nil {
		klog.ErrorS(updateErr, "Failed to update session status", "sess", session)
		return updateErr
	}

	return nil
}

func (sm *sessionManager) UpdateSessionFinalizer(session *fornaxv1.ApplicationSession) error {
	apiServerClient := util.GetFornaxCoreApiClient()
	client := apiServerClient.CoreV1().ApplicationSessions(session.Namespace)
	updatedSession := session.DeepCopy()
	if util.SessionIsOpen(updatedSession) {
		util.AddFinalizer(&updatedSession.ObjectMeta, fornaxv1.FinalizerOpenSession)
	} else {
		util.RemoveFinalizer(&updatedSession.ObjectMeta, fornaxv1.FinalizerOpenSession)
	}

	if len(session.Finalizers) != len(updatedSession.Finalizers) {
		for i := 0; i <= 3; i++ {
			_, err := client.Update(context.TODO(), updatedSession, metav1.UpdateOptions{})
			if err == nil {
				break
			}
		}
	}

	return nil
}
