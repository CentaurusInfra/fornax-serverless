# Serverless gce test environment platform setup and running steps

## Overview
This doc represent how to setup gce serverless test envrironment and detail steps.


## Machine and file Prepare
1. If you are new to this project, you can follow instruction "1. Install and Setup Fornax Core" in the fornax_setup.md doc, you can reference [fornax_setup.md](https://github.com/CentaurusInfra/fornax-serverless/blob/main/doc/fornax_setup.md).
```sh
```
2. After you build project, you can follow step and deploy the execute file, all bash script code is in /hack/test/gce folder
```sh
```

## 1. Run the following file and deploy

step 1. run gce_create_deploy.sh file and wait untill finished(first step: create instance, second step: copy file to each instance, third step: execute script to install third party software, fourth step - optional: running fornaxtest, fifth step - optional: collecting logs)

```script
bash ./hack/test/gce/gce_create_deploy.sh
```

By default, before run script above, all ENV set as the description in ./hack/test/gce/config_default.sh. you can change all ENV based on test request as below:
```
export FORNAX_GCE_PROJECT=workload-controller-manager FORNAX_GCE_ZONE=us-central1-a FORNAX_INSTANCE_PREFIX=sonya-fornax CORE_AUTO_START=true NODEAGENT_AUTO_START=true SIM_AUTO_START=false TEST_AUTO_START=false LOG_AUTO_COLLECT=false
export CORE_MACHINE_TYPE=n1-standard-16 CORE_DISK_TYPE=pd-ssd CORE_DISK_SIZE=40 CORE_IMAGE_PROJECT=ubuntu-os-cloud CORE_IMAGE=ubuntu-2004-focal-v20221018  NODE_MACHINE_TYPE=n1-standard-32 NODE_DISK_TYPE=pd-ssd NODE_DISK_SIZE=30 NODE_IMAGE_PROJECT=ubuntu-os-cloud NODE_IMAGE=ubuntu-2004-focal-v20221018 NODE_NUM=2
export  CORE_ETCD_SERVERS=http://127.0.0.1:2379 CORE_SECURE_PORT=9443 CORE_BIND_ADDRESS=127.0.0.1 CORE_LOG_FILE=fornaxcore-$(date '+%s').log CORE_STANDALONE_DEBUG_MODE=true CORE_DEFAULT_PORT=18001 NODE_DISABLE_SWAP=false NODE_LOG_FILE=nodeagent-$(date '+%s').log
```

if you set SIM_AUTO_START=true, add the following ENV
```
export SIM_NUM_OF_NODE=100 SIM_LOG_FILE=simulatenode-$(date '+%s').log
```

if you set TEST_AUTO_START=true, add the following ENV
```
export TEST_NUM_OF_SESSION_PER_APP=10 TEST_NUM_OF_APP=2 TEST_NUM_OF_INIT_POD_PER_APP=0 TEST_NUM_OF_BURST_POD_PER_APP=10 TEST_NUM_OF_TEST_CYCLE=10 TEST_LOG_FILE=fornaxtest-$(date '+%s').log
```
if you set LOG_AUTO_COLLECT=true, add the following ENV
```
export LOG_DIR_LOCAL=~/logs/fornax/fornaxtest
```


## 2. Manually verify fornaxcore and nodeagent service running.
1. login fornaxcore machine and verify fornaxcore service is started. After login, run the following script and see fornaxcore service if it is started.

```script
bash ./hack/test/gce/serverless_status.sh
```

## 3. Manually login fornaxcor machine and run the test script.

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
bash ./hack/test/gce/serverless_stop.sh
```

2. After your test done, please delete all instance. avoid the charge. Run the following command
```script
bash ./hack/test/gce/gce_delete.sh
```

2. Stop all instance, you can run:
```script
bash ./hack/test/gce/gce_stop.sh
```

2. Start all instance, you can run:
```script
bash ./hack/test/gce/gce_start.sh
```
