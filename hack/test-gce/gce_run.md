# Serverless gce test environment platform setup and running steps

## Overview
This doc represent how to setup gce serverless test envrironment and detail steps.


## Machine and file Prepare
If you already have machine and with source file ready, and have all build execut file on  ~/go/src/CentaurusInfra/fornax-serverlessan/bin,
you can copy following file to $HOME(or create new file with the same name and copy following file content to the file). 

```script
gce_create_deploy.sh
fornaxcore_deploy.sh
nodeagent_deploy.sh
fornaxcore_start.sh
nodeagent_start.sh
serverless_start.sh
```

make sure execute binay file is in ~/go/src/CentaurusInfra/fornax-serverlessan/bin/
```sh
fornaxcore
fornaxtest
nodeagent
```

make sure kubeconfig is in ~/go/src/CentaurusInfra/fornax-serverless/
```
kubeconfig
```

## 1. Run the following file and deploy

step 1. run gce_create_deploy.sh file and wait untill finished(first step: create instance, second step: copy file to each instance, third step: execute script to install thired party software)

```script
bash gce_create_deploy.sh
```

After above done, please run following script to start fornaxcore and nodeagent service.

```script
bash serverless_start.sh
```

## 2. Verify fornaxcore and nodeagent service running.
1. login fornaxcore machine and verify fornaxcore service is started. After login, run the following script and see fornaxcore service if it is started.

```script
ps -aux | grep fornaxcore
```

2. pickup one of nodeagent, login nodeagent machine and verify nodeagent service is started. After login, run the following script and see nodeagent serviceif it is started.

```script
ps -aus | grep nodeagent
```

## 3. Login fornaxcor machine and run the test script.

1. cold start
run the following script to perform test. (note: cycle based on node number, 2 nodes should run at 5 cycle, 20 nodes run at 50 cycle)

```script
./bin/fornaxtest --test-case session_create --num-of-session-per-app 1 --num-of-app 100 --num-of-init-pod-per-app 0 --num-of-test-cycle 50
```

## 4. After running the test, record the "Test Summary", and P99, P90, P50 for your reference in the future.


## 5. Reference

1. Stop fornaxcore and nodeagent, you can run:
```script
bash serverless_stop.sh
```

2. After your test done, please delete all instance. avoid the charge. Run the following command
```script
bash gce_delete.sh
```

2. Stop all instance, you can run:
```script
bash gce_stop.sh
```

2. Start all instance, you can run:
```script
bash gce_start.sh
```
