#! /bin/bash

set -e

#To check running process of nodeagent
nodeagent=`ps -aef | grep ./bin/nodeagent | grep -v sh| grep -v grep| awk '{print $2}'`

pushd $HOME

nodeagent_check(){
   if [ "$nodeagent" != "" ];
    then
      echo -e "$HOSTNAME: nodeagent service is running.\n" 
   else
      echo -e "$HOSTNAME: nodeagent service is not running.\n"
   fi
}

nodeagent_check