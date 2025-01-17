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

package core

import (
	"sort"
	"sync"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements"
	log "github.com/sirupsen/logrus"
	wscale "github.com/vishnuchalla/workers-scale/workerscale"
)

type BaseScenario struct{}

// Returns a new scenario object
func (awsScenario *BaseScenario) OrchestrateWorkload(scaleConfig wscale.ScaleConfig) string {
	var err error
	kubeClientProvider := config.NewKubeClientProvider("", "")
	clientSet, restConfig := kubeClientProvider.ClientSet(0, 0)
	machineClient := wscale.GetMachineClient(restConfig)
	if scaleConfig.ScaleEventEpoch != 0 {
		log.Info("Scale event epoch time specified. Hence calculating node latencies without any scaling")
		wscale.SetupMetrics(scaleConfig.UUID, scaleConfig.Metadata, kubeClientProvider)
		measurements.Start()
		if err := wscale.WaitForNodes(clientSet); err != nil {
			log.Fatalf("Error waiting for nodes: %v", err)
		}
		if err = measurements.Stop(); err != nil {
			log.Error(err.Error())
		}
		scaledMachineDetails, amiID := wscale.GetMachines(machineClient, scaleConfig.ScaleEventEpoch)
		wscale.FinalizeMetrics(&sync.Map{}, scaledMachineDetails, scaleConfig.Metadata, scaleConfig.Indexer, amiID, scaleConfig.ScaleEventEpoch)
		return amiID
	} else {
		machineSetDetails := wscale.GetMachinesets(machineClient)
		prevMachineDetails, _ := wscale.GetMachines(machineClient, 0)
		wscale.SetupMetrics(scaleConfig.UUID, scaleConfig.Metadata, kubeClientProvider)
		measurements.Start()
		machineSetsToEdit := adjustMachineSets(machineSetDetails, scaleConfig.AdditionalWorkerNodes)
		log.Info("Updating machinessets evenly to reach desired count")
		wscale.EditMachineSets(machineClient, clientSet, machineSetsToEdit, true)
		if err = measurements.Stop(); err != nil {
			log.Error(err.Error())
		}
		scaledMachineDetails, amiID := wscale.GetMachines(machineClient, 0)
		wscale.DiscardPreviousMachines(prevMachineDetails, scaledMachineDetails)
		wscale.FinalizeMetrics(machineSetsToEdit, scaledMachineDetails, scaleConfig.Metadata, scaleConfig.Indexer, amiID, scaleConfig.ScaleEventEpoch)
		if scaleConfig.GC {
			log.Info("Restoring machine sets to previous state")
			wscale.EditMachineSets(machineClient, clientSet, machineSetsToEdit, false)
		}
		return amiID
	}
}

// adjustMachineSets equally spreads requested number of machines across machinesets
func adjustMachineSets(machineSetReplicas map[int][]string, desiredWorkerCount int) *sync.Map {
	var lastIndex int
	machineSetsToEdit := sync.Map{}
	var keys []int
	for key := range machineSetReplicas {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	index := 0
	for index < len(keys) {
		modified := false
		value := keys[index]
		if machineSets, exists := machineSetReplicas[value]; exists {
			for index, machineSet := range machineSets {
				if desiredWorkerCount > 0 {
					if _, exists := machineSetsToEdit.Load(machineSet); !exists {
						machineSetsToEdit.Store(machineSet, wscale.MachineSetInfo{
							PrevReplicas:    value,
							CurrentReplicas: value + 1,
						})
					}
					msValue, _ := machineSetsToEdit.Load(machineSet)
					msInfo := msValue.(wscale.MachineSetInfo)
					msInfo.CurrentReplicas = value + 1
					machineSetsToEdit.Store(machineSet, msInfo)
					machineSetReplicas[value+1] = append(machineSetReplicas[value+1], machineSet)
					lastIndex = index
					desiredWorkerCount--
					modified = true
				} else {
					break
				}
			}
			if lastIndex == len(machineSets)-1 {
				delete(machineSetReplicas, value)
			} else {
				machineSetReplicas[value] = machineSets[lastIndex+1:]
			}
		}
		if modified && (index == len(keys)-1) || (value+1 != keys[index+1]) {
			keys = append(keys[:index+1], append([]int{value + 1}, keys[index+1:]...)...)
		}
		if desiredWorkerCount <= 0 {
			break
		}
		index++
	}
	return &machineSetsToEdit
}
