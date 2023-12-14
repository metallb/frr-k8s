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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/metallb/frr-k8s/internal/frr"
)

const ConversionSuccess = "success"

// FRRConfigurationReconciler reconciles a FRRConfiguration object.
type FRRConfigurationReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	FRRHandler         frr.ConfigHandler
	Logger             log.Logger
	NodeName           string
	Namespace          string
	ReloadStatus       func()
	conversionResult   string
	conversionResMutex sync.Mutex
	AlwaysBlockCIDRS   []net.IPNet
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

func (r *FRRConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	level.Info(r.Logger).Log("controller", "FRRConfigurationReconciler", "start reconcile", req.NamespacedName.String())
	defer level.Info(r.Logger).Log("controller", "FRRConfigurationReconciler", "end reconcile", req.NamespacedName.String())
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
	err := r.Client.List(ctx, &configs)
	if err != nil {
		conversionResult = fmt.Sprintf("failed: %v", err)
		return ctrl.Result{}, err
	}

	level.Debug(r.Logger).Log("controller", "FRRConfigurationReconciler", "k8s config", dumpK8sConfigs(configs))

	if len(configs.Items) == 0 {
		err := r.applyEmptyConfig(req)
		if err != nil {
			updateErrors.Inc()
			configStale.Set(1)
			conversionResult = fmt.Sprintf("failed: %v", err)
		}
		return ctrl.Result{}, err
	}

	thisNode := &corev1.Node{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: r.NodeName}, thisNode)
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
		level.Error(r.Logger).Log("controller", "FRRConfigurationReconciler", "failed to apply the config", req.NamespacedName.String(), "error", err)
		conversionResult = fmt.Sprintf("failed: %v", err)
		return ctrl.Result{}, nil
	}

	level.Debug(r.Logger).Log("controller", "FRRConfigurationReconciler", "frr config", dumpFRRConfig(config))

	if err := r.FRRHandler.ApplyConfig(config); err != nil {
		updateErrors.Inc()
		configStale.Set(1)
		conversionResult = fmt.Sprintf("failed: %v", err)
		level.Error(r.Logger).Log("controller", "FRRConfigurationReconciler", "failed to apply the config", req.NamespacedName.String(), "error", err)
		return ctrl.Result{}, err
	}

	configLoaded.Set(1)
	configStale.Set(0)

	return ctrl.Result{}, nil
}

func (r *FRRConfigurationReconciler) applyEmptyConfig(req ctrl.Request) error {
	config, err := apiToFRR(ClusterResources{}, []net.IPNet{})
	if err != nil {
		level.Error(r.Logger).Log("controller", "FRRConfigurationReconciler", "failed to translate the empty config", req.NamespacedName.String(), "error", err)
		panic("failed to translate empty config")
	}

	if err := r.FRRHandler.ApplyConfig(config); err != nil {
		level.Error(r.Logger).Log("controller", "FRRConfigurationReconciler", "failed to apply the empty config", req.NamespacedName.String(), "error", err)
		return err
	}
	return nil
}

// configsForNode filters the given FRRConfigurations such that only the ones matching the given labels are returned.
// This also validates that the configuration objects have a valid nodeSelector.
func configsForNode(cfgs []frrk8sv1beta1.FRRConfiguration, nodeLabels map[string]string) ([]frrk8sv1beta1.FRRConfiguration, error) {
	valid := []frrk8sv1beta1.FRRConfiguration{}
	for _, cfg := range cfgs {
		cfg := cfg
		selector, err := metav1.LabelSelectorAsSelector(&cfg.Spec.NodeSelector)
		if err != nil {
			return nil, fmt.Errorf("could not parse nodeSelector for FRRConfiguration %s/%s, err: %w", cfg.Namespace, cfg.Name, err)
		}

		if !selector.Matches(labels.Set(nodeLabels)) {
			continue
		}

		valid = append(valid, cfg)
	}

	return valid, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FRRConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	p := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return filterNodeEvent(e, r.NodeName)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&frrk8sv1beta1.FRRConfiguration{}).
		Watches(&corev1.Node{}, &handler.EnqueueRequestForObject{}).
		Watches(&corev1.Secret{}, &handler.EnqueueRequestForObject{}).
		WithEventFilter(p).
		Complete(r)
}

func (r *FRRConfigurationReconciler) getSecrets(ctx context.Context) (map[string]corev1.Secret, error) {
	var secrets corev1.SecretList
	err := r.List(ctx, &secrets, client.InNamespace(r.Namespace))
	if k8serrors.IsNotFound(err) {
		return map[string]corev1.Secret{}, nil
	}
	if err != nil {
		level.Error(r.Logger).Log("controller", "FRRConfigurationReconciler", "error", "failed to get secrets", "error", err)
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
