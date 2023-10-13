package metricadapters

import (
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/metrics"
	"k8s.io/client-go/util/workqueue"
	leaderelectionmetrics "k8s.io/component-base/metrics/prometheus/clientgo/leaderelection"
	restclientmetrics "k8s.io/component-base/metrics/prometheus/restclient"
	workqueuemetrics "k8s.io/component-base/metrics/prometheus/workqueue"
)

func SetDefaultMetricAdapters() {
	leaderelection.SetProvider(&leaderElectionMetricsProvider{})
	metrics.Register(metrics.RegisterOpts{
		ClientCertExpiry:      execPluginCertTTLAdapter,
		ClientCertRotationAge: restclientmetrics.NewClientCertRotationAgeMetric(),
		RequestLatency:        restclientmetrics.NewRequestLatencyMetric(),
		ResolverLatency:       restclientmetrics.NewResolverLatencyMetric(),
		RequestSize:           restclientmetrics.NewRequestSizeMetric(),
		ResponseSize:          restclientmetrics.NewResponseSizeMetric(),
		RateLimiterLatency:    restclientmetrics.NewRateLimiterLatencyMetric(),
		RequestResult:         restclientmetrics.NewRequestResultMetric(),
		RequestRetry:          restclientmetrics.NewRequestRetryMetric(),
		ExecPluginCalls:       restclientmetrics.NewExecPluginCallsMetric(),
		TransportCacheEntries: restclientmetrics.NewTransportCacheEntriesMetric(),
		TransportCreateCalls:  restclientmetrics.NewTransportCacheCallsMetric(),
	})
	workqueue.SetProvider(&workqueueMetricsProvider{})
}

type leaderElectionMetricsProvider struct{}

func (p *leaderElectionMetricsProvider) NewLeaderMetric() leaderelection.SwitchMetric {
	return leaderelectionmetrics.NewLeaderMetric()
}

type workqueueMetricsProvider struct{}

func (*workqueueMetricsProvider) NewAddsMetric(name string) workqueue.CounterMetric {
	return workqueuemetrics.NewAddsMetric(name)
}

func (*workqueueMetricsProvider) NewDepthMetric(name string) workqueue.GaugeMetric {
	return workqueuemetrics.NewDepthMetric(name)
}

func (*workqueueMetricsProvider) NewLatencyMetric(name string) workqueue.HistogramMetric {
	return workqueuemetrics.NewLatencyMetric(name)
}

func (*workqueueMetricsProvider) NewLongestRunningProcessorSecondsMetric(name string) workqueue.SettableGaugeMetric {
	return workqueuemetrics.NewLongestRunningProcessorSecondsMetric(name)
}

func (*workqueueMetricsProvider) NewRetriesMetric(name string) workqueue.CounterMetric {
	return workqueuemetrics.NewRetriesMetric(name)
}

func (*workqueueMetricsProvider) NewUnfinishedWorkSecondsMetric(name string) workqueue.SettableGaugeMetric {
	return workqueuemetrics.NewUnfinishedWorkSecondsMetric(name)
}

func (*workqueueMetricsProvider) NewWorkDurationMetric(name string) workqueue.HistogramMetric {
	return workqueuemetrics.NewWorkDurationMetric(name)
}
