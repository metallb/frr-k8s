// SPDX-License-Identifier:Apache-2.0

package health

import (
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	Logger log.Logger
)

const (
	healthPath = "/healthz"
)

func SetupWebhookWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register(
		healthPath,
		&healthHandler{})

	return nil
}

type healthHandler struct{}

func (h *healthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte(`{"status": "ok"}`))
	if err != nil {
		level.Error(Logger).Log("webhook", "healthcheck", "error when writing reply", err)
	}
}
