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

package rosa

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/measurements"
	machinev1beta1 "github.com/openshift/client-go/machine/clientset/versioned/typed/machine/v1beta1"
	log "github.com/sirupsen/logrus"
	wscale "github.com/vishnuchalla/workers-scale/workerscale"
	core "github.com/vishnuchalla/workers-scale/workerscale/core"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RosaScenario struct{}

func (rosaScenario *RosaScenario) OrchestrateWorkload(scaleConfig wscale.ScaleConfig) string {
	var err error
	var triggerJob string
	var clusterID string
	var hcNamespace string
	var machineClient interface{}
	var triggerTime time.Time

	kubeClientProvider := config.NewKubeClientProvider("", "")
	clientSet, restConfig := kubeClientProvider.ClientSet(0, 0)
	dynamicClient := dynamic.NewForConfigOrDie(restConfig)
	clusterID = getClusterID(dynamicClient, scaleConfig.IsHCP)
	if scaleConfig.IsHCP {
		if scaleConfig.MCKubeConfig == "" {
			log.Fatal("Error reading management cluster kubeconfig. Please provide a valid path")
		}
		mcKubeClientProvider := config.NewKubeClientProvider(scaleConfig.MCKubeConfig, "")
		mcClientSet, mcRestConfig := mcKubeClientProvider.ClientSet(0, 0)
		machineClient = wscale.GetCAPIClient(mcRestConfig)
		hcNamespace = wscale.GetHCNamespace(mcClientSet, clusterID)
	} else {
		machineClient = wscale.GetMachineClient(restConfig)
	}

	if scaleConfig.ScaleEventEpoch != 0 && !scaleConfig.AutoScalerEnabled {
		log.Info("Scale event epoch time specified. Hence calculating node latencies without any scaling")
		wscale.SetupMetrics(scaleConfig.UUID, scaleConfig.Metadata, kubeClientProvider)
		measurements.Start()
		if err = wscale.WaitForNodes(clientSet); err != nil {
			log.Fatalf("Error waiting for nodes: %v", err)
		}
		scaledMachineDetails, amiID := getMachineDetails(machineClient, scaleConfig.ScaleEventEpoch, clusterID, hcNamespace, scaleConfig.IsHCP)
		if err := measurements.Stop(); err != nil {
			log.Error(err.Error())
		}
		wscale.FinalizeMetrics(&sync.Map{}, scaledMachineDetails, scaleConfig.Metadata, scaleConfig.Indexer, amiID, scaleConfig.ScaleEventEpoch)
		return amiID
	} else {
		verifyRosaInstall()
		var machinePools []wscale.MachinePool
		cmd := exec.Command("rosa", "list", "machinepool", "--cluster", clusterID, "--output", "json")
		mpOutput, err := cmd.CombinedOutput()
		if err != nil {
			log.Fatalf("Unable to list machinepools: %v. Output: %s", err, string(mpOutput))
		}
		err = json.Unmarshal(mpOutput, &machinePools)
		if err != nil {
			log.Fatalf("Error parsing JSON output: %v\n", err)
		}
		prevMachineDetails, _ := getMachineDetails(machineClient, 0, clusterID, hcNamespace, scaleConfig.IsHCP)
		wscale.SetupMetrics(scaleConfig.UUID, scaleConfig.Metadata, kubeClientProvider)
		measurements.Start()

		triggerTime = editMachinepool(clusterID, machinePools, scaleConfig.AdditionalWorkerNodes, scaleConfig.AutoScalerEnabled)
		if scaleConfig.AutoScalerEnabled {
			triggerJob, triggerTime = core.CreateBatchJob(clientSet)
			// Slightly more delay for the cluster autoscaler resources to come up
			time.Sleep(5 * time.Minute)
		}
		log.Info("Waiting for the machinesets to be ready")
		if err = waitForWorkers(machineClient, clusterID, hcNamespace, scaleConfig.IsHCP); err != nil {
			log.Fatalf("Error waiting for MachineSets to be ready: %v", err)
		}
		scaledMachineDetails, amiID := getMachineDetails(machineClient, 0, clusterID, hcNamespace, scaleConfig.IsHCP)
		wscale.DiscardPreviousMachines(prevMachineDetails, scaledMachineDetails)
		if err := measurements.Stop(); err != nil {
			log.Error(err.Error())
		}
		wscale.FinalizeMetrics(&sync.Map{}, scaledMachineDetails, scaleConfig.Metadata, scaleConfig.Indexer, amiID, triggerTime.Unix())
		if scaleConfig.AutoScalerEnabled {
			core.DeleteBatchJob(clientSet, triggerJob)
			time.Sleep(1 * time.Minute)
		}
		if scaleConfig.GC {
			log.Info("Restoring machine pool to previous state")
			editMachinepool(clusterID, machinePools, 0, scaleConfig.AutoScalerEnabled)
			log.Info("Waiting for the machinesets to scale down")
			if err = waitForWorkers(machineClient, clusterID, hcNamespace, scaleConfig.IsHCP); err != nil {
				log.Fatalf("Error waiting for MachineSets to scale down: %v", err)
			}
		}
		return amiID
	}
}

// editMachinepool edits machinepool to desired replica count
func editMachinepool(clusterID string, machinePools []wscale.MachinePool, additionalWorkerNodes int, autoScalerEnabled bool) time.Time {
	quotient := additionalWorkerNodes / len(machinePools)
	remainder := additionalWorkerNodes % len(machinePools)
	if additionalWorkerNodes == 0 {
		quotient = 0
		remainder = 0
	}
	for _, machinePool := range machinePools {
		cmdArgs := []string{"edit", "machinepool", "-c", clusterID, machinePool.ID, fmt.Sprintf("--enable-autoscaling=%t", autoScalerEnabled)}
		if autoScalerEnabled {
			cmdArgs = append(cmdArgs, fmt.Sprintf("--min-replicas=%d", machinePool.Replicas))
			cmdArgs = append(cmdArgs, fmt.Sprintf("--max-replicas=%d", machinePool.Replicas+quotient+(remainder&1)))
		} else {
			cmdArgs = append(cmdArgs, fmt.Sprintf("--replicas=%d", machinePool.Replicas+quotient+(remainder&1)))
		}
		cmd := exec.Command("bash", "-c", "rosa"+" "+strings.Join(cmdArgs, " "))
		editOutput, err := cmd.CombinedOutput()
		if err != nil {
			log.Fatalf("Failed to edit machinepool: %v. Output: %s", err, string(editOutput))
		}
		log.Infof("Machinepool %v edited successfully on cluster: %v", machinePool.ID, clusterID)
		log.Debug(string(editOutput))
		if remainder > 0 {
			remainder--
		}
	}
	triggerTime := time.Now().UTC().Truncate(time.Second)
	time.Sleep(1 * time.Minute)
	return triggerTime
}

// verifyRosaInstall verifies rosa installation and login
func verifyRosaInstall() {
	if _, err := exec.LookPath("rosa"); err != nil {
		log.Fatal("ROSA CLI is not installed. Please install it and retry.")
		return
	}
	log.Info("ROSA CLI is installed.")

	cmd := exec.Command("bash", "-c", "rosa whoami")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Fatalf("You are not logged in. Please login using 'rosa login' and retry: %v. Output: %s", err, string(output))
	}
	log.Info("You are already logged in.")
	log.Debug(string(output))
}

// getClusterID fetches the clusterID
func getClusterID(dynamicClient dynamic.Interface, mcPrescence bool) string {
	clusterVersionGVR := schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusterversions",
	}

	clusterVersion, err := dynamicClient.Resource(clusterVersionGVR).Get(context.TODO(), "version", metav1.GetOptions{})
	if err != nil {
		log.Fatalf("Error fetching cluster version: %v", err)
	}

	clusterID, found, err := unstructured.NestedString(clusterVersion.Object, "spec", "clusterID")
	if err != nil || !found {
		log.Fatalf("Error retrieving cluster ID: %v", err)
	}

	// Special case for hcp where cluster version object has external ID
	if mcPrescence {
		cmd := exec.Command("rosa", "describe", "cluster", "-c", clusterID, "-o", "json")
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Fatalf("Failed to describe cluster: %v. Output: %s", err, string(output))
		}
		var result map[string]interface{}
		if err := json.Unmarshal(output, &result); err != nil {
			log.Fatalf("Failed to parse JSON: %v", err)
		}
		actualID, ok := result["id"].(string)
		if !ok {
			log.Fatal("ID field not found or invalid in the output.")
		}
		return actualID
	}
	return clusterID
}

// Function to fetch machine details based on the scenario (standard Rosa or RosaHCP).
func getMachineDetails(machineClient interface{}, epoch int64, clusterID string, hcNamespace string, isHCP bool) (map[string]wscale.MachineInfo, string) {
	if isHCP {
		return wscale.GetCapiMachines(machineClient.(client.Client), epoch, clusterID, hcNamespace)
	}
	return wscale.GetMachines(machineClient.(*machinev1beta1.MachineV1beta1Client), epoch)
}

// Function to wait for worker MachineSets based on the scenario (standard Rosa or RosaHCP).
func waitForWorkers(machineClient interface{}, clusterID string, hcNamespace string, isHCP bool) error {
	if isHCP {
		return wscale.WaitForCAPIMachineSets(machineClient.(client.Client), clusterID, hcNamespace)
	}
	return wscale.WaitForWorkerMachineSets(machineClient.(*machinev1beta1.MachineV1beta1Client))
}
