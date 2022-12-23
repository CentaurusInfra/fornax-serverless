#! /bin/bash

set -e

FORNAX_ROOT=$(dirname "${BASH_SOURCE}")/../../..
source "${FORNAX_ROOT}/hack/test/gce/config_default.sh"


#To kill running process of fornaxcore
fornaxcore=`ps -aef | grep ./bin/fornaxcore | grep -v sh| grep -v grep| awk '{print $2}'`

pushd $HOME

fornaxcore_process(){
   if [ -z "$fornaxcore" ];
    then
      echo fornaxcore process is not running 
   else
      sudo pkill -9 fornaxcore
      echo fornaxcore process killed forcefully, process id $fornaxcore.
   fi
}


start_fornaxcore(){
    echo -e "## START FORNAXCORE"
    pushd $HOME/go/src/centaurusinfra.io/fornax-serverless
	echo '## RUN FORNAXCORE'
    # change max file description to fix "socket: too many open files" error from fornax core logs
   # ulimit -n 65535
    # run fornaxcore on the background
	nohup ./bin/fornaxcore --etcd-servers=${CORE_ETCD_SERVERS} --secure-port=${CORE_SECURE_PORT} --standalone-debug-mode --bind-address=${CORE_BIND_ADDRESS} >> ${CORE_LOG_FILE} 2>&1 &
    echo -e "## DONE\n"
}

fornaxcore_process

start_fornaxcore

echo "start fornaxcore service successfully."
