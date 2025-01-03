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

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/cloud-bulldozer/go-commons/indexers"
	ocpmetadata "github.com/cloud-bulldozer/go-commons/ocp-metadata"
	"github.com/cloud-bulldozer/go-commons/version"
	uid "github.com/google/uuid"
	"github.com/kube-burner/kube-burner/pkg/burner"
	"github.com/kube-burner/kube-burner/pkg/config"
	"github.com/kube-burner/kube-burner/pkg/prometheus"
	"github.com/kube-burner/kube-burner/pkg/util"
	"github.com/kube-burner/kube-burner/pkg/util/metrics"
	"github.com/kube-burner/kube-burner/pkg/workloads"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	wscale "github.com/vishnuchalla/workers-scale/workerscale"
	core "github.com/vishnuchalla/workers-scale/workerscale/core"
	platforms "github.com/vishnuchalla/workers-scale/workerscale/platforms"
)

// rootCmd represents the base command when called without any subcommands
var err error
var enableAutoscaler, isHCP, gc bool
var uuid, mcKubeConfig string
var metricsProfiles []string
var prometheusStep time.Duration
var scaleEventEpoch, start, end int64
var rc, additionalWorkerNodes int
var prometheusURL, prometheusToken string
var userMetadata, metricsDirectory, tarballName string
var indexer config.MetricsEndpoint
var clusterMetadataMap map[string]interface{}
var ocpMetaAgent ocpmetadata.Metadata
var metricsEndpoint string
var esServer, esIndex string

const autoScaled = "autoScaled"
const imageID = "imageId"

var rootCmd = &cobra.Command{
	Use:   "workers-scale",
	Short: "Scales openshift worker nodes and captures time spent",
	Long:  `Utility to scale our openshift cluster's worker nodes to a desired count and capture their bootup times as day 2 operation`,
	PreRun: func(cmd *cobra.Command, args []string) {
		if cmd.Name() == "version" {
			return
		}
		util.ConfigureLogging(cmd)
	},
	Run: func(cmd *cobra.Command, args []string) {
		if start == 0 {
			start = time.Now().Unix()
		}
		jobEnd := end
		uuid, _ = cmd.Flags().GetString("uuid")
		kubeClientProvider := config.NewKubeClientProvider("", "")
		_, restConfig := kubeClientProvider.DefaultClientSet()
		ocpMetaAgent, err = ocpmetadata.NewMetadata(restConfig)
		workloads.ConfigSpec.GlobalConfig.UUID = uuid
		// When metricsEndpoint is specified, don't fetch any prometheus token
		if metricsEndpoint == "" {
			prometheusURL, prometheusToken, err = ocpMetaAgent.GetPrometheus()
			if err != nil {
				log.Fatal("Error obtaining prometheus information from cluster: ", err.Error())
			}
		}
		indexer = config.MetricsEndpoint{
			Endpoint:      prometheusURL,
			Token:         prometheusToken,
			Step:          prometheusStep,
			Metrics:       metricsProfiles,
			SkipTLSVerify: true,
		}
		if esServer != "" && esIndex != "" {
			indexer.IndexerConfig = indexers.IndexerConfig{
				Type:    indexers.ElasticIndexer,
				Servers: []string{esServer},
				Index:   esIndex,
			}
		} else {
			if metricsDirectory == "collected-metrics" {
				metricsDirectory = metricsDirectory + "-" + uuid
			}
			indexer.IndexerConfig = indexers.IndexerConfig{
				Type:             indexers.LocalIndexer,
				MetricsDirectory: metricsDirectory,
				TarballName:      tarballName,
			}
		}

		clusterMetadata, err := ocpMetaAgent.GetClusterMetadata()
		if scaleEventEpoch == 0 {
			clusterMetadata.WorkerNodesCount += additionalWorkerNodes
			clusterMetadata.TotalNodes += additionalWorkerNodes
		}
		if err != nil {
			log.Fatal("Error obtaining clusterMetadata: ", err.Error())
		}
		metadata := make(map[string]interface{})
		jsonData, _ := json.Marshal(clusterMetadata)
		json.Unmarshal(jsonData, &clusterMetadataMap)
		for k, v := range clusterMetadataMap {
			metadata[k] = v
		}
		if enableAutoscaler {
			metadata[autoScaled] = true
		} else {
			metadata[autoScaled] = false
		}
		workloads.ConfigSpec.MetricsEndpoints = append(workloads.ConfigSpec.MetricsEndpoints, indexer)
		metricsScraper := metrics.ProcessMetricsScraperConfig(metrics.ScraperConfig{
			ConfigSpec:      &workloads.ConfigSpec,
			MetricsEndpoint: metricsEndpoint,
			UserMetaData:    userMetadata,
			MetricsMetadata: map[string]interface{}{
				"ocpMajorVersion": clusterMetadata.OCPMajorVersion,
				"ocpVersion":      clusterMetadata.OCPVersion,
			},
			SummaryMetadata: metadata,
		})
		var indexerValue indexers.Indexer
		for _, value := range metricsScraper.IndexerList {
			indexerValue = value
			break
		}
		scenario := fetchScenario(enableAutoscaler, clusterMetadata)
		if _, ok := scenario.(*platforms.RosaScenario); ok {
			if clusterMetadata.MasterNodesCount == 0 && clusterMetadata.InfraNodesCount == 0 {
				isHCP = true
			}
		} else {
			isHCP = false
		}
		if isHCP {
			metricsScraper.SummaryMetadata[wscale.ClusterType] = wscale.RosaHCP
		}
		imageId := scenario.OrchestrateWorkload(wscale.ScaleConfig{
			UUID:                  uuid,
			AdditionalWorkerNodes: additionalWorkerNodes,
			Metadata:              metricsScraper.MetricsMetadata,
			Indexer:               indexerValue,
			GC:                    gc,
			ScaleEventEpoch:       scaleEventEpoch,
			AutoScalerEnabled:     enableAutoscaler,
			MCKubeConfig:          mcKubeConfig,
			IsHCP:                 isHCP,
		})
		metricsScraper.SummaryMetadata[imageID] = imageId
		if end == 0 {
			jobEnd = time.Now().Unix()
			end = jobEnd + wscale.TenMinutes
		} else {
			end += wscale.TenMinutes
		}
		for _, prometheusClient := range metricsScraper.PrometheusClients {
			prometheusJob := prometheus.Job{
				Start: time.Unix(start, 0),
				End:   time.Unix(end, 0),
				JobConfig: config.Job{
					Name: wscale.JobName,
				},
			}
			if prometheusClient.ScrapeJobsMetrics(prometheusJob) != nil {
				rc = 1
			}
		}
		if workloads.ConfigSpec.MetricsEndpoints[0].Type == indexers.LocalIndexer && tarballName != "" {
			if err := metrics.CreateTarball(workloads.ConfigSpec.MetricsEndpoints[0].IndexerConfig); err != nil {
				log.Fatal(err)
			}
		}
		jobSummary := burner.JobSummary{
			Timestamp:    time.Unix(start, 0).UTC(),
			EndTimestamp: time.Unix(jobEnd, 0).UTC(),
			ElapsedTime:  time.Unix(jobEnd, 0).UTC().Sub(time.Unix(start, 0).UTC()).Round(time.Second).Seconds(),
			UUID:         uuid,
			JobConfig: config.Job{
				Name: wscale.JobName,
			},
			Metadata:   metricsScraper.SummaryMetadata,
			MetricName: "jobSummary",
			Version:    fmt.Sprintf("%v@%v", version.Version, version.GitCommit),
			Passed:     rc == 0,
		}
		burner.IndexJobSummary([]burner.JobSummary{jobSummary}, indexerValue)
		log.Info("👋 Exiting workers-scale ", uuid)
		os.Exit(rc)
	},
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.workers-scale.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().StringSliceVar(&metricsProfiles, "metrics-profile", []string{"metrics-nodebootup.yml", "metrics-nodebootup-report.yml"}, "Comma separated list of metrics profiles to use")
	rootCmd.Flags().StringVar(&metricsEndpoint, "metrics-endpoint", "", "YAML file with a list of metric endpoints, overrides the es-server and es-index flags")
	rootCmd.Flags().Int64Var(&start, "start", 0, "Epoch start time")
	rootCmd.Flags().Int64Var(&end, "end", 0, "Epoch end time")
	rootCmd.Flags().StringVar(&esServer, "es-server", "", "Elastic Search endpoint")
	rootCmd.Flags().StringVar(&esIndex, "es-index", "", "Elastic Search index")
	rootCmd.Flags().StringVar(&uuid, "uuid", uid.NewString(), "Benchmark UUID")
	rootCmd.Flags().BoolVar(&gc, "gc", true, "Garbage collect created resources")
	rootCmd.Flags().StringVar(&metricsDirectory, "metrics-directory", "collected-metrics", "Directory to dump the metrics files in, when using default local indexing")
	rootCmd.Flags().StringVar(&mcKubeConfig, "mc-kubeconfig", "", "Path for management cluster kubeconfig")
	rootCmd.Flags().DurationVar(&prometheusStep, "step", 30*time.Second, "Prometheus step size")
	rootCmd.Flags().IntVar(&additionalWorkerNodes, "additional-worker-nodes", 3, "Additional workers to scale")
	rootCmd.Flags().BoolVar(&enableAutoscaler, "enable-autoscaler", false, "Enables autoscaler while scaling the cluster")
	rootCmd.Flags().Int64Var(&scaleEventEpoch, "scale-event-epoch", 0, "Scale event epoch time")
	rootCmd.Flags().StringVar(&userMetadata, "user-metadata", "", "User provided metadata file, in YAML format")
	rootCmd.Flags().StringVar(&tarballName, "tarball-name", "", "Dump collected metrics into a tarball with the given name, requires local indexing")
	rootCmd.Flags().SortFlags = false
	util.SetupCmd(rootCmd)
}

// FetchScenario helps us to fetch relevant class
func fetchScenario(enableAutoscaler bool, clusterMetadata ocpmetadata.ClusterMetadata) wscale.Scenario {
	if clusterMetadata.ClusterType == "rosa" {
		return &platforms.RosaScenario{}
	} else {
		if enableAutoscaler {
			return &core.AutoScalerScenario{}
		}
		return &core.BaseScenario{}
	}
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
