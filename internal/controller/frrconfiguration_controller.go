/*
Copyright 2023.

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
	"fmt"
	"net"
	"sync"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-kit/log/level"
	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
	"github.com/metallb/frr-k8s/internal/logging"
)

const ConversionSuccess = "success"

const frrConfigurationReconcileName = "frrconfig"

// Validation only reads this path; reuse it across configurations.
var nodeSelectorPath = field.NewPath("nodeSelector")

// FRRConfigurationReconciler reconciles a FRRConfiguration object.
type FRRConfigurationReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	FRRHandler         frr.ConfigHandler
	NodeName           string
	Namespace          string
	ReloadStatus       func()
	conversionResult   string
	conversionResMutex sync.Mutex
	AlwaysBlockCIDRS   []net.IPNet
	DefaultLogLevel    logging.Level
}

func (r *FRRConfigurationReconciler) ConversionResult() string {
	r.conversionResMutex.Lock()
	defer r.conversionResMutex.Unlock()
	return r.conversionResult
}

// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrconfigurations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrconfigurations/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,verbs=get;list;watch
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=validatingwebhookconfigurations,resourceNames="frr-k8s-validating-webhook-configuration",verbs=update
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrk8sconfigurations,verbs=get;list;watch

func (r *FRRConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := logging.GetLogger()
	level.Info(l).Log("controller", "FRRConfigurationReconciler", "start reconcile", req.String())
	defer level.Info(l).Log("controller", "FRRConfigurationReconciler", "end reconcile", req.String())
	level.Debug(l).Log("controller", "FRRConfigurationReconciler", "log level controller", "debug")

	lastConversionResult := r.conversionResult
	conversionResult := ConversionSuccess

	defer func() {
		r.conversionResMutex.Lock()
		r.conversionResult = conversionResult
		r.conversionResMutex.Unlock()
		if conversionResult != lastConversionResult {
			r.ReloadStatus()
		}
	}()

	updates.Inc()

	configs := frrk8sv1beta1.FRRConfigurationList{}
	// The cached objects are treated as immutable. configsForNode returns deep
	// copies of the small matching subset before it reaches conversion code.
	err := r.List(ctx, &configs, client.UnsafeDisableDeepCopy)
	if err != nil {
		conversionResult = fmt.Sprintf("failed: %v", err)
		return ctrl.Result{}, err
	}

	k8sDump := ""
	if l.GetLogLevel().IsAllOrDebug() {
		k8sDump = dumpK8sConfigs(configs)
	}
	level.Debug(l).Log("controller", "FRRConfigurationReconciler", "k8s config", k8sDump)

	// logLevel is used exclusively to set the FRR instance's log level. The log level for the controllers is
	// set via the FRRK8sConfiguration controller.
	logLevel, err := getLogLevel(ctx, r, r.Namespace, r.DefaultLogLevel)
	if err != nil {
		return ctrl.Result{}, err
	}
	level.Debug(l).Log("controller", "FRRConfigurationReconciler", "log level FRR", logLevel)

	if len(configs.Items) == 0 {
		err := r.applyEmptyConfig(logLevel)
		if err != nil {
			updateErrors.Inc()
			configStale.Set(1)
			conversionResult = fmt.Sprintf("failed: %v", err)
		}
		return ctrl.Result{}, err
	}

	thisNode := &corev1.Node{}
	err = r.Get(ctx, types.NamespacedName{Name: r.NodeName}, thisNode)
	if err != nil {
		conversionResult = fmt.Sprintf("failed: %v", err)
		return ctrl.Result{}, err
	}

	cfgs, err := configsForNode(configs.Items, thisNode.Labels)
	if err != nil {
		updateErrors.Inc()
		configStale.Set(1)
		conversionResult = fmt.Sprintf("failed: %v", err)
		return ctrl.Result{}, err
	}

	secrets, err := r.getSecrets(ctx)
	if err != nil {
		conversionResult = fmt.Sprintf("failed: %v", err)
		return ctrl.Result{}, err
	}

	resources := ClusterResources{
		FRRConfigs:      cfgs,
		PasswordSecrets: secrets,
	}
	config, err := apiToFRR(resources, r.AlwaysBlockCIDRS)
	if err != nil {
		updateErrors.Inc()
		configStale.Set(1)
		level.Error(l).Log("controller", "FRRConfigurationReconciler", "failed to convert the config, error", err)
		conversionResult = fmt.Sprintf("failed: %v", err)
		return ctrl.Result{}, nil
	}

	frrDump := ""
	if l.GetLogLevel().IsAllOrDebug() {
		frrDump = dumpFRRConfig(config)
	}
	level.Debug(l).Log("controller", "FRRConfigurationReconciler", "frr config", frrDump)

	config.Loglevel = frr.LevelFrom(logLevel)
	if err := r.FRRHandler.ApplyConfig(config); err != nil {
		updateErrors.Inc()
		configStale.Set(1)
		conversionResult = fmt.Sprintf("failed: %v", err)
		level.Error(l).Log("controller", "FRRConfigurationReconciler", "failed to apply the config, error", err)
		return ctrl.Result{}, err
	}

	configLoaded.Set(1)
	configStale.Set(0)

	return ctrl.Result{}, nil
}

// applyEmptyConfig generates and applies an empty FRR configuration with the specified log level.
// This is called when no FRRConfiguration resources exist in the cluster, ensuring FRR is configured
// with a minimal valid configuration rather than leaving it in an undefined state. The function
// translates empty cluster resources to FRR configuration format, sets the provided log level,
// and applies the configuration via the FRR handler. Returns an error if applying the configuration
// fails. Panics if translating the empty configuration fails, as this indicates a critical bug.
func (r *FRRConfigurationReconciler) applyEmptyConfig(logLevel logging.Level) error {
	l := logging.GetLogger()
	config, err := apiToFRR(ClusterResources{}, []net.IPNet{})
	if err != nil {
		level.Error(l).Log("controller", "FRRConfigurationReconciler", "failed to translate the empty config, error", err)
		panic("failed to translate empty config")
	}
	config.Loglevel = frr.LevelFrom(logLevel)

	if err := r.FRRHandler.ApplyConfig(config); err != nil {
		level.Error(l).Log("controller", "FRRConfigurationReconciler", "failed to apply the empty config, error", err)
		return err
	}
	return nil
}

// configsForNode filters the given FRRConfigurations such that only the ones matching the given labels are returned.
// This also validates that the configuration objects have a valid nodeSelector.
func configsForNode(cfgs []frrk8sv1beta1.FRRConfiguration, nodeLabels map[string]string) ([]frrk8sv1beta1.FRRConfiguration, error) {
	valid := []frrk8sv1beta1.FRRConfiguration{}
	for i := range cfgs {
		cfg := &cfgs[i]
		if err := validateNodeSelector(&cfg.Spec.NodeSelector); err != nil {
			return nil, fmt.Errorf("could not parse nodeSelector for FRRConfiguration %s/%s, err: %w", cfg.Namespace, cfg.Name, err)
		}
		matches, err := nodeSelectorMatches(&cfg.Spec.NodeSelector, nodeLabels)
		if err != nil {
			return nil, fmt.Errorf("could not parse nodeSelector for FRRConfiguration %s/%s, err: %w", cfg.Namespace, cfg.Name, err)
		}
		if !matches {
			continue
		}
		valid = append(valid, *cfg.DeepCopy())
	}

	return valid, nil
}

// validateNodeSelector checks the full selector, including expressions on configurations
// whose exact labels do not match this node.
func validateNodeSelector(selector *metav1.LabelSelector) error {
	return metav1validation.ValidateLabelSelector(selector,
		metav1validation.LabelSelectorValidationOptions{}, nodeSelectorPath).ToAggregate()
}

// nodeSelectorMatches checks exact labels before building a selector for expressions.
// The caller must validate the selector before using this fast path.
func nodeSelectorMatches(selector *metav1.LabelSelector, nodeLabels map[string]string) (bool, error) {
	for key, value := range selector.MatchLabels {
		actual, exists := nodeLabels[key]
		if !exists || actual != value {
			return false, nil
		}
	}
	if len(selector.MatchExpressions) == 0 {
		return true, nil
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false, err
	}
	return parsed.Matches(labels.Set(nodeLabels)), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FRRConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	p := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return filterNodeEvent(e, r.NodeName)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		Watches(&frrk8sv1beta1.FRRConfiguration{},
			// The controller is level driven, so we squash all the frrconfiguration changes to a single key.
			// By doing this, the controller will throttle when there are a large amount of configurations generated
			// at the same time.
			handler.EnqueueRequestsFromMapFunc(reconcileRequestForFRRConfiguration),
		).
		For(&corev1.Node{}).
		// Secret changes rebuild the same full configuration. Sharing the key
		// coalesces bursts (including relists) instead of reconciling once per Secret.
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(reconcileRequestForFRRConfiguration)).
		Watches(&frrk8sv1beta1.FRRK8sConfiguration{}, &handler.EnqueueRequestForObject{}).
		WithEventFilter(p).
		Complete(r)
}

// reconcileRequestForFRRConfiguration maps configuration and Secret events to one
// request per namespace; the reconciler reads all configuration and Secret objects.
func reconcileRequestForFRRConfiguration(_ context.Context, obj client.Object) []reconcile.Request {
	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{
			Name:      frrConfigurationReconcileName,
			Namespace: obj.GetNamespace(),
		}},
	}
}

func (r *FRRConfigurationReconciler) getSecrets(ctx context.Context) (map[string]corev1.Secret, error) {
	var secrets corev1.SecretList
	l := logging.GetLogger()
	err := r.List(ctx, &secrets, client.InNamespace(r.Namespace))
	if k8serrors.IsNotFound(err) {
		return map[string]corev1.Secret{}, nil
	}
	if err != nil {
		level.Error(l).Log("controller", "FRRConfigurationReconciler", "error", "failed to get secrets", "error", err)
		return nil, err
	}
	secretsMap := make(map[string]corev1.Secret)
	for _, secret := range secrets.Items {
		secretsMap[secret.Name] = secret
	}
	return secretsMap, nil
}

func filterNodeEvent(e event.UpdateEvent, thisNode string) bool {
	newNodeObj, ok := e.ObjectNew.(*corev1.Node)
	if !ok {
		return true
	}

	oldNodeObj, ok := e.ObjectOld.(*corev1.Node)
	if !ok {
		return true
	}

	// Node updates do not change its name, we have this just in case
	if oldNodeObj.Name != newNodeObj.Name {
		return true
	}

	// Ignoring event if it's not for our node
	if newNodeObj.Name != thisNode {
		return false
	}

	// Ignoring event if it didn't change the node's labels
	if labels.Equals(labels.Set(oldNodeObj.Labels), labels.Set(newNodeObj.Labels)) {
		return false
	}

	return true
}
