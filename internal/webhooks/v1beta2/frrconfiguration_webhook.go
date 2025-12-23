// SPDX-License-Identifier:Apache-2.0

package v1beta2

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"errors"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	apiv1beta2 "github.com/metallb/frr-k8s/api/v1beta2"
	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
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
	frrConfigWebhookPath    = "/validate-frrk8s-metallb-io-v1beta2-frrconfiguration"
	frrDefaulterWebhookPath = "/mutate-frrk8s-metallb-io-v1beta2-frrconfiguration"
)

func SetupWebhookWithManager(mgr ctrl.Manager) error {
	v := &FRRConfigValidator{}
	v.client = mgr.GetClient()
	v.decoder = admission.NewDecoder(mgr.GetScheme())

	mgr.GetWebhookServer().Register(
		frrConfigWebhookPath,
		&webhook.Admission{Handler: v})

	d := &FRRConfigurationDefaulter{}
	d.client = mgr.GetClient()
	d.DefaultLogLevel = "info"

	mgr.GetWebhookServer().Register(
		frrDefaulterWebhookPath,
		admission.WithCustomDefaulter(mgr.GetScheme(), &apiv1beta2.FRRConfiguration{}, d))

	return nil
}

// +kubebuilder:webhook:path=/mutate-frrk8s-metallb-io-v1beta2-frrconfiguration,mutating=true,failurePolicy=fail,sideEffects=None,groups=frrk8s.metallb.io,resources=frrconfigurations,verbs=create;update,versions=v1beta2,name=mfrrconfigurationsvalidationwebhook-v1beta2.metallb.io,admissionReviewVersions=v1

// FRRConfigurationDefaulter struct is responsible for setting default values on the custom resource of the
// Kind FRRConfiguration when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type FRRConfigurationDefaulter struct {
	client          client.Client
	DefaultLogLevel string
}

var _ webhook.CustomDefaulter = &FRRConfigurationDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind CronJob.
func (d *FRRConfigurationDefaulter) Default(_ context.Context, obj runtime.Object) error {
	frrConfiguration, ok := obj.(*apiv1beta2.FRRConfiguration)
	if !ok {
		return fmt.Errorf("expected an FRRConfiguration object but got %T", obj)
	}
	level.Info(Logger).Log("Defaulting for FRRConfiguration", "name", frrConfiguration.GetName())

	// Set default values
	d.applyDefaults(frrConfiguration)
	return nil
}

// applyDefaults applies default values to CronJob fields.
func (d *FRRConfigurationDefaulter) applyDefaults(frrConfiguration *apiv1beta2.FRRConfiguration) {
	frrConfiguration.Spec.LogLevel = d.DefaultLogLevel
}

type FRRConfigValidator struct {
	ClusterResourceNamespace string

	client  client.Client
	decoder admission.Decoder
}

//+kubebuilder:webhook:verbs=create;update,path=/validate-frrk8s-metallb-io-v1beta2-frrconfiguration,mutating=false,failurePolicy=fail,groups=frrk8s.metallb.io,resources=frrconfigurations,versions=v1beta2,name=frrconfigurationsvalidationwebhook-v1beta2.metallb.io,sideEffects=None,admissionReviewVersions=v1

func (v *FRRConfigValidator) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	var config apiv1beta2.FRRConfiguration
	var oldConfig apiv1beta2.FRRConfiguration
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
	cfgs   *apiv1beta2.FRRConfigurationList
}

func validateConfigCreate(frrConfig *apiv1beta2.FRRConfiguration) ([]string, error) {
	level.Debug(Logger).Log("webhook", "frrconfiguration", "action", "create", "name", frrConfig.Name, "namespace", frrConfig.Namespace)
	defer level.Debug(Logger).Log("webhook", "frrconfiguration", "action", "end create", "name", frrConfig.Name, "namespace", frrConfig.Namespace)

	return validateConfig(frrConfig)
}

func validateConfigUpdate(frrConfig *apiv1beta2.FRRConfiguration) ([]string, error) {
	level.Debug(Logger).Log("webhook", "frrconfiguration", "action", "update", "name", frrConfig.Name, "namespace", frrConfig.Namespace)
	defer level.Debug(Logger).Log("webhook", "frrconfiguration", "action", "end update", "name", frrConfig.Name, "namespace", frrConfig.Namespace)

	return validateConfig(frrConfig)
}

func validateConfigDelete(_ *apiv1beta2.FRRConfiguration) ([]string, error) {
	return []string{}, nil
}

func validateConfig(frrConfig *apiv1beta2.FRRConfiguration) ([]string, error) {
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
		warnings = append(warnings, "disableMP is deprecated and will be removed in a future release")
	}

	existingFRRConfigurations, err := getFRRConfigurations()
	if err != nil {
		return warnings, err
	}

	matchingNodes := []nodeAndConfigs{}
	for _, n := range existingNodes {
		if selector.Matches(labels.Set(n.Labels)) {
			matchingNodes = append(matchingNodes, nodeAndConfigs{
				name:   n.Name,
				labels: n.Labels,
				cfgs:   &apiv1beta2.FRRConfigurationList{},
			})
		}
	}

	for _, n := range matchingNodes {
		for _, cfg := range existingFRRConfigurations.Items {
			nodeSelector := cfg.Spec.NodeSelector
			selector, err := getCachedSelector(nodeSelector)
			if err != nil {
				// shouldn't happen as it would have been denied earlier, just in case.
				continue
			}

			if cfg.Name == frrConfig.Name {
				// shouldn't happen for creates as it would be considered an update, and in any case
				// we add the updated one at the end because for updates we don't want the old and updated resources
				// to be considered together.
				for _, rold := range cfg.Spec.BGP.Routers {
					for _, rnew := range frrConfig.Spec.BGP.Routers {
						if rold.VRF != rnew.VRF {
							continue
						}
						for _, nold := range rold.Neighbors {
							for _, nnew := range rnew.Neighbors {
								if nold.ASN == nnew.ASN && nold.Address == nnew.Address && nold.EnableGracefulRestart != nnew.EnableGracefulRestart {
									warnings = append(warnings, "Graceful restart configuration changed, it will be available on the next restart")
									continue
								}
							}
						}
					}
				}
				continue
			}

			if selector.Matches(labels.Set(n.labels)) {
				n.cfgs.Items = append(n.cfgs.Items, cfg)
			}
		}
		n.cfgs.Items = append(n.cfgs.Items, *frrConfig)
	}

	for _, n := range matchingNodes {
		err := Validate(n.cfgs)
		if err != nil {
			return warnings, errors.Join(err, fmt.Errorf("resource is invalid for node %s", n.name))
		}
	}

	return warnings, nil
}

var getFRRConfigurations = func() (*apiv1beta2.FRRConfigurationList, error) {
	frrConfigurationsList := &apiv1beta2.FRRConfigurationList{}
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

func containsDisableMP(routers []apiv1beta2.Router) bool {
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
