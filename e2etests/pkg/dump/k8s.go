// SPDX-License-Identifier:Apache-2.0

package dump

import (
	"log"
	"os"
	"strings"
	"time"

	frrk8sv1beta1 "github.com/metallb/frr-k8s/api/v1beta1"
	"github.com/openshift-kni/k8sreporter"
	"k8s.io/apimachinery/pkg/runtime"
)

const LogsTimeDepth = 10 * time.Minute

func NewK8sReporter(namespace string) *k8sreporter.KubernetesReporter {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		log.Fatalf("KUBECONFIG not set")
	}

	if ReportPath == "" {
		log.Fatalf("ReportPath is not set")
	}

	// When using custom crds, we need to add them to the scheme
	addToScheme := func(s *runtime.Scheme) error {
		err := frrk8sv1beta1.AddToScheme(s)
		if err != nil {
			return err
		}
		return nil
	}

	// The namespaces we want to dump resources for (including pods and pod logs)
	dumpNamespace := func(ns string) bool {
		switch {
		case ns == namespace:
			return true
		case strings.HasPrefix(ns, "frrk8s"):
			return true
		}
		return false
	}

	// The list of CRDs we want to dump
	crds := []k8sreporter.CRData{
		{Cr: &frrk8sv1beta1.FRRConfigurationList{}},
		{Cr: &frrk8sv1beta1.FRRNodeStateList{}},
	}

	reporter, err := k8sreporter.New(kubeconfig, addToScheme, dumpNamespace, ReportPath, crds...)
	if err != nil {
		log.Fatalf("Failed to initialize the reporter %s", err)
	}
	return reporter
}

func K8sInfo(testName string, reporter *k8sreporter.KubernetesReporter) {
	reporter.Dump(LogsTimeDepth, testName)
}
