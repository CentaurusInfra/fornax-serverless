#! /bin/bash

set -e

#To check running process of fornaxcore
fornaxcore=`ps -aef | grep ./bin/fornaxcore | grep -v sh| grep -v grep| awk '{print $2}'`

pushd $HOME

fornaxcore_check(){
   if [ "$fornaxcore" != "" ];
    then
      echo -e "$HOSTNAME: fornaxcore service is running. process id $fornaxcore\n" 
   else
      echo -e "$HOSTNAME: fornaxcore service is not running.\n"
   fi
}

fornaxcore_check