// SPDX-License-Identifier:Apache-2.0

package webhooks

import (
	"context"
	"fmt"
	"net/http"

	"errors"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/metallb/frr-k8s/api/v1beta1"
	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

	selector, err := metav1.LabelSelectorAsSelector(&frrConfig.Spec.NodeSelector)
	if err != nil {
		return warnings, errors.Join(err, errors.New("resource contains an invalid NodeSelector"))
	}

	existingNodes, err := getNodes()
	if err != nil {
		return warnings, err
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
				cfgs:   &v1beta1.FRRConfigurationList{},
			})
		}
	}

	for _, n := range matchingNodes {
		for _, cfg := range existingFRRConfigurations.Items {
			nodeSelector := cfg.Spec.NodeSelector
			selector, err := metav1.LabelSelectorAsSelector(&nodeSelector)
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
				n.cfgs.Items = append(n.cfgs.Items, *cfg.DeepCopy())
			}
		}
		n.cfgs.Items = append(n.cfgs.Items, *frrConfig.DeepCopy())
	}

	for _, n := range matchingNodes {
		err := Validate(n.cfgs)
		if err != nil {
			return warnings, errors.Join(err, fmt.Errorf("resource is invalid for node %s", n.name))
		}
	}

	return warnings, nil
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
