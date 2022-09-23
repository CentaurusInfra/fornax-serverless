#!/bin/bash

#start
declare num=0
source ./hack/fornax_curl.zshrc

# create app
post_app  fornaxtest  ./hack/test-data/sessionwrapper-echoserver-app-create.yaml

# create app-session
source ./hack/fornax_curl.zshrc
snum=1
for i in {1..$snum}
do
  echo "Loop spin:" $i
  num=$((i-1))
  echo "n is $num"
  string1='echo-session-'$num
  string2='echo-session-'$i
  sudo echo "$string1"
  sudo echo "$string2"
  # Replace previous session
  sudo sed -i "s/$string1/$string2/" ./hack/test-data/sessionwrapper-echoserver-session-create.yaml
  # Verify the session number
  awk '{if(NR==4) print $0}' ./hack/test-data/sessionwrapper-echoserver-session-create.yaml
  #sleep 1
  post_session fornaxtest  ./hack/test-data/sessionwrapper-echoserver-session-create.yaml
  #sleep 1
done

#end