package worker

import (
	"github.com/cirruslabs/chacha/pkg/localnetworkhelper"
	v1 "github.com/cirruslabs/orchard/pkg/resource/v1"
	"go.uber.org/zap"
)

type Option func(*Worker)

func WithName(name string) Option {
	return func(worker *Worker) {
		worker.name = name
	}
}

func WithResources(resources v1.Resources) Option {
	return func(worker *Worker) {
		worker.resources = resources
	}
}

func WithLabels(labels v1.Labels) Option {
	return func(worker *Worker) {
		worker.labels = labels
	}
}

func WithDefaultCPUAndMemory(defaultCPU uint64, defaultMemory uint64) Option {
	return func(worker *Worker) {
		worker.defaultCPU = defaultCPU
		worker.defaultMemory = defaultMemory
	}
}

func WithLocalNetworkHelper(localNetworkHelper *localnetworkhelper.LocalNetworkHelper) Option {
	return func(worker *Worker) {
		worker.localNetworkHelper = localNetworkHelper
	}
}

func WithLogger(logger *zap.Logger) Option {
	return func(worker *Worker) {
		worker.logger = logger.Sugar()
	}
}
