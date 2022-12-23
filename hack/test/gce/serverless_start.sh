#! /bin/bash

set -e

FORNAX_ROOT=$(dirname "${BASH_SOURCE}")/../../..
source "${FORNAX_ROOT}/hack/test/gce/config_default.sh"

pushd $HOME

# restart serverless fornaxcore and nodeagent
start_serverless_by_filter() {
    echo -e "## Restart serverless fornaxcore and nodeagent\n"
    names=`gcloud compute instances list --project ${PROJECT} --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"${CORE_INSTANCE_PREFIX}"* ]]; then
            echo "start fornaxcore service in: $name"
            gcloud compute ssh $name --command="bash ~/fornaxcore_start.sh" --project=${PROJECT} --zone=${ZONE} > /dev/null 2>&1 &
            sleep 1
        fi

        if [[ $name == *"${NODE_INSTANCE_PREFIX}"* ]]; then
            echo "Start nodeagent service in: $name"
            gcloud compute ssh $name --command="bash ~/nodeagent_start.sh" --project=${PROJECT} --zone=${ZONE} > /dev/null 2>&1 &
            sleep 1
        fi
    done
}

start_serverless_by_filter

echo "all instance services started successfully."