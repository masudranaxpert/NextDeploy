package sysinfo

import (
	"context"
	"testing"
)

func TestDriverCounterWinsAndFallsBack(t *testing.T) {
	SetProxyMetricsEndpoints([]string{"http://127.0.0.1:1/none"})
	defer SetProxyMetricsEndpoints(nil)

	SetProxyRequestsTotalFunc(func(context.Context) (uint64, bool) { return 42, true })
	if got, ok := queryProxyRequestsTotal(); !ok || got != 42 {
		t.Fatalf("driver counter not used: %d %v", got, ok)
	}
	SetProxyRequestsTotalFunc(func(context.Context) (uint64, bool) { return 0, false })
	if _, ok := queryProxyRequestsTotal(); ok {
		t.Fatal("no driver counter and no endpoint must report nothing")
	}
	SetProxyRequestsTotalFunc(nil)
}
