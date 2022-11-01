#! /bin/bash

set -e

delete_instance_by_filter() {
  names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`
  for name in $names
  do
    if [ $name == "NAME" ]; then
        continue
    fi

    if [[ $name == *"fornaxcore"* ]] || [[ $name == *"nodeagent"* ]]; then
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

check_knownhosts_google(){
    if [ "$(ls $HOME/.ssh/google_compute_known_hosts)" != "" ]; then
        echo -e "google known_hosts already exits, we remove this file first\n";
        rm -rf ~/.ssh/google_compute_known_hosts
    fi
}


delete_instance_by_filter

# check_knownhosts
check_knownhosts_google

echo "all instance have been deleted seccessfully."