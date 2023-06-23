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
	"path/filepath"
	"testing"

	"github.com/go-kit/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	frrk8sv1beta1 "github.com/metallb/frrk8s/api/v1beta1"
	"github.com/metallb/frrk8s/internal/frr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	localFRR  fakeFRR
	ctx       context.Context
	cancel    context.CancelFunc
)

type fakeFRR struct {
	lastConfig *frr.Config
	mustError  bool
}

func (f *fakeFRR) ApplyConfig(config *frr.Config) error {
	f.lastConfig = config
	if f.mustError {
		return fmt.Errorf("error")
	}
	return nil
}

func TestAPIs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = frrk8sv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	err = (&FRRConfigurationReconciler{
		Client:     k8sManager.GetClient(),
		Scheme:     k8sManager.GetScheme(),
		FRRHandler: &localFRR,
		Logger:     log.NewNopLogger(),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	ctx, cancel = context.WithCancel(context.TODO())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("Frrk8s controller", func() {
	Context("when a FRRConfiguration is created", func() {
		AfterEach(func() {
			toDel := &frrk8sv1beta1.FRRConfiguration{}
			toDel.Name = "test"
			toDel.Namespace = "default"
			err := k8sClient.Delete(context.Background(), toDel)
			if apierrors.IsNotFound(err) {
				return
			}
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() int {
				frrConfigList := &frrk8sv1beta1.FRRConfigurationList{}
				err := k8sClient.List(context.Background(), frrConfigList)
				Expect(err).ToNot(HaveOccurred())
				return len(frrConfigList.Items)
			}).Should(Equal(0))
		})

		It("should apply the configuration to FRR", func() {
			frrConfig := &frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN: uint32(42),
							},
						},
					},
				},
			}
			err := k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return localFRR.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(42),
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						Neighbors:    []*frr.NeighborConfig{},
					}},
				},
			))
		})

		It("should apply and modify the configuration to FRR", func() {
			frrConfig := &frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN: uint32(42),
							},
						},
					},
				},
			}
			err := k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return localFRR.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(42),
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						Neighbors:    []*frr.NeighborConfig{},
					}},
				},
			))

			frrConfig.Spec.BGP.Routers[0].ASN = uint32(43)
			frrConfig.Spec.BGP.Routers[0].Prefixes = []string{"192.168.1.0/32"}

			err = k8sClient.Update(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return localFRR.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(43),
						IPV4Prefixes: []string{"192.168.1.0/32"},
						IPV6Prefixes: []string{},
						Neighbors:    []*frr.NeighborConfig{},
					}},
				},
			))

		})

		It("should create and delete the configuration to FRR", func() {
			frrConfig := &frrk8sv1beta1.FRRConfiguration{
				ObjectMeta: ctrl.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: frrk8sv1beta1.FRRConfigurationSpec{
					BGP: frrk8sv1beta1.BGPConfig{
						Routers: []frrk8sv1beta1.Router{
							{
								ASN: uint32(42),
							},
						},
					},
				},
			}
			err := k8sClient.Create(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return localFRR.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{{MyASN: uint32(42),
						IPV4Prefixes: []string{},
						IPV6Prefixes: []string{},
						Neighbors:    []*frr.NeighborConfig{},
					}},
				},
			))

			err = k8sClient.Delete(context.Background(), frrConfig)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() *frr.Config {
				return localFRR.lastConfig
			}).Should(Equal(
				&frr.Config{
					Routers: []*frr.RouterConfig{},
				},
			))
		})
	})
})
