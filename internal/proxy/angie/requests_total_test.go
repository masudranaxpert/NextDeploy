package angie

import (
	"context"
	"testing"
)

func TestRequestsTotalSumsServerZones(t *testing.T) {
	d := New(WithRuntimeHooks(RuntimeHooks{
		Exec: func(context.Context, string, ...string) (string, error) {
			return `{"angie":{"version":"1.12.4"},"http":{"server_zones":{
				"site_a":{"requests":{"total":3210,"processing":1}},
				"panel":{"requests":{"total":90}}}}}`, nil
		},
	}))
	got, err := d.RequestsTotal(context.Background())
	if err != nil || got != 3300 {
		t.Fatalf("RequestsTotal = %d, %v; want 3300", got, err)
	}
}
