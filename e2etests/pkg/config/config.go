// SPDX-License-Identifier:Apache-2.0

package config

import (
	"context"
	"time"

	frrk8sv1beta2 "github.com/metallb/frr-k8s/api/v1beta2"
	"github.com/metallb/frrk8stests/pkg/k8s"
	"github.com/metallb/frrk8stests/pkg/k8sclient"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type Updater struct {
	client client.Client
}

func NewUpdater() (*Updater, error) {
	r := k8sclient.RestConfig()
	myScheme := runtime.NewScheme()
	if err := frrk8sv1beta2.AddToScheme(myScheme); err != nil {
		return nil, err
	}
	if err := v1.AddToScheme(myScheme); err != nil {
		return nil, err
	}

	cl, err := client.New(r, client.Options{
		Scheme: myScheme,
	})
	if err != nil {
		return nil, err
	}
	return &Updater{
		client: cl,
	}, nil
}

func (u *Updater) Update(secrets []v1.Secret, configs ...frrk8sv1beta2.FRRConfiguration) error {
	for i, config := range configs {
		_, err := controllerutil.CreateOrUpdate(context.Background(), u.client, &config, func() error {
			old := &configs[i].Spec
			config.Spec = *old.DeepCopy()
			return nil
		})

		if err != nil {
			return err
		}
	}
	for i, s := range secrets {
		_, err := controllerutil.CreateOrUpdate(context.Background(), u.client, &s, func() error {
			s.Data = secrets[i].Data
			s.StringData = secrets[i].StringData
			s.Type = secrets[i].Type
			return nil
		})

		if err != nil {
			return err
		}
	}

	return nil
}

func (u *Updater) UpdateFrrOperatorConfiguration(config frrk8sv1beta2.FRROperatorConfiguration) error {
	_, err := controllerutil.CreateOrUpdate(context.Background(), u.client, &config, func() error {
		old := &config.Spec
		config.Spec = *old.DeepCopy()
		return nil
	})

	if err != nil {
		return err
	}
	return nil
}

// CleanFRROperatorConfiguration deletes all FRROperatorConfigurations inside FRRK8sNamespace.
// It will return after all resources are gone, or after a maximum of 60 seconds, whichever comes first.
func (u *Updater) CleanFRROperatorConfiguration() error {
	err := u.client.DeleteAllOf(context.Background(), &frrk8sv1beta2.FRROperatorConfiguration{}, client.InNamespace(k8s.FRRK8sNamespace))
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer deadlineCancel()

	return wait.PollUntilContextCancel(deadlineCtx, 1*time.Second, true,
		func(ctx context.Context) (bool, error) {
			var resourceList frrk8sv1beta2.FRROperatorConfigurationList
			if err = u.client.List(ctx, &resourceList, client.InNamespace(k8s.FRRK8sNamespace)); err != nil {
				return false, err
			}
			if len(resourceList.Items) == 0 {
				return true, nil
			}
			return false, nil
		})
}

func (u *Updater) Clean() error {
	err := u.CleanFRROperatorConfiguration()

	err = u.client.DeleteAllOf(context.Background(), &frrk8sv1beta2.FRRConfiguration{}, client.InNamespace(k8s.FRRK8sNamespace))
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	err = u.client.DeleteAllOf(context.Background(), &v1.Secret{}, client.InNamespace(k8s.FRRK8sNamespace))
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}
