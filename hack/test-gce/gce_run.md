# Serverless gce test environment platform setup and running steps

## Overview
This doc represent how to setup gce serverless test envrironment and detail steps.


## Machine and file Prepare
1. If you are new to this project, you can follow instruction "1. Install and Setup Fornax Core" in the fornax_setup.md doc, you can reference [fornax_setup.md](https://github.com/CentaurusInfra/fornax-serverless/blob/main/doc/fornax_setup.md).
```sh
```
2. After you build project, you can follow step and deploy the execute file, all bash script code is in /hack/test-gce folder
```sh
```

## 1. Run the following file and deploy

step 1. run gce_create_deploy.sh file and wait untill finished(first step: create instance, second step: copy file to each instance, third step: execute script to install third party software)

```script
bash ./hack/test-gce/gce_create_deploy.sh
```

After above done, please run following script to start fornaxcore and nodeagent service.

```script
bash ./hack/test-gce/serverless_start.sh
```

## 2. Verify fornaxcore and nodeagent service running.
1. login fornaxcore machine and verify fornaxcore service is started. After login, run the following script and see fornaxcore service if it is started.

```script
bash ./hack/test-gce/serverless_status.sh
```

## 3. Login fornaxcor machine and run the test script.

1. cold start
run the following script to perform test. (note: cycle based on node number, 2 nodes should run at 5 cycle, 20 nodes run at 50 cycle)

```script
cd ./go/src/centaurusinfra.io/fornax-serverless
./bin/fornaxtest --test-case session_create --num-of-session-per-app 1 --num-of-app 100 --num-of-init-pod-per-app 0 --num-of-test-cycle 50
```

## 4. After running the test, record the "Test Summary", and P99, P90, P50 for your reference in the future.


## 5. Reference
Notes: the following bash script, you can run at your host or dev machine.

1. Stop fornaxcore and nodeagent, you can run:
```script
bash ./hack/test-gce/serverless_stop.sh
```

2. After your test done, please delete all instance. avoid the charge. Run the following command
```script
bash ./hack/test-gce/gce_delete.sh
```

2. Stop all instance, you can run:
```script
bash ./hack/test-gce/gce_stop.sh
```

2. Start all instance, you can run:
```script
bash ./hack/test-gce/gce_start.sh
```
