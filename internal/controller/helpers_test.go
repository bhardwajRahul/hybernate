/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1alpha1 "github.com/okedeji/hybernate/api/v1alpha1"
	"github.com/okedeji/hybernate/internal/signal"
)

func TestAppendUserSignals_PrometheusQueriesConfiguredEndpoint(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotQuery = req.URL.Query().Get("query")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {"resultType": "vector", "result": [{"metric": {}, "value": [1234567890, "1"]}]}
		}`))
	}))
	defer srv.Close()

	r := &Reconciler{prometheusURL: srv.URL}
	checkers := r.appendUserSignals(nil, []v1alpha1.ProbeSpec{
		{Source: v1alpha1.ProbeSourcePrometheus, PromQL: `sum(rate(http_requests_total[10m])) == 0`},
	})
	require.Len(t, checkers, 1)

	res, err := checkers[0].Check(context.Background(), "staging", "api")

	require.NoError(t, err)
	assert.True(t, res.Confirm)
	assert.Equal(t, `sum(rate(http_requests_total[10m])) == 0`, gotQuery)
}

func TestAppendUserSignals_PrometheusWithoutEndpointReturnsError(t *testing.T) {
	r := &Reconciler{}
	checkers := r.appendUserSignals(nil, []v1alpha1.ProbeSpec{
		{Source: v1alpha1.ProbeSourcePrometheus, PromQL: `up`},
	})
	require.Len(t, checkers, 1)

	_, err := checkers[0].Check(context.Background(), "staging", "api")

	require.ErrorIs(t, err, signal.ErrEndpointNotConfigured)
}
