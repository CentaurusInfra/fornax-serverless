#! /bin/bash

set -e

#To kill running process of nodeagent
nodeagent=`ps -aef | grep ./bin/nodeagent | grep -v sh| grep -v grep| awk '{print $2}'`

pushd $HOME

nodeagent_process(){
   if [ -z "$nodeagent" ];
    then
      echo nodeagent process is not running 
   else
      sudo pkill -9 nodeagent
      echo nodeagent process killed forcefully, process id $nodeagent.
   fi
}

start_nodeagent(){
    echo -e "## START NODEAGENT"
    pushd $HOME/go/src/centaurusinfra.io/fornax-serverless
	echo '## RUN NODEAGENT To Connect to FORNAXCORE'
    echo '# Get Fornaxcore IP'
    fornaxcoreip=`gcloud compute instances list --format='table(INTERNAL_IP)' --filter="name=fornaxcore" | awk '{if(NR==2) print $1}'`
    echo "Fornaxcore IP is: $fornaxcoreip"
    sleep 1
	# following line command, put nodeagent run at background
	nohup sudo ./bin/nodeagent --fornaxcore-url $fornaxcoreip:18001 --disable-swap=false >> nodeagent.logs 2>&1 &
    echo -e "## DONE\n"
}

nodeagent_process

start_nodeagent

echo "start nodeagent service successfully."