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

// The loader runs as a DaemonSet, one instance per node. It loads profiles
// onto its own node and reports what it loaded in ProfileNodeStatus. It never
// decides promotion or demotion: the controller decides cluster-wide, the
// loader only applies the mode it is told to.
package main

import (
	"flag"
	"os"
	"path/filepath"

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

	armadillov1alpha1 "github.com/tanchwa/armadillo/api/v1alpha1"
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
	var profileDir string
	var securityFSDir string
	var nodeName string
	var metricsAddr string
	var probeAddr string
	flag.StringVar(&profileDir, "apparmor-profile-dir", "/var/lib/armadillo/profiles",
		"The host directory the loader writes profiles to before running apparmor_parser. "+
			"Keep it separate from /etc/apparmor.d so the loader cannot overwrite the host's own profiles.")
	flag.StringVar(&securityFSDir, "apparmor-securityfs-dir", "/sys/kernel/security/apparmor",
		"The host AppArmor securityfs directory. It is passed to apparmor_parser as --subdomainfs "+
			"and used to read the kernel's hash of loaded profiles.")
	flag.StringVar(&nodeName, "node-name", "",
		"The name of the node this loader runs on. Set it from the downward API (spec.nodeName).")
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if nodeName == "" {
		setupLog.Error(nil, "Flag --node-name is required")
		os.Exit(1)
	}

	// Fail fast on a node without AppArmor. Running here would report nothing
	// loaded, and pods bound to a profile would fail with CreateContainerError.
	if _, err := os.Stat(filepath.Join(securityFSDir, "profiles")); err != nil {
		setupLog.Error(err, "AppArmor securityfs not found, AppArmor may be disabled on this node",
			"path", securityFSDir)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		// Every node runs its own loader, so there is nothing to elect.
		LeaderElection: false,
	})
	if err != nil {
		setupLog.Error(err, "Failed to create manager")
		os.Exit(1)
	}

	// TODO: register the loader reconciler. It watches ManagedProfiles, writes
	// profiles under profileDir, runs apparmor_parser -r, and records the
	// loaded hash in this node's ProfileNodeStatus.
	//
	// Do not put +kubebuilder:rbac markers in loader code: controller-gen merges
	// every marker under ./... into the manager's role. The loader's RBAC is
	// hand-written in config/loader/role.yaml.

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting loader", "node", nodeName, "profileDir", profileDir, "securityFSDir", securityFSDir)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Failed to run loader")
		os.Exit(1)
	}
}
