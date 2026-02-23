package ruledforward

import (
	"github.com/coredns/coredns/plugin"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "ruledforward",
		Name:      "requests_total",
		Help:      "Counter of requests handled by ruledforward, per group and action.",
	}, []string{"group", "action"})

	noMatchTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "ruledforward",
		Name:      "no_match_total",
		Help:      "Counter of requests that did not match any group and were passed to the next plugin.",
	})

	forwardUpstreamFailTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: plugin.Namespace,
		Subsystem: "ruledforward",
		Name:      "forward_upstream_fail_total",
		Help:      "Counter of forward groups where all upstreams failed for a request.",
	}, []string{"group"})
)
