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
	"centaurusinfra.io/fornax-serverless/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/errors"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SessionManager interface {
	UpdateSessionStatus(session *fornaxv1.ApplicationSession, newStatus *fornaxv1.ApplicationSessionStatus) (*fornaxv1.ApplicationSession, error)
	UpdateSessionStatusFromNode(pod *v1.Pod, sessions []*fornaxv1.ApplicationSession) error
	OpenSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error
	CloseSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error
}

var _ SessionManager = &sessionManager{}

type sessionManager struct {
	ctx             context.Context
	nodeAgentProxy  nodeagent.NodeAgentProxy
	apiServerClient fornaxclient.Interface
}

func NewSessionManager(ctx context.Context, nodeAgentProxy nodeagent.NodeAgentProxy, apiServerClient fornaxclient.Interface) *sessionManager {
	mgr := &sessionManager{
		ctx:             ctx,
		apiServerClient: apiServerClient,
		nodeAgentProxy:  nodeAgentProxy,
	}
	return mgr
}

// treat node as authority for opened session status, session status from node could be Starting, Available, Closed,
// use status from node always, then session will be closed if deletion is requested
func (sm *sessionManager) UpdateSessionStatusFromNode(pod *v1.Pod, sessions []*fornaxv1.ApplicationSession) error {
	errs := []error{}
	for _, session := range sessions {
		client := sm.apiServerClient.CoreV1().ApplicationSessions(session.Namespace)
		oldCopy, err := client.Get(context.Background(), session.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				// session should be closed as fornax core does not have it anymore
				err = sm.CloseSession(pod, session)
			}

			errs = append(errs, err)
		}
		_, err = sm.UpdateSessionStatus(oldCopy, session.Status.DeepCopy())
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
		return sm.nodeAgentProxy.CloseSession(nodeName, pod, session)
	} else {
		return fmt.Errorf("Can not find which node this pod is on, %s", util.ResourceName(pod))
	}
}

func (sm *sessionManager) OpenSession(pod *v1.Pod, session *fornaxv1.ApplicationSession) error {
	if nodeName, found := pod.GetLabels()[fornaxv1.LabelFornaxCoreNode]; found {
		return sm.nodeAgentProxy.OpenSession(nodeName, pod, session)
	} else {
		return fmt.Errorf("Can not find which node session is on, %s", util.ResourceName(session))
	}
}

// updateApplicationSessionStatus attempts to update the Status of the given Application Session and return updated Application Session
func (sm *sessionManager) UpdateSessionStatus(session *fornaxv1.ApplicationSession, newStatus *fornaxv1.ApplicationSessionStatus) (*fornaxv1.ApplicationSession, error) {
	client := sm.apiServerClient.CoreV1().ApplicationSessions(session.Namespace)

	if reflect.DeepEqual(session.Status, *newStatus) {
		return session, nil
	}

	var getErr, updateErr error
	updatedSession := session.DeepCopy()
	updatedSession.Status = *newStatus

	for i := 0; i <= 3; i++ {
		updatedSession, updateErr = client.UpdateStatus(context.TODO(), updatedSession, metav1.UpdateOptions{})
		if updateErr == nil {
			break
		}
	}
	if updateErr != nil {
		return nil, updateErr
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
		return updatedSession, getErr
	} else {
		return updatedSession, getErr
	}
}
