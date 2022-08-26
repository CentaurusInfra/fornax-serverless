/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	sigCh := make(chan os.Signal, 1)
	listen, err := net.Listen("tcp", "0.0.0.0:80")
	if err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}

	fmt.Println("echo server ready on port", 80)
	go func() {
		for {
			conn, err := listen.Accept()
			if err != nil {
				fmt.Println(err)
				fmt.Println("echo server exit")
				os.Exit(-1)
			}
			go handleConnection(conn)
		}
	}()

	// handle incoming connections
	signal.Notify(sigCh, syscall.SIGINT)
	signal.Notify(sigCh, syscall.SIGTERM)
	for {
		sig := <-sigCh
		switch sig {
		case syscall.SIGINT:
			fmt.Println("echo server exit")
			os.Exit(1)
		case syscall.SIGTERM:
			fmt.Println("echo server exit")
			os.Exit(2)
		}
	}
}

func handleConnection(conn net.Conn) {
	conn.Write([]byte(fmt.Sprintf("hello, I echo you:")))
	for {
		buffer := make([]byte, 1024)
		_, err := conn.Read(buffer)
		if err != nil {
			fmt.Println(err)
			conn.Close()
			return
		}
		// respond
		conn.Write([]byte("Hi!"))
		conn.Write(buffer)
	}
}
