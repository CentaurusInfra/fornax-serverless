#! /bin/bash

set -e

#To check pods amount of nodeagent
podcount=`sudo crictl pods | awk '{if(NR>1) print $1}' | wc -l`

pushd $HOME

podcount_check(){
   if [ "$podcount" == "0" ];
    then
      echo -e "$HOSTNAME: has 0 pod.\n" 
   else
      echo -e "$HOSTNAME: have $podcount pods.\n"
   fi
}

podcount_check