# Quark Container Installation and Configuration for Fornax Serverless

## Overview
This doc represent how to install Quark Container and how to integeration with Fornax Serverless.


## Machine Prepare (Brand new machine)
If you have a brand new machine, you may need to install following component to start the machine. 

```script
sudo apt-get update
sudo apt install build-essential
sudo snap install curl  # version 7.81.0
sudo apt-get install vim
sudo apt install git
```
## 1. Install Containerd (If you have not install the containerd, do this step)
If you have not installed containerd, you need install containerd. The detail see [Setting Up Detail](https://github.com/CentaurusInfra/fornax-serverless/blob/main/doc/fornax_setup.md). Reference 2.1.1 Install containerd. 
If you already installed containerd, skip this step.

## 2. Install Rust
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

Update latest version, rustup update
```sh
rustup update nightly-2021-12-04-x86_64-unknown-linux-gnu
rustup toolchain install nightly-2021-12-04-x86_64-unknown-linux-gnu
rustup default nightly-2021-12-04-x86_64-unknown-linux-gnu
```

Add runst-src component to nightly
```sh
rustup component add rust-src --toolchain nightly-2021-12-04-x86_64-unknown-linux-gnu
```

Verify src component
```sh
rustup component list --toolchain nightly-2021-12-04-x86_64-unknown-linux-gnu | grep rust-src
```

Display current Installation Info
```sh
rustup show 
rustup --version
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
1. Download the source code and go Quark folder
```sh
git clone https://github.com/QuarkContainer/Quark.git
cd Quark
```

2. make
```sh
make
```

3. make install after make is successful
```sh
make install
```

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
