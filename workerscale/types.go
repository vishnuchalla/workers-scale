// Copyright 2024 workers-scale Authors.
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

import (
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Interface for our scenarios
type Scenario interface {
	OrchestrateWorkload(ScaleConfig) string
}

// ScaleConfig contains configuration for scaling
type ScaleConfig struct {
	UUID                  string
	AdditionalWorkerNodes int
	Metadata              map[string]interface{}
	Indexer               indexers.Indexer
	GC                    bool
	ScaleEventEpoch       int64
	AutoScalerEnabled     bool
	MCKubeConfig          string
	IsHCP                 bool
}

// Struct to extract AMIID from aws provider spec
type AWSProviderSpec struct {
	AMI struct {
		ID string `json:"id"`
	} `json:"ami"`
}

// MachineInfo provides information about a machine resource
type MachineInfo struct {
	nodeUID           string
	creationTimestamp time.Time
	readyTimestamp    time.Time
}

// MachineSetInfo provides information about a machineset resource
type MachineSetInfo struct {
	LastUpdatedTime time.Time
	PrevReplicas    int
	CurrentReplicas int
}

// MachinePool of a ROSA cluster
type MachinePool struct {
	ID       string `json:"id"`
	Replicas int    `json:"replicas"`
}

// ProviderStatusCondition of a machine
type ProviderStatusCondition struct {
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	Message            string      `json:"message"`
	Reason             string      `json:"reason"`
	Status             string      `json:"status"`
	Type               string      `json:"type"`
}

// ProviderStatus of a machine
type ProviderStatus struct {
	Conditions []ProviderStatusCondition `json:"conditions"`
}

// NodeReadyMetric to capture details on node bootup
type NodeReadyMetric struct {
	Timestamp                time.Time         `json:"timestamp"`
	ScaleEventTimestamp      time.Time         `json:"scaleEventTimestamp"`
	MachineCreationTimestamp time.Time         `json:"-"`
	MachineCreationLatency   int               `json:"machineCreationLatency"`
	MachineReadyTimestamp    time.Time         `json:"-"`
	MachineReadyLatency      int               `json:"machineReadyLatency"`
	NodeCreationTimestamp    time.Time         `json:"-"`
	NodeCreationLatency      int               `json:"nodeCreationLatency"`
	NodeReadyTimestamp       time.Time         `json:"-"`
	NodeReadyLatency         int               `json:"nodeReadyLatency"`
	MetricName               string            `json:"metricName"`
	AMIID                    string            `json:"amiID"`
	UUID                     string            `json:"uuid"`
	JobName                  string            `json:"jobName,omitempty"`
	Name                     string            `json:"nodeName"`
	Labels                   map[string]string `json:"labels"`
	Metadata                 interface{}       `json:"metadata,omitempty"`
}

// NodeReadyStacked to capture details on node bootup
type NodeReadyLatencyStackedMeasurement struct {
	UUID                string      `json:"uuid"`
	BootImageID         string      `json:"bootImageID"`
	MachineCreation_P99 int         `json:"machineCreation_P99"`
	MachineCreation_P95 int         `json:"machineCreation_P95"`
	MachineCreation_P50 int         `json:"machineCreation_P50"`
	MachineCreation_Min int         `json:"machineCreation_min"`
	MachineCreation_Max int         `json:"machineCreation_max"`
	MachineCreation_Avg int         `json:"machineCreation_avg"`
	MachineReady_P99    int         `json:"machineReady_P99"`
	MachineReady_P95    int         `json:"machineReady_P95"`
	MachineReady_P50    int         `json:"machineReady_P50"`
	MachineReady_Min    int         `json:"machineReady_min"`
	MachineReady_Max    int         `json:"machineReady_max"`
	MachineReady_Avg    int         `json:"machineReady_avg"`
	NodeCreation_P99    int         `json:"nodeCreation_P99"`
	NodeCreation_P95    int         `json:"nodeCreation_P95"`
	NodeCreation_P50    int         `json:"nodeCreation_P50"`
	NodeCreation_Min    int         `json:"nodeCreation_min"`
	NodeCreation_Max    int         `json:"nodeCreation_max"`
	NodeCreation_Avg    int         `json:"nodeCreation_avg"`
	NodeReady_P99       int         `json:"nodeReady_P99"`
	NodeReady_P95       int         `json:"nodeReady_P95"`
	NodeReady_P50       int         `json:"nodeReady_P50"`
	NodeReady_Min       int         `json:"nodeReady_min"`
	NodeReady_Max       int         `json:"nodeReady_max"`
	NodeReady_Avg       int         `json:"nodeReady_avg"`
	Timestamp           time.Time   `json:"timestamp"`
	MetricName          string      `json:"metricName"`
	JobName             string      `json:"jobName,omitempty"`
	Metadata            interface{} `json:"metadata,omitempty"`
}
