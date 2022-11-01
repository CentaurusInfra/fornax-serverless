#! /bin/bash

set -e

pushd $HOME

# restart serverless fornaxcore and nodeagent
start_serverless_by_filter() {
    echo -e "## Restart serverless fornaxcore and nodeagent\n"
    names=`gcloud compute instances list --project quark-serverless --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"fornaxcore"* ]]; then
            echo "start fornaxcore service in: $name"
            gcloud compute ssh $name --command="bash ~/fornaxcore_start.sh" --project=quark-serverless --zone=us-central1-a > /dev/null 2>&1 &
            sleep 1
        fi

        if [[ $name == *"nodeagent"* ]]; then
            echo "Start nodeagent service in: $name"
            gcloud compute ssh $name --command="bash ~/nodeagent_start.sh" --project=quark-serverless --zone=us-central1-a > /dev/null 2>&1 &
            sleep 1
        fi
    done
}

start_serverless_by_filter

echo "all instance services started successfully."