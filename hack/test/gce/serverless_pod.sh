#! /bin/bash

set -e

FORNAX_ROOT=$(dirname "${BASH_SOURCE}")/../../..
source "${FORNAX_ROOT}/hack/test/gce/config_default.sh"

pushd $HOME

# check nodeagent pod amount in each instance
check_nodeagent_pod_amount() {
    echo -e "## check nodeagent pod amount in the instances\n"
    names=`gcloud compute instances list --project ${PROJECT} --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [[ $name == *"${NODE_INSTANCE_PREFIX}"* ]]; then
            echo "check nodeagent pod amount in: $name"
            gcloud compute ssh $name --command="bash ~/nodeagent_podcount.sh" --project=${PROJECT} --zone=${ZONE} &
            sleep 1
        fi
    done    
}

check_nodeagent_pod_amount

echo "check all instance pod amount done"