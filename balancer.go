// Copyright 2024 Andres Morey
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package grpcdispatcher

import (
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
)

type picker struct {
	subConns map[string]balancer.SubConn
}

// Pick randomly selects a SubConn.
func (p *picker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	wantIp := info.Ctx.Value(dispatcherAddrCtxKey).(string)
	sc, exists := p.subConns[wantIp]
	if !exists {
		return balancer.PickResult{}, fmt.Errorf("subconn for ip %s not ready", wantIp)
	}
	return balancer.PickResult{SubConn: sc}, nil
}

// pickerBuilder builds the CustomPicker.
type pickerBuilder struct{}

// Build creates a new CustomPicker.
func (b *pickerBuilder) Build(info base.PickerBuildInfo) balancer.Picker {
	subConns := make(map[string]balancer.SubConn)
	for sc, scInfo := range info.ReadySCs {
		subConns[scInfo.Address.Addr] = sc
	}
	return &picker{subConns: subConns}
}
