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
        gcloud compute instances delete $instance_name --zone=us-central1-a

    done
    # gcloud compute instances delete davidzhu-instance-1 --zone=us-central1-a
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
        gcloud compute instances delete $name --zone=us-central1-a
        sleep 1
    fi
  done
}

# delete_instance_by_number

delete_instance_by_filter

echo "all instance have been deleted seccessfully."