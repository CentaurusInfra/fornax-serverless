# fornax-serverless

## Scope
### Goals
Fornax-Serverless is targeted as general serverless application management platform within an edge site. It features a lightweight, cost-effective, highly elastic, permanent and secure serverless application management system.   
 ![image](https://user-images.githubusercontent.com/16367914/161353804-8988914c-d719-4764-8a0c-de51e9ab073a.png)

* Support serverless application (stateful/stateless)
	for both stateful and stateless applications, Fornax-serverless manages their lifecycle in serverless fashion, so as app developers not to concern themselves of server capacity plan and ops operation. Users pay per-use, not per resource allocation. It manages the application life cycles from creation, to scheduling, launching, and scaling, and termination.
	Stateful application each has its own life cycle. When an app is has no active user sessions, it enters zombie state, and Fornax-serverless will reap it and release the resource. Stateless application has no state; Fornax-serverless will decrease running instances when user requests decrease, particularly will have 0 instance(zero-cost) if no user request is being processed.
*  Manage cluster resources
	for cluster resources, Fornax-serverless, as edge site orchestrator, abstracts away the compute/storage/network, so as application developers not to take care of hardware maintenance and OS upgrade. It manages hardware resources automatically, with minimum overhead.
*  Use Quark as container runtime
	application runs as Quark containers. Fornax-serverless leverages Quark’s unique Standby/Ready mode to minimize idle footprint and optimize cold start latency.
*  Secure execution: network isolation/multi-tenancy
	as a shared application orchestration system, Fornax-serverless provides network isolation and multi-tenancy required by secure environment.

### No-goals
1. support lambda like serverless function
1. support event-triggering
1. provide direct support for global resource management

### Terms
* Stateful application
Each stateful application is different as it has own state. Users of stateful application need to access the specific interesting instance. Its lifecycle is generally controlled by application itself with the help of the management system. Particularly when application decides to stop the execution, the system has a way to get notified and then is able to release the resource it occupied. In order to facilitate such stateful application life cycle management, there should be specific mechanism (TBD) presented by system for application to notify the significant life cycle events. Stateful application is specified in format of application model (TBD).

* Stateless application
Stateless application does not have states, so its instances can be on/off anytime. System could turn off its instance anytime. Stateless application is specified in format of application model (TBD), with the execution code as container image.

* Function
Function, in context of serverless, typically is specified as a body of source code for one specific function. Fornax-serverless does not support it for now.

### Get Start With Fornax-Serverless
See our documentation on [Get Start](https://github.com/CentaurusInfra/fornax-serverless/blob/main/doc/get_start.md), You can setup and practice Fornax-Serverless application. 

 
