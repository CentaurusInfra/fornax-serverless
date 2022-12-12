#! /bin/bash

set -e

FORNAX_ROOT=$(dirname "${BASH_SOURCE}")/../../..
source "${FORNAX_ROOT}/hack/test/gce/config_default.sh"

pushd $HOME

# remove fornaxcore and nodeagent file from the instance
remove_fornaxcore_nodeagent_file() {
    echo -e "## Stop fornaxcore and nodeagent file in the instances\n"
    names=`gcloud compute instances list --project ${PROJECT} --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [[ $name == *"${CORE_INSTANCE_PREFIX}"* ]]; then
            echo "remove fornaxcore and fornaxtest file in the instance: $name"
            gcloud compute --project=${PROJECT} ssh $name --command="cd ~/go/src/centaurusinfra.io/fornax-serverless/bin && rm fornaxcore fornaxtest" --zone=${ZONE} &
        fi

        if [[ $name == *"${NODE_INSTANCE_PREFIX}"* ]]; then
            echo "remove nodeagent file in instance: $name"
            gcloud compute --project=${PROJECT} ssh $name --command="cd ~/go/src/centaurusinfra.io/fornax-serverless/bin && rm nodeagent" --zone=${ZONE} &
        fi
    done
    sleep 2 
    echo -e "Remove file is done.\n"   
}

# copy exe file to the each instance
copy_file_to_instance() {
    echo -e "## Copy file to the instance\n"
    names=`gcloud compute instances list --project ${PROJECT} --format="table(name)" | awk '{print $1}'`
    for name in $names
    do
        if [ $name == "NAME" ]; then
            continue
        fi

        if [[ $name == *"${CORE_INSTANCE_PREFIX}"* ]]; then
            echo "copy file to fornaxcore instance: $name"
            gcloud compute scp ./bin/fornaxcore ./bin/fornaxtest $name:~/go/src/centaurusinfra.io/fornax-serverless/bin/ --project=${PROJECT} --zone=${ZONE} &
        fi

        if [[ $name == *"${NODE_INSTANCE_PREFIX}"* ]]; then
            echo "copy file to nodeagent instance: $name"
            gcloud compute scp ./bin/nodeagent $name:~/go/src/centaurusinfra.io/fornax-serverless/bin/ --project=${PROJECT} --zone=${ZONE} &
        fi
    done
    
    echo -e "Copy file is done.\n"
    sleep 2
}

remove_fornaxcore_nodeagent_file

copy_file_to_instance

echo -e "all replace file is done on instance.\n"