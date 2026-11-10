/*
Copyright 2026.

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

// The webhook binds pods to their profiles at admission: it sets the AppArmor
// profile of the containers a ProfileBinding selects and injects the startup
// gate. It runs apart from the manager so that it can be scaled and restarted
// independently; pod creation in opted-in namespaces depends on it.
package main

import (
	"crypto/tls"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	armadillov1alpha1 "github.com/tanchwa/armadillo/api/v1alpha1"
	webhookv1 "github.com/tanchwa/armadillo/internal/webhook/v1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(armadillov1alpha1.AddToScheme(scheme))
}

func main() {
	var gateImage string
	var loaderPort int
	var webhookCertPath, webhookCertName, webhookCertKey string
	var probeAddr string
	flag.StringVar(&gateImage, "gate-image", os.Getenv("GATE_IMAGE"),
		"The image of the startup gate injected into pods. It must contain /gate. Defaults to $GATE_IMAGE.")
	flag.IntVar(&loaderPort, "loader-port", 9099,
		"The host port of the loader's gate endpoint. Must match the hostPort of the loader DaemonSet.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "/tmp/k8s-webhook-server/serving-certs",
		"The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if gateImage == "" {
		setupLog.Error(nil, "Flag --gate-image or environment variable GATE_IMAGE is required")
		os.Exit(1)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		CertDir:  webhookCertPath,
		CertName: webhookCertName,
		KeyName:  webhookCertKey,
		// HTTP/2 is disabled for the same reason as in the manager: the Stream
		// Cancellation and Rapid Reset CVEs.
		TLSOpts: []func(*tls.Config){func(c *tls.Config) { c.NextProtos = []string{"http/1.1"} }},
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		// Every replica serves admission requests, so there is nothing to elect.
		LeaderElection: false,
	})
	if err != nil {
		setupLog.Error(err, "Failed to create manager")
		os.Exit(1)
	}

	if err := webhookv1.SetupPodWebhookWithManager(mgr, webhookv1.PodWebhookOptions{
		GateImage:  gateImage,
		LoaderPort: loaderPort,
	}); err != nil {
		setupLog.Error(err, "Failed to create webhook", "webhook", "Pod")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	// Ready only once the server can answer, so the Service never routes
	// admission requests to a replica that would fail them.
	if err := mgr.AddReadyzCheck("readyz", webhookServer.StartedChecker()); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting webhook", "gateImage", gateImage, "loaderPort", loaderPort)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Failed to run webhook")
		os.Exit(1)
	}
}
