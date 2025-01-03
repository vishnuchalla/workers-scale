// Copyright 2024 The workers-scale Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package workerscale

import "time"

// Resource constants
const RosaHCP = "rosa-hcp"
const JobName = "workers-scale"
const ClusterType = "clusterType"
const MachineNamespace = "openshift-machine-api"
const DefaultNamespace = "default"
const DefaultClusterAutoScaler = "default"
const AutoScalerBuffer = 10

// Measurement constants
const measurementName = "nodeLatency"
const nodeReadyLatencyMeasurement = "nodeReadyLatencyMeasurement"
const nodeReadyLatencyQuantilesMeasurement = "nodeReadyLatencyQuantilesMeasurement"
const nodeReadyLatencyStackedMeasurement = "nodeReadyLatencyStackedMeasurement"

// Misc constants
const maxWaitTimeout = 4 * time.Hour
const TenMinutes = 600
