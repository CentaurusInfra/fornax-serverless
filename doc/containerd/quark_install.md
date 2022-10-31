# Quark Container Installation and Configuration for Fornax Serverless

## Overview
This doc represent how to install Quark Container and how to integeration with Fornax Serverless.


## Machine Prepare (Brand new machine)
If you have a brand new machine, you may need to install following component to start the machine. 

```script
sudo apt-get update
sudo apt install build-essential
sudo apt install curl
sudo apt-get install vim
sudo apt install git
```
## 1. Install Containerd (If you have not install the containerd, do this step)
If you have not installed containerd, you need install containerd. The detail see [Setting Up Detail](https://github.com/CentaurusInfra/fornax-serverless/blob/main/doc/fornax_setup.md). Reference 2.1.1 Install containerd. 
If you already installed containerd, skip this step.

## 2. Install Rustup
When intallation stop and show option, please select option 1. then follow instruction
```sh
curl --proto '=https' --tlsv1.2 https://sh.rustup.rs -sSf | sh
```

To configure your current shell, run
```sh
source $HOME/.cargo/env
```

Verify
```sh
ls -l $HOME/.cargo/bin
ls -l $HOME/.cargo/env
```

## 3. Install Cargo Build
The build take a little more time, be patient.
```sh
cargo install cargo-xbuild
```

Installing lcap library
```sh
sudo apt-get install libcap-dev
```

Also, some extra libraries 
```sh
sudo apt-get install build-essential cmake gcc libudev-dev libnl-3-dev libnl-route-3-dev ninja-build pkg-config valgrind python3-dev cython3 python3-docutils pandoc libclang-dev
```


## 4. Build Quark Container
follow https://github.com/QuarkContainer/Quark#installing-from-source

## 5. Setup and Configuration Quark Container
1. You can reference part "Install/Setup/Configuration" in [Quark Read.md](https://github.com/QuarkContainer/Quark#readme).



## 6. Integeration with Fornax Serverless

1. Build Quark Runtime with shim mode, To build Quark with shim mode, change the following configuration in <b>config.json</b>

```sh
  "ShimMode"      : true,
```

and run 
```sh
make clean; make
```

in a terminal to rebuild quark binary


2. Install Quark binary to each fornax server-less nodes
```sh
make install
```

Notice the quark binary is renamed as containerd-shim-quark-v1, this is to follow containerd's naming convention for shims.

3. Config containerd in fornax server-less node or cluster.
open <b>/etc/containerd/config.toml</b> and add or append the following entry in the containerd config.toml

```
version = 2
[plugins."io.containerd.runtime.v1.linux"]
  shim_debug = true
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runc]
  runtime_type = "io.containerd.runc.v2"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runsc.v1"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.quark]
  runtime_type = "io.containerd.quark.v1"
```

4. Restart the containerd service

```sh 
sudo systemctl restart containerd
```


## 7. Install CNI (if you have not install CNI)
If you have not installed CNI,  please reference 2.1.2 Install CNI in [Fornax  Setup Detail](https://github.com/CentaurusInfra/fornax-serverless/blob/main/doc/fornax_setup.md).


## Reference
### 1. If you want to run "runsc" (gVisor), you need install runsc component
1. Install latest relase version. To download and install the latest release manually follow these steps:
```sh
(
  set -e
  ARCH=$(uname -m)
  URL=https://storage.googleapis.com/gvisor/releases/release/latest/${ARCH}
  wget ${URL}/runsc ${URL}/runsc.sha512 \
    ${URL}/containerd-shim-runsc-v1 ${URL}/containerd-shim-runsc-v1.sha512
  sha512sum -c runsc.sha512 \
    -c containerd-shim-runsc-v1.sha512
  rm -f *.sha512
  chmod a+rx runsc containerd-shim-runsc-v1
  sudo mv runsc containerd-shim-runsc-v1 /usr/local/bin
)
```

2. To install gVisor as a Containerd runtime, run the following commands:
```sh
/usr/local/bin/runsc install
sudo systemctl restart containerd
```

### 2. If you want to run Kata, you need install Kata component

1. Install Kata
```sh
ARCH=$(arch)
BRANCH="${BRANCH:-master}"
sudo sh -c "echo 'deb http://download.opensuse.org/repositories/home:/katacontainers:/releases:/${ARCH}:/${BRANCH}/xUbuntu_$(lsb_release -rs)/ /' > /etc/apt/sources.list.d/kata-containers.list"
curl -sL  http://download.opensuse.org/repositories/home:/katacontainers:/releases:/${ARCH}:/${BRANCH}/xUbuntu_$(lsb_release -rs)/Release.key | sudo apt-key add -
sudo -E apt-get update
sudo -E apt-get -y install kata-runtime kata-proxy kata-shim
```

2. Verify Kata runtime version
```sh
kata-runtime --kata-show-default-config-paths   
kata-runtime --kata-config=/some/where/configuration.toml

kata-runtime -version
```
