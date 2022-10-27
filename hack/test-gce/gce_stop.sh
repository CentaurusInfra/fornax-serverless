#! /bin/bash

set -e


stop_instance_by_filter() {
  names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`
  for name in $names
  do
    if [ $name == "NAME" ]; then
        continue
    fi

    if [[ $name == *"fornaxcore"* ]] || [[ $name == *"nodeagent"* ]]; then
        echo "delete instance name: $name"
        gcloud compute instances stop $name --async --project=quark-serverless --zone=us-central1-a --quiet &
        sleep 1
    fi
  done
}

stop_instance_by_filter

echo "all instance have been stopped seccessfully."