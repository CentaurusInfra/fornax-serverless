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

package v1

const (
	LabelFornaxCore                   = "core.fornax-serverless.centaurusinfra.io"
	LabelFornaxCoreNode               = "node.fornax-serverless.centaurusinfra.io"
	LabelFornaxCorePod                = "pod.fornax-serverless.centaurusinfra.io"
	LabelFornaxCoreNodeDaemon         = "daemon.fornax-serverless.centaurusinfra.io"
	LabelFornaxCoreApplication        = "application.core.fornax-serverless.centaurusinfra.io"
	LabelFornaxCoreCreationUnixMicro  = "create.unixmicro.core.fornax-serverless.centaurusinfra.io"
	LabelFornaxCoreApplicationSession = "applicationsession.core.fornax-serverless.centaurusinfra.io"
	LabelFornaxCoreSessionService     = "sessionservice.core.fornax-serverless.centaurusinfra.io"
)

var (
	ApplicationKind        = SchemeGroupVersion.WithKind("Application")
	ApplicationSessionKind = SchemeGroupVersion.WithKind("ApplicationSession")
)
