#! /bin/bash

set -e

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
    # run fornaxcore on the background
	nohup ./bin/fornaxcore --etcd-servers=http://127.0.0.1:2379 --secure-port=9443 --standalone-debug-mode --bind-address=127.0.0.1 >> fornaxcore.logs 2>&1 &
    echo -e "## DONE\n"
}

fornaxcore_process

start_fornaxcore

echo "start fornaxcore service successfully."