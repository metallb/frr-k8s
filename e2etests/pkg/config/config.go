// SPDX-License-Identifier:Apache-2.0

package config

import (
	"context"

	frrk8sv1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type Updater struct {
	client client.Client
}

func NewUpdater(r *rest.Config) (*Updater, error) {
	myScheme := runtime.NewScheme()
	if err := frrk8sv1beta1.AddToScheme(myScheme); err != nil {
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

func (u *Updater) Update(configs ...frrk8sv1beta1.FRRConfiguration) error {
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

	return nil
}

func (u *Updater) Clean() error {
	err := u.client.DeleteAllOf(context.Background(), &frrk8sv1beta1.FRRConfiguration{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}
