#!/usr/bin/env bash

#####Ubuntu default value for "ulimit -n" is 1024, which is not enough for some applicaiton
### Only support ubuntu 

ULIMIT_N=${ULIMIT_N:-65535}

function attach_string_tofile {
    local file_update=$1
    local string_match=$2
    local string_attach=$3

    match_return=$(grep -Fe "$string_match" "$file_update")

    if [[ -z $match_return ]]; then
        ## stirng not existing, attach it.
        echo "$string_attach" | sudo tee -a $file_update
    else
        ## stirng already existing, check if started with "#"        
        if [[ $match_return == \#* ]]; then
            ## started with "#", still need attach
            echo "$string_attach" | sudo tee -a $file_update
        fi
    
    fi
}


###############
#   main function
###############

echo "Updating max openfiles number: ulimit -n"

# add the following line to  /etc/sysctl.conf
# fs.file-max = 65535
attach_string_tofile "/etc/sysctl.conf" "fs.file-max" "fs.file-max = ${ULIMIT_N}"

# refresh with new config
sudo sysctl -p

# add following lines to /etc/security/limits.conf
#* soft     nproc          65535    
#* hard     nproc          65535   
#* soft     nofile         65535   
#* hard     nofile         65535
#root soft     nproc          65535    
#root hard     nproc          65535   
#root soft     nofile         65535   
#root hard     nofile         65535

attach_string_tofile "/etc/security/limits.conf" "\* soft     nproc" "* soft     nproc          ${ULIMIT_N}"
attach_string_tofile "/etc/security/limits.conf" "\* hard     nproc" "* hard     nproc          ${ULIMIT_N}"
attach_string_tofile "/etc/security/limits.conf" "\* soft     nofile" "* soft     nofile          ${ULIMIT_N}"
attach_string_tofile "/etc/security/limits.conf" "\* hard     nofile" "* hard     nofile          ${ULIMIT_N}"
attach_string_tofile "/etc/security/limits.conf" "root soft     nproc" "root soft     nproc          ${ULIMIT_N}"
attach_string_tofile "/etc/security/limits.conf" "root hard     nproc" "root hard     nproc          ${ULIMIT_N}"
attach_string_tofile "/etc/security/limits.conf" "root soft     nofile" "root soft     nofile          ${ULIMIT_N}"
attach_string_tofile "/etc/security/limits.conf" "root hard     nofile" "root hard     nofile          ${ULIMIT_N}"

#add this line to /etc/pam.d/common-session
#session required pam_limits.so
attach_string_tofile "/etc/pam.d/common-session" "session required pam_limits.so" "session required pam_limits.so"



#####if you are running in same session, please log out and log in again to verify ulimit -n