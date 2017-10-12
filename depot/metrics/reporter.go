package metrics

import (
	"os"
	"time"

	"code.cloudfoundry.org/clock"
	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/lager"
)

const (
	totalMemory     = "CapacityTotalMemory"
	totalDisk       = "CapacityTotalDisk"
	totalContainers = "CapacityTotalContainers"

	remainingMemory     = "CapacityRemainingMemory"
	remainingDisk       = "CapacityRemainingDisk"
	remainingContainers = "CapacityRemainingContainers"

	allocatedMemory = "CapacityAllocatedMemory"
	allocatedDisk   = "CapacityAllocatedDisk"

	containerUsageMemory = "ContainerUsageMemory"
	containerUsageDisk   = "ContainerUsageDisk"

	containerCount         = "ContainerCount"
	startingContainerCount = "StartingContainerCount"
)

type ExecutorSource interface {
	GetBulkMetrics(logger lager.Logger) (map[string]executor.Metrics, error)
	RemainingResources(lager.Logger) (executor.ExecutorResources, error)
	TotalResources(lager.Logger) (executor.ExecutorResources, error)
	ListContainers(lager.Logger) ([]executor.Container, error)
}

type Reporter struct {
	Interval       time.Duration
	ExecutorSource ExecutorSource
	Clock          clock.Clock
	Logger         lager.Logger
	MetronClient   loggingclient.IngressClient
}

func (reporter *Reporter) Run(signals <-chan os.Signal, ready chan<- struct{}) error {
	logger := reporter.Logger.Session("metrics-reporter")

	close(ready)

	timer := reporter.Clock.NewTimer(reporter.Interval)

	for {
		select {
		case <-signals:
			logger.Info("signalled")
			return nil

		case <-timer.C():
			var allocatedMemoryMB, allocatedDiskMB, containerUsageDiskMB, containerUsageMemoryMB int

			remainingCapacity, err := reporter.ExecutorSource.RemainingResources(logger)
			if err != nil {
				reporter.Logger.Error("failed-remaining-resources", err)
				remainingCapacity.Containers = -1
				remainingCapacity.DiskMB = -1
				remainingCapacity.MemoryMB = -1
				allocatedDiskMB = -1
				allocatedMemoryMB = -1
			}

			totalCapacity, err := reporter.ExecutorSource.TotalResources(logger)
			if err != nil {
				reporter.Logger.Error("failed-total-resources", err)
				totalCapacity.Containers = -1
				totalCapacity.DiskMB = -1
				totalCapacity.MemoryMB = -1
				allocatedDiskMB = -1
				allocatedMemoryMB = -1
			}

			if allocatedDiskMB == 0 && allocatedMemoryMB == 0 {
				allocatedDiskMB = totalCapacity.DiskMB - remainingCapacity.DiskMB
				allocatedMemoryMB = totalCapacity.MemoryMB - remainingCapacity.MemoryMB
			}

			bulkMetrics, err := reporter.ExecutorSource.GetBulkMetrics(logger)
			if err != nil {
				reporter.Logger.Error("failed-bulk-metrics", err)
				containerUsageDiskMB = -1
				containerUsageMemoryMB = -1
			} else {
				containerUsageMemoryMB, containerUsageDiskMB = calculateUsageMetrics(bulkMetrics)
			}

			var nContainers, startingCount int
			containers, err := reporter.ExecutorSource.ListContainers(logger)
			if err != nil {
				reporter.Logger.Error("failed-to-list-containers", err)
				nContainers = -1
			} else {
				nContainers = len(containers)
				for _, c := range containers {
					if containerIsStarting(c) {
						startingCount++
					}
				}
			}

			err = reporter.MetronClient.SendMebiBytes(totalMemory, totalCapacity.MemoryMB)
			if err != nil {
				logger.Error("failed-to-send-total-memory-metric", err)
			}
			err = reporter.MetronClient.SendMebiBytes(totalDisk, totalCapacity.DiskMB)
			if err != nil {
				logger.Error("failed-to-send-total-disk-metric", err)
			}
			err = reporter.MetronClient.SendMetric(totalContainers, totalCapacity.Containers)
			if err != nil {
				logger.Error("failed-to-send-total-container-metric", err)
			}

			err = reporter.MetronClient.SendMebiBytes(remainingMemory, remainingCapacity.MemoryMB)
			if err != nil {
				logger.Error("failed-to-send-remaining-memory-metric", err)
			}
			err = reporter.MetronClient.SendMebiBytes(remainingDisk, remainingCapacity.DiskMB)
			if err != nil {
				logger.Error("failed-to-send-remaining-disk-metric", err)
			}
			err = reporter.MetronClient.SendMetric(remainingContainers, remainingCapacity.Containers)
			if err != nil {
				logger.Error("failed-to-send-remaining-containers-metric", err)
			}

			err = reporter.MetronClient.SendMebiBytes(allocatedMemory, allocatedMemoryMB)
			if err != nil {
				logger.Error("failed-to-send-allocated-memory-metric", err)
			}
			err = reporter.MetronClient.SendMebiBytes(allocatedDisk, allocatedDiskMB)
			if err != nil {
				logger.Error("failed-to-send-allocated-disk-metric", err)
			}

			err = reporter.MetronClient.SendMebiBytes(containerUsageMemory, containerUsageMemoryMB)
			if err != nil {
				logger.Error("failed-to-send-container-memory-metric", err)
			}
			err = reporter.MetronClient.SendMebiBytes(containerUsageDisk, containerUsageDiskMB)
			if err != nil {
				logger.Error("failed-to-send-container-disk-metric", err)
			}

			err = reporter.MetronClient.SendMetric(containerCount, nContainers)
			if err != nil {
				logger.Error("failed-to-send-container-count-metric", err)
			}

			err = reporter.MetronClient.SendMetric(startingContainerCount, startingCount)
			if err != nil {
				logger.Error("failed-to-send-starting-container-count-metric", err)
			}

			timer.Reset(reporter.Interval)
		}
	}
}

func containerIsStarting(container executor.Container) bool {
	return container.State == executor.StateReserved ||
		container.State == executor.StateInitializing ||
		container.State == executor.StateCreated
}

func bytesToMebibytes(bytes int) int {
	return bytes / 1024 / 1024
}

func calculateUsageMetrics(metrics map[string]executor.Metrics) (int, int) {
	var memUsageMB, diskUsageMB int
	for _, m := range metrics {
		memUsageMB += bytesToMebibytes(int(m.MemoryUsageInBytes))
		diskUsageMB += bytesToMebibytes(int(m.DiskUsageInBytes))
	}
	return memUsageMB, diskUsageMB
}
