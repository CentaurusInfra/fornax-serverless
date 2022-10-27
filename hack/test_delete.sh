#!/bin/bash

source ./hack/fornax_curl.zshrc
# declare num=0
delete_app_session_by_range() {
  source ./hack/fornax_curl.zshrc
  for i in {0..100}
  do
    echo "Loop spin:" $i
    app_session_name='echoserver'$i
    sudo echo "Application Session Name is : $app_session_name"
    sudo echo "app session name is: $app_session_name"
    # kubectl --kubeconfig kubeconfig delete applicationsession --namespace game1 $app_session_name
    kubectl delete application --kubeconfig kubeconfig --namespace fornaxtest $app_session_name
    #sleep 1
  done
}


# Delete application session by session name
delete_app_session_by_name() {
  source ./hack/fornax_curl.zshrc
  names=`sudo kubectl --kubeconfig kubeconfig get applicationsession --all-namespaces | awk '{print $2}'`
  for name in $names
  do
    if [ $name == "NAME" ]; then
        continue
    fi

    sudo echo "$name"
    sudo kubectl --kubeconfig kubeconfig delete applicationsession --namespace fornaxtest $name
    sleep 1
  done
}

# Delete application session by namesapce and session name
delete_app_session() {
  source ./hack/fornax_curl.zshrc
  text=`sudo kubectl --kubeconfig kubeconfig get applicationsession --all-namespaces | awk '//{print $1 "#" $2}'`
  echo "$text" > output.txt
  file="output.txt"

  while read -r line; 
  do
      echo -e "$line"
      IFS='#'     # hyphen (-) is set as delimiter
      read -ra strarr <<< "$line"   # str is read into an array as tokens separated by IFS
      namesp=${strarr[0]}
      sessionname=${strarr[1]}
      if [ $namesp == "NAMESPACE" ];then
          continue
      fi
      echo "name space is:  $namesp "
      echo "session name is :  $sessionname "
      # delete application session
      sudo kubectl --kubeconfig kubeconfig delete applicationsession --namespace $namesp $sessionname
  done <$file 
}


# Delete application 
delete_app() {
  source ./hack/fornax_curl.zshrc
  # names=`sudo kubectl --kubeconfig kubeconfig get applications --all-namespaces | awk '//{printf "%s-%s\n", $1,$2}'`
  text=`sudo kubectl --kubeconfig kubeconfig get applications --all-namespaces | awk '//{print $1 "-" $2}'`
  echo "$text" > output.txt
  file="output.txt"

  while read -r line; 
  do
      echo -e "$line"
      IFS='-'     # hyphen (-) is set as delimiter
      read -ra strarr <<< "$line"   # str is read into an array as tokens separated by IFS
      namesp=${strarr[0]}
      na=${strarr[1]}
      if [ $namesp == "NAMESPACE" ];then
          continue
      fi
      echo "name space is:  $namesp "
      echo "name is :  $na "
      # delete application
      sudo kubectl delete application --kubeconfig kubeconfig --namespace $namesp $na
  done <$file 
}


# Delete pods
delete_pods() {
  source ./hack/fornax_curl.zshrc

  pod_ids=`sudo crictl pods | awk '{print $1}'`

  for pod_id in $pod_ids
  do
    if [ $pod_id == "POD" ]; then
      continue
    fi
    
    echo $pod_id
    sudo crictl stopp $pod_id
    sleep 1
    sudo crictl rmp $pod_id
  done
}


delete_pods

delete_app_session

delete_app

