#! /bin/bash

set -e


delete_instance_by_number() {
    # Enter the nodeagent number which you want to delete 
    echo -e "## Enter nodeagent number which you want to delete in your test:"
    read instance_num
    echo -e "\n"

    # delete virtual machine instance
    gcloud compute instances delete davidzhu-fornaxcore --zone=us-central1-a

    # instance_num=2
    for ((i = 1; i<=$instance_num; i++))
    do
        instance_name='davidzhu-nodeagent-'$i
        echo -e "delete $instance_name \n"
        gcloud compute instances delete $instance_name --zone=us-central1-a --quiet &

    done
}

delete_instance_by_filter() {
  names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`
  for name in $names
  do
    if [ $name == "NAME" ]; then
        continue
    fi

    if [[ $name == *"davidzhu-fornaxcore"* ]] || [[ $name == *"davidzhu-nodeagent"* ]]; then
        echo "delete instance name: $name"
        gcloud compute instances delete $name --project=quark-serverless --zone=us-central1-a --quiet &
        sleep 1
    fi
  done
}

check_knownhosts(){
    if [ "$(ls $HOME/.ssh/known_hosts)" != "" ]; then
        echo -e "known_hosts already exits, we remove this file first\n";
        rm -rf ~/.ssh/known_hosts
    fi
}

# delete_instance_by_number

delete_instance_by_filter

check_knownhosts

echo "all instance have been deleted seccessfully."