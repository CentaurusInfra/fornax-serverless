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
	"sync"
	"time"
)

func main() {
	mapa := map[int]int{}
	count := 60000
	// for i := 0; i < count; i++ {
	// 	mapa[i] = i
	// }

	mu := sync.RWMutex{}
	st := time.Now().UnixMicro()
	for i := 0; i < count; i++ {
		mu.Lock()
		mapa[i] = i
		mu.Unlock()
	}
	et := time.Now().UnixMicro()
	fmt.Printf("use %d micro second\n", et-st)

	st = time.Now().UnixMicro()
	for i := 0; i < count; i++ {
		_ = mapa[i]
	}
	et = time.Now().UnixMicro()
	fmt.Printf("use %d micro second\n", et-st)

}
