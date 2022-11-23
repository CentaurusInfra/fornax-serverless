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
	"sort"
	"sync/atomic"
	"time"

	"centaurusinfra.io/fornax-serverless/cmd/fornaxtest/config"
	fornaxv1 "centaurusinfra.io/fornax-serverless/pkg/apis/core/v1"
	"centaurusinfra.io/fornax-serverless/pkg/client/informers/externalversions"
	"centaurusinfra.io/fornax-serverless/pkg/util"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
)

var (
	kubeConfig = util.GetFornaxCoreKubeConfig()

	SessionWrapperEchoServerSpec = &fornaxv1.ApplicationSpec{
		Containers: []v1.Container{{
			Name:  "echoserver",
			Image: "512811/sessionwrapper:latest",
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
					"cpu":    util.ResourceQuantity(0.5*1000, v1.ResourceCPU),
				},
				Requests: map[v1.ResourceName]resource.Quantity{
					"memory": util.ResourceQuantity(50*1024*1024, v1.ResourceMemory),
					"cpu":    util.ResourceQuantity(0.01*1000, v1.ResourceCPU),
				},
			},
		}},
		UsingNodeSessionService: true,
		ConfigData:              map[string]string{},
		ScalingPolicy: fornaxv1.ScalingPolicy{
			MinimumInstance:         0,
			MaximumInstance:         500000,
			Burst:                   1,
			ScalingPolicyType:       "idle_session_number",
			IdleSessionNumThreshold: &fornaxv1.IdelSessionNumThreshold{HighWaterMark: 0, LowWaterMark: 0},
		},
	}

	CloseGracePeriodSeconds             = uint16(10)
	SessionWrapperEchoServerSessionSpec = &fornaxv1.ApplicationSessionSpec{
		ApplicationName:               "",
		SessionData:                   "session-data",
		KillInstanceWhenSessionClosed: false,
		CloseGracePeriodSeconds:       &CloseGracePeriodSeconds,
		OpenTimeoutSeconds:            5,
	}

	NoSessionWrapperEchoServerSpec = &fornaxv1.ApplicationSpec{
		Containers: []v1.Container{{
			Name:  "echoserver",
			Image: "centaurusinfra.io/fornax-serverless/echoserver:v0.1.0",
			Ports: []v1.ContainerPort{{
				Name:          "echoserver",
				ContainerPort: 80,
			}},
			Resources: v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					"memory": util.ResourceQuantity(50*1024*1024, v1.ResourceMemory),
					"cpu":    util.ResourceQuantity(0.5*1000, v1.ResourceCPU),
				},
				Requests: map[v1.ResourceName]resource.Quantity{
					"memory": util.ResourceQuantity(50*1024*1024, v1.ResourceMemory),
					"cpu":    util.ResourceQuantity(0.01*1000, v1.ResourceCPU),
				},
			},
		}},
		UsingNodeSessionService: false,
		ConfigData:              map[string]string{},
		ScalingPolicy: fornaxv1.ScalingPolicy{
			MinimumInstance:         0,
			MaximumInstance:         500000,
			Burst:                   1,
			ScalingPolicyType:       "idle_session_number",
			IdleSessionNumThreshold: &fornaxv1.IdelSessionNumThreshold{HighWaterMark: 0, LowWaterMark: 0},
		},
	}

	NodeJsHelloWorldSpec = &fornaxv1.ApplicationSpec{
		Containers: []v1.Container{{
			Name:  "nodejs",
			Image: "centaurusinfra.io/fornax-serverless/nodejs-hw:v0.1.0",
			Ports: []v1.ContainerPort{{
				Name:          "nodejs",
				ContainerPort: 8080,
			}},
			Resources: v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					"memory": util.ResourceQuantity(500*1024*1024, v1.ResourceMemory),
					"cpu":    util.ResourceQuantity(0.5*1000, v1.ResourceCPU),
				},
				Requests: map[v1.ResourceName]resource.Quantity{
					"memory": util.ResourceQuantity(100*1024*1024, v1.ResourceMemory),
					"cpu":    util.ResourceQuantity(0.01*1000, v1.ResourceCPU),
				},
			},
		}},
		UsingNodeSessionService: false,
		ConfigData:              map[string]string{},
		ScalingPolicy: fornaxv1.ScalingPolicy{
			MinimumInstance:         0,
			MaximumInstance:         500000,
			Burst:                   1,
			ScalingPolicyType:       "idle_session_number",
			IdleSessionNumThreshold: &fornaxv1.IdelSessionNumThreshold{HighWaterMark: 0, LowWaterMark: 0},
		},
	}
)

func initApplicationInformer(ctx context.Context, namespace string) {
	informerFactory := externalversions.NewSharedInformerFactoryWithOptions(
		util.GetFornaxCoreApiClient(util.GetFornaxCoreKubeConfig()), 0*time.Minute, externalversions.WithNamespace(namespace),
	)
	applicationInformer := informerFactory.Core().V1().Applications()
	applicationInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    onApplicationAddEvent,
		UpdateFunc: onApplicationUpdateEvent,
		DeleteFunc: onApplicationDeleteEvent,
	})
	informerFactory.Start(ctx.Done())
	synced := applicationInformer.Informer().HasSynced
	cache.WaitForNamedCacheSync(fornaxv1.ApplicationKind.Kind, ctx.Done(), synced)
}

func onApplicationAddEvent(obj interface{}) {
	app := obj.(*fornaxv1.Application)
	updateApplicationStatus(app, time.Now())
	atomic.AddInt32(&addevents, 1)
}

func onApplicationUpdateEvent(old, cur interface{}) {
	_ = old.(*fornaxv1.Application)
	newCopy := cur.(*fornaxv1.Application)
	updateApplicationStatus(newCopy, time.Now())
	atomic.AddInt32(&updevents, 1)
}

func onApplicationDeleteEvent(obj interface{}) {
	_ = obj.(*fornaxv1.Application)
	atomic.AddInt32(&delevents, 1)
}

func updateApplicationStatus(app *fornaxv1.Application, revTime time.Time) {
	appMapLock.Lock()
	defer appMapLock.Unlock()
	if ta, found := appMap[util.Name(app)]; found {
		ta.application = app
	}
}

func initApplicationSessionInformer(ctx context.Context, namespace string) {
	informerFactory := externalversions.NewSharedInformerFactoryWithOptions(
		util.GetFornaxCoreApiClient(util.GetFornaxCoreKubeConfig()), 0*time.Minute, externalversions.WithNamespace(namespace),
	)
	sessionInformer := informerFactory.Core().V1().ApplicationSessions()
	sessionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    onApplicationSessionAddEvent,
		UpdateFunc: onApplicationSessionUpdateEvent,
		DeleteFunc: onApplicationSessionDeleteEvent,
	})
	informerFactory.Start(ctx.Done())
	synced := sessionInformer.Informer().HasSynced
	cache.WaitForNamedCacheSync(fornaxv1.ApplicationSessionKind.Kind, ctx.Done(), synced)
}

func onApplicationSessionAddEvent(obj interface{}) {
	newCopy := obj.(*fornaxv1.ApplicationSession)
	updateSessionStatus(newCopy, time.Now())
	atomic.AddInt32(&addevents, 1)
}

// callback from Application informer when ApplicationSession is updated
// or session status is reported back from node
// if session in terminal state, remove this session from pool,
// else add new copy into pool
// do not need to sync application unless session is deleting or status changed
func onApplicationSessionUpdateEvent(old, cur interface{}) {
	_ = old.(*fornaxv1.ApplicationSession)
	newCopy := cur.(*fornaxv1.ApplicationSession)
	updateSessionStatus(newCopy, time.Now())
	atomic.AddInt32(&updevents, 1)
}

func updateSessionStatus(session *fornaxv1.ApplicationSession, revTime time.Time) {
	appSessionMapLock.Lock()
	defer appSessionMapLock.Unlock()
	tms := revTime.UnixMilli()
	if ts, found := appSessionMap[session.Name]; found {
		if ts.status == fornaxv1.SessionStatusPending &&
			(session.Status.SessionStatus == fornaxv1.SessionStatusAvailable ||
				session.Status.SessionStatus == fornaxv1.SessionStatusClosed ||
				session.Status.SessionStatus == fornaxv1.SessionStatusTimeout) {
			ts.status = session.Status.SessionStatus
			ts.watchAvailableTimeMilli = tms
			ts.internalAvailableTimeMilli = session.Status.AvailableTimeMicro / 1000
			allTestSessions = append(allTestSessions, ts)
		}

		if ts.status == fornaxv1.SessionStatusPending || ts.status == fornaxv1.SessionStatusUnspecified {
			ts.watchCreationTimeMilli = tms
		}
	}
}

func onApplicationSessionDeleteEvent(obj interface{}) {
	_ = obj.(*fornaxv1.ApplicationSession)
	atomic.AddInt32(&delevents, 1)
}

func waitForSessionSetup(namespace, appName string, sessions TestSessionArray) {
	if len(sessions) > 0 {
		for {
			time.Sleep(1 * time.Millisecond)
			allSetup := true
			for _, ts := range sessions {
				if ts.status == fornaxv1.SessionStatusPending {
					allSetup = false
					break
				}
			}

			if allSetup {
				break
			}
		}
	}
}

func runAppFullCycleTest(cycleName, namespace, appName string, testConfig config.TestConfiguration) []*TestSession {
	application, err := describeApplication(namespace, appName)
	if err != nil {
		klog.ErrorS(err, "Failed to find application", "name", appName)
		return []*TestSession{}
	}
	if application == nil {
		application = createAndWaitForApplicationSetup(namespace, appName, testConfig)
	}

	sessions := createAndWaitForSessionSetup(application, namespace, appName, cycleName, testConfig)
	cleanupAppFullCycleTest(namespace, appName, sessions)
	return sessions
}

func cleanupAppFullCycleTest(namespace, appName string, sessions []*TestSession) {
	application, _ := describeApplication(namespace, appName)
	instanceNum := application.Status.TotalInstances
	delTime := time.Now()
	deleteApplication(namespace, appName)
	for {
		time.Sleep(100 * time.Millisecond)
		appl, err := describeApplication(namespace, appName)
		if err == nil && appl == nil {
			klog.Infof("Application: %s took %d milli second to teardown %d instances\n", appName, time.Now().Sub(delTime).Milliseconds(), instanceNum)
			break
		}
		continue
	}

	for _, v := range sessions {
		deleteSession(v.session.Namespace, v.session.Name)
	}
}

func createAndWaitForApplicationSetup(namespace, appName string, testConfig config.TestConfiguration) *fornaxv1.Application {
	appSpec := SessionWrapperEchoServerSpec.DeepCopy()
	if testConfig.NoNodeSessionService {
		// appSpec = NoSessionWrapperEchoServerSpec.DeepCopy()
		appSpec = NodeJsHelloWorldSpec.DeepCopy()
	}

	appSpec.ScalingPolicy.Burst = uint32(testConfig.NumOfBurstPodsPerApp)
	appSpec.ScalingPolicy.MinimumInstance = uint32(testConfig.NumOfInitPodsPerApp)
	ta, err := createApplication(namespace, appName, appSpec)
	if err != nil {
		klog.ErrorS(err, "Failed to create application", "name", appName)
		return nil
	}
	waitForAppSetup(ta)
	return ta.application
}

type TestApplication struct {
	application        *fornaxv1.Application
	creationTimeMilli  int64
	availableTimeMilli int64
	warmUpInstances    int
}

type TestSession struct {
	session                    *fornaxv1.ApplicationSession
	apiCreationTimeMilli       int64
	watchCreationTimeMilli     int64
	internalAvailableTimeMilli int64
	watchAvailableTimeMilli    int64
	status                     fornaxv1.SessionStatus
}

type TestApplicationArray []*TestApplication

func (sn TestApplicationArray) Len() int {
	return len(sn)
}

//so, sort latency from smaller to lager value
func (sn TestApplicationArray) Less(i, j int) bool {
	return sn[i].availableTimeMilli-sn[i].creationTimeMilli < sn[j].availableTimeMilli-sn[j].creationTimeMilli
}

func (sn TestApplicationArray) Swap(i, j int) {
	sn[i], sn[j] = sn[j], sn[i]
}

type TestAppMap map[string]*TestApplication

type TestSessionMap map[string]*TestSession

type TestSessionArray []*TestSession

func (sn TestSessionArray) Len() int {
	return len(sn)
}

//so, sort latency from smaller to lager value
func (sn TestSessionArray) Less(i, j int) bool {
	return sn[i].watchAvailableTimeMilli-sn[i].apiCreationTimeMilli < sn[j].watchAvailableTimeMilli-sn[j].apiCreationTimeMilli
}

func (sn TestSessionArray) Swap(i, j int) {
	sn[i], sn[j] = sn[j], sn[i]
}

func createAndWaitForSessionSetup(application *fornaxv1.Application, namespace, appName, sessionBaseName string, testConfig config.TestConfiguration) []*TestSession {
	numOfSession := testConfig.NumOfSessionPerApp
	sessions := []*TestSession{}
	for i := 0; i < numOfSession; i++ {
		sessName := fmt.Sprintf("%s-%s-session-%d", appName, sessionBaseName, i)
		ts, err := createSession(namespace, sessName, appName, SessionWrapperEchoServerSessionSpec)
		if err == nil && ts != nil {
			sessions = append(sessions, ts)
		}
		if err != nil {
			klog.ErrorS(err, "Create session", "name", sessName)
		}
	}

	waitForSessionSetup(namespace, appName, sessions)

	return sessions
}

func waitForAppSetup(ta *TestApplication) {
	if ta.warmUpInstances > 0 {
		allTestApps = append(allTestApps, ta)
		klog.Infof("waiting for %d pods of app %s setup", ta.warmUpInstances, ta.application.Name)
		for {
			if int(ta.application.Status.IdleInstances) >= ta.warmUpInstances {
				ct := ta.creationTimeMilli
				at := time.Now().UnixMilli()
				klog.Infof("Application: %s took %d milli second to setup %d instances\n", ta.application.Name, at-ct, ta.warmUpInstances)
				ta.availableTimeMilli = at
				break
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
	}
}

func waitForSessionTearDown(namespace, appName string, sessions []*TestSession) {
	if len(sessions) > 0 {
		for {
			time.Sleep(10 * time.Millisecond)
			app, err := describeApplication(namespace, appName)
			if err != nil {
				continue
			}

			if app == nil || app.Status.AllocatedInstances == 0 {
				// all instance are release or recreated
				break
			}

		}
	}
}

func createSessionTest(cycleName, namespace, appName string, testConfig config.TestConfiguration) []*TestSession {
	application, err := describeApplication(namespace, appName)
	if err != nil {
		klog.ErrorS(err, "Failed to find application", "name", appName)
		return []*TestSession{}
	}
	if application == nil {
		application = createAndWaitForApplicationSetup(namespace, appName, testConfig)
	}

	return createAndWaitForSessionSetup(application, namespace, appName, cycleName, testConfig)
}

func runSessionFullCycleTest(cycleName, namespace, appName string, testConfig config.TestConfiguration) []*TestSession {
	application, err := describeApplication(namespace, appName)
	if err != nil {
		klog.ErrorS(err, "Failed to find application", "name", appName)
		return []*TestSession{}
	}
	if application == nil {
		application = createAndWaitForApplicationSetup(namespace, appName, testConfig)
	}

	sessions := createAndWaitForSessionSetup(application, namespace, appName, cycleName, testConfig)
	cleanupSessionFullCycleTest(namespace, appName, sessions)
	return sessions
}

func cleanupSessionFullCycleTest(namespace, appName string, sessions []*TestSession) {
	for _, sess := range sessions {
		deleteSession(namespace, sess.session.Name)
	}
	waitForSessionTearDown(namespace, appName, sessions)
}

func createApplication(namespace, name string, appSpec *fornaxv1.ApplicationSpec) (*TestApplication, error) {
	client := util.GetFornaxCoreApiClient(kubeConfig)
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
		},
		Spec:   *appSpec.DeepCopy(),
		Status: fornaxv1.ApplicationStatus{},
	}
	appMapLock.Lock()
	ta := &TestApplication{
		application:        application,
		creationTimeMilli:  time.Now().UnixMilli(),
		availableTimeMilli: 0,
		warmUpInstances:    int(appSpec.ScalingPolicy.MinimumInstance),
	}
	appMap[util.Name(application)] = ta
	appMapLock.Unlock()
	_, err := appClient.Create(context.Background(), application, metav1.CreateOptions{})
	klog.InfoS("Application created", "application", util.Name(application), "initial pods", application.Spec.ScalingPolicy.MinimumInstance)
	return ta, err
}

func createSession(namespace, name, applicationName string, sessionSpec *fornaxv1.ApplicationSessionSpec) (*TestSession, error) {
	client := util.GetFornaxCoreApiClient(kubeConfig)
	sessionClient := client.CoreV1().ApplicationSessions(namespace)
	spec := *sessionSpec.DeepCopy()
	spec.ApplicationName = applicationName
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
		Spec: spec,
	}
	ts := &TestSession{
		session: session,
		status:  fornaxv1.SessionStatusPending,
	}

	appSessionMapLock.Lock()
	appSessionMap[ts.session.Name] = ts
	appSessionMapLock.Unlock()
	ts.apiCreationTimeMilli = time.Now().UnixMilli()
	session, err := sessionClient.Create(context.Background(), session, metav1.CreateOptions{})
	if err != nil {
		appSessionMapLock.Lock()
		delete(appSessionMap, ts.session.Name)
		appSessionMapLock.Unlock()
		return nil, err
	}
	// klog.InfoS("Session created", "application", applicationName, "session", name)
	return ts, nil
}

func deleteApplication(namespace, name string) error {
	client := util.GetFornaxCoreApiClient(kubeConfig)
	appClient := client.CoreV1().Applications(namespace)
	klog.InfoS("Applications deleted", "namespace", namespace, "app", name)
	return appClient.Delete(context.Background(), name, metav1.DeleteOptions{})
}

func deleteSession(namespace, name string) error {
	client := util.GetFornaxCoreApiClient(kubeConfig)
	appClient := client.CoreV1().ApplicationSessions(namespace)
	return appClient.Delete(context.Background(), name, metav1.DeleteOptions{})
}

func describeApplication(namespace, name string) (*fornaxv1.Application, error) {
	client := util.GetFornaxCoreApiClient(kubeConfig)
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
func describeSession(namespace, name string) (*fornaxv1.ApplicationSession, error) {
	client := util.GetFornaxCoreApiClient(kubeConfig)
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

func summaryAppTestResult(apps TestApplicationArray, st, et int64) {
	if len(apps) == 0 {
		return
	}
	sort.Sort(apps)
	p99 := apps[len(apps)*99/100]
	p90 := apps[len(apps)*90/100]
	p50 := apps[len(apps)*50/100]
	klog.Infof("%d App created, Every App setup %d instances, total instances %d setup in %d ms", len(apps), apps[0].warmUpInstances, len(apps)*apps[0].warmUpInstances, et-st)
	klog.Infof("App setup time: p99 %d milli seconds", p99.availableTimeMilli-p99.creationTimeMilli)
	klog.Infof("App setup time: p90 %d milli seconds", p90.availableTimeMilli-p90.creationTimeMilli)
	klog.Infof("App setup time: p50 %d milli seconds", p50.availableTimeMilli-p50.creationTimeMilli)
}

func summarySessionTestResult(sessions TestSessionArray, st, et int64) {
	timeoutSessions := []*TestSession{}
	failedSessions := []*TestSession{}
	successSession := 0
	for _, v := range sessions {
		if v.status == fornaxv1.SessionStatusClosed {
			failedSessions = append(failedSessions, v)
		}
		if v.status == fornaxv1.SessionStatusAvailable {
			successSession += 1
		}
		if v.status == fornaxv1.SessionStatusTimeout {
			timeoutSessions = append(timeoutSessions, v)
		}
	}
	if len(sessions) == 0 {
		return
	}
	klog.Infof("--------%d sessions tested in %d ms, rate %d/s ----------", len(sessions), et-st, int64(len(sessions))*1000/(et-st))
	klog.Infof("%d success, %d failed, %d timeout", successSession, len(failedSessions), len(timeoutSessions))
	sort.Sort(sessions)
	p99 := sessions[len(sessions)*99/100]
	p90 := sessions[len(sessions)*90/100]
	p50 := sessions[len(sessions)*50/100]
	klog.Infof("Session setup time: p99 %d ms, st: %s, et: %s, iet: %s, %s",
		p99.watchAvailableTimeMilli-p99.apiCreationTimeMilli,
		time.UnixMilli(p99.apiCreationTimeMilli).String(),
		time.UnixMilli(p99.watchAvailableTimeMilli).String(),
		time.UnixMilli(p99.internalAvailableTimeMilli).String(),
		util.Name(p99.session))
	klog.Infof("Session setup time: p90 %d ms, st: %s, et: %s, iet: %s, %s",
		p90.watchAvailableTimeMilli-p90.apiCreationTimeMilli,
		time.UnixMilli(p90.apiCreationTimeMilli).String(),
		time.UnixMilli(p90.watchAvailableTimeMilli).String(),
		time.UnixMilli(p90.internalAvailableTimeMilli).String(),
		util.Name(p90.session))
	klog.Infof("Session setup time: p50 %d ms, st: %s, et: %s, iet: %s, %s",
		p50.watchAvailableTimeMilli-p50.apiCreationTimeMilli,
		time.UnixMilli(p50.apiCreationTimeMilli).String(),
		time.UnixMilli(p50.watchAvailableTimeMilli).String(),
		time.UnixMilli(p50.internalAvailableTimeMilli).String(),
		util.Name(p50.session))

	klog.Infof("-------- sessions rate counters--------")
	for _, v := range testSessionCounters {
		milliseconds := v.et - v.st
		klog.Infof("--------ct: %s, %d sessions tested in %d ms, rate %d/s ----------", time.UnixMilli(v.et).Truncate(time.Second), v.numOfSessions, milliseconds, int64(v.numOfSessions)*1000/(milliseconds))
	}

	for _, v := range timeoutSessions {
		klog.InfoS("timeout session", "s", util.Name(v.session))
	}

	for _, v := range failedSessions {
		klog.InfoS("failed session", "s", util.Name(v.session))
	}
}
