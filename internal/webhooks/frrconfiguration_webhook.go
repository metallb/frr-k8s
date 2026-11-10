// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"errors"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/metallb/frr-k8s/api/v1beta1"
	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/lru"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	Logger        log.Logger
	WebhookClient client.Reader
	Validate      func(resources ...client.ObjectList) error
)

const (
	frrConfigWebhookPath = "/validate-frrk8s-metallb-io-v1beta1-frrconfiguration"
	healthPath           = "/healthz"
)

type FRRConfigValidator struct {
	ClusterResourceNamespace string

	client  client.Client
	decoder admission.Decoder
}

func (v *FRRConfigValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	v.client = mgr.GetClient()
	v.decoder = admission.NewDecoder(mgr.GetScheme())

	mgr.GetWebhookServer().Register(
		frrConfigWebhookPath,
		&webhook.Admission{Handler: v})

	mgr.GetWebhookServer().Register(
		healthPath,
		&healthHandler{})

	return nil
}

//+kubebuilder:webhook:verbs=create;update,path=/validate-frrk8s-metallb-io-v1beta1-frrconfiguration,mutating=false,failurePolicy=fail,groups=frrk8s.metallb.io,resources=frrconfigurations,versions=v1beta1,name=frrconfigurationsvalidationwebhook.metallb.io,sideEffects=None,admissionReviewVersions=v1

func (v *FRRConfigValidator) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	var config v1beta1.FRRConfiguration
	var oldConfig v1beta1.FRRConfiguration
	if req.Operation == v1.Delete {
		if err := v.decoder.DecodeRaw(req.OldObject, &config); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	} else {
		if err := v.decoder.Decode(req, &config); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		if req.OldObject.Size() > 0 {
			if err := v.decoder.DecodeRaw(req.OldObject, &oldConfig); err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			}
		}
	}

	var warnings []string
	switch req.Operation {
	case v1.Create:
		w, err := validateConfigCreate(&config)
		if err != nil {
			return admission.Denied(err.Error())
		}
		warnings = w
	case v1.Update:
		w, err := validateConfigUpdate(&config)
		if err != nil {
			return admission.Denied(err.Error())
		}
		warnings = w
	case v1.Delete:
		w, err := validateConfigDelete(&config)
		if err != nil {
			return admission.Denied(err.Error())
		}
		warnings = w
	}
	return admission.Allowed("").WithWarnings(warnings...)
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

type nodeAndConfigs struct {
	name   string
	labels map[string]string
	cfgs   *v1beta1.FRRConfigurationList
}

func validateConfigCreate(frrConfig *v1beta1.FRRConfiguration) ([]string, error) {
	level.Debug(Logger).Log("webhook", "frrconfiguration", "action", "create", "name", frrConfig.Name, "namespace", frrConfig.Namespace)
	defer level.Debug(Logger).Log("webhook", "frrconfiguration", "action", "end create", "name", frrConfig.Name, "namespace", frrConfig.Namespace)

	return validateConfig(frrConfig)
}

func validateConfigUpdate(frrConfig *v1beta1.FRRConfiguration) ([]string, error) {
	level.Debug(Logger).Log("webhook", "frrconfiguration", "action", "update", "name", frrConfig.Name, "namespace", frrConfig.Namespace)
	defer level.Debug(Logger).Log("webhook", "frrconfiguration", "action", "end update", "name", frrConfig.Name, "namespace", frrConfig.Namespace)

	return validateConfig(frrConfig)
}

func validateConfigDelete(_ *v1beta1.FRRConfiguration) ([]string, error) {
	return []string{}, nil
}

func validateConfig(frrConfig *v1beta1.FRRConfiguration) ([]string, error) {
	var warnings []string

	selector, err := getCachedSelector(frrConfig.Spec.NodeSelector)
	if err != nil {
		return warnings, errors.Join(err, errors.New("resource contains an invalid NodeSelector"))
	}

	existingNodes, err := getNodes()
	if err != nil {
		return warnings, err
	}

	if containsDisableMP(frrConfig.Spec.BGP.Routers) {
		warnings = append(warnings, "disableMP is deprecated, is ignored and will be removed in a future release")
	}

	existingFRRConfigurations, err := getFRRConfigurations()
	if err != nil {
		return warnings, err
	}

	matchingNodes, groupingWarnings := configsPerMatchingNode(frrConfig, selector, existingNodes, existingFRRConfigurations)
	warnings = append(warnings, groupingWarnings...)

	for _, n := range matchingNodes {
		err := Validate(n.cfgs)
		if err != nil {
			return warnings, errors.Join(err, fmt.Errorf("resource is invalid for node %s", n.name))
		}
	}

	return warnings, nil
}

// configsPerMatchingNode returns, for every node selected by frrConfig, the
// existing configurations that apply to that node followed by frrConfig itself,
// plus any warnings implied by replacing an existing configuration of the same
// name.
func configsPerMatchingNode(frrConfig *v1beta1.FRRConfiguration, nodeSelector labels.Selector, existingNodes []corev1.Node, existingConfigs *v1beta1.FRRConfigurationList) ([]nodeAndConfigs, []string) {
	var warnings []string

	matchingNodes := make([]nodeAndConfigs, 0, len(existingNodes))
	for _, n := range existingNodes {
		if nodeSelector.Matches(labels.Set(n.Labels)) {
			matchingNodes = append(matchingNodes, nodeAndConfigs{
				name:   n.Name,
				labels: n.Labels,
				cfgs:   &v1beta1.FRRConfigurationList{},
			})
		}
	}

	// Resolve each existing config's node selector once and fan it out over the
	// nodes it matches. Resolving per node re-derives the selector for every
	// (node, config) pair, so a config selecting every node multiplies that work
	// by the node count on every admission.
	for _, cfg := range existingConfigs.Items {
		if cfg.Name == frrConfig.Name {
			// shouldn't happen for creates as it would be considered an update, and in any case
			// we add the updated one at the end because for updates we don't want the old and updated resources
			// to be considered together.
			if len(matchingNodes) > 0 {
				warnings = append(warnings, gracefulRestartChangeWarnings(cfg, *frrConfig)...)
			}
			continue
		}

		cfgSelector, err := getCachedSelector(cfg.Spec.NodeSelector)
		if err != nil {
			// shouldn't happen as it would have been denied earlier, just in case.
			continue
		}

		for i := range matchingNodes {
			if cfgSelector.Matches(labels.Set(matchingNodes[i].labels)) {
				matchingNodes[i].cfgs.Items = append(matchingNodes[i].cfgs.Items, cfg)
			}
		}
	}

	for i := range matchingNodes {
		matchingNodes[i].cfgs.Items = append(matchingNodes[i].cfgs.Items, *frrConfig)
	}

	return matchingNodes, warnings
}

// gracefulRestartChangeWarnings reports a graceful restart change that only
// takes effect once FRR is restarted.
func gracefulRestartChangeWarnings(oldCfg, newCfg v1beta1.FRRConfiguration) []string {
	var warnings []string
	for _, rold := range oldCfg.Spec.BGP.Routers {
		for _, rnew := range newCfg.Spec.BGP.Routers {
			if rold.VRF != rnew.VRF {
				continue
			}
			for _, nold := range rold.Neighbors {
				for _, nnew := range rnew.Neighbors {
					if nold.ASN == nnew.ASN && nold.Address == nnew.Address && nold.EnableGracefulRestart != nnew.EnableGracefulRestart {
						warnings = append(warnings, "Graceful restart configuration changed, it will be available on the next restart")
					}
				}
			}
		}
	}
	return warnings
}

var getFRRConfigurations = func() (*v1beta1.FRRConfigurationList, error) {
	frrConfigurationsList := &v1beta1.FRRConfigurationList{}
	err := WebhookClient.List(context.Background(), frrConfigurationsList)
	if err != nil {
		return nil, errors.Join(err, errors.New("failed to get existing FRRConfiguration objects"))
	}
	return frrConfigurationsList, nil
}

var getNodes = func() ([]corev1.Node, error) {
	nodesList := &corev1.NodeList{}
	err := WebhookClient.List(context.Background(), nodesList)
	if err != nil {
		return nil, errors.Join(err, errors.New("failed to get existing Node objects"))
	}
	return nodesList.Items, nil
}

func containsDisableMP(routers []v1beta1.Router) bool {
	for _, r := range routers {
		for _, n := range r.Neighbors {
			//nolint:staticcheck // DisableMP is deprecated but still supported for backward compatibility
			if n.DisableMP {
				return true
			}
		}
	}
	return false
}

var (
	selectorCache = lru.New(300)
	selectorMutex sync.RWMutex
)

func getCachedSelector(nodeSelector metav1.LabelSelector) (labels.Selector, error) {
	key := fmt.Sprintf("%v-%v", nodeSelector.MatchLabels, nodeSelector.MatchExpressions)

	selectorMutex.RLock()
	if cached, ok := selectorCache.Get(key); ok {
		selectorMutex.RUnlock()
		return cached.(labels.Selector), nil
	}
	selectorMutex.RUnlock()

	selector, err := metav1.LabelSelectorAsSelector(&nodeSelector)
	if err != nil {
		return nil, err
	}

	selectorMutex.Lock()
	selectorCache.Add(key, selector)
	selectorMutex.Unlock()

	return selector, nil
}
