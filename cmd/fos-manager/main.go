/*
Copyright 2022.

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

package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	failoverv1alpha1 "github.com/mycreepy/failover-service-operator/api/v1alpha1"
	"github.com/mycreepy/failover-service-operator/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(failoverv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var secureMetrics bool
	var enableLeaderElection bool
	var probeAddr string
	var interval time.Duration

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metric endpoint binds to. Set to 0 to disable the metric endpoint.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true, "Enable authentication for metric endpoint")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.DurationVar(&interval, "interval", 10*time.Second, "Interval for checking active targets")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logger)
	klog.SetLoggerWithOptions(logger, klog.ContextualLogger(true))
	ctx = log.IntoContext(ctx, logger)

	metricsOptions := metricsserver.Options{
		BindAddress: metricsAddr,
	}

	if secureMetrics {
		metricsOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsOptions,
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: 9443,
		}),
		HealthProbeBindAddress:        probeAddr,
		LeaderElection:                enableLeaderElection,
		LeaderElectionID:              "fos-manager",
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	r := &controllers.FailoverServiceReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Interval: interval,
	}

	err = r.SetupWithManager(mgr)
	if err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "FailoverService")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	err = mgr.Add(r)
	if err != nil {
		setupLog.Error(err, "unable to add reconciler to manager")
		os.Exit(1)
	}

	err = mgr.AddHealthzCheck("healthz", healthz.Ping)
	if err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	err = mgr.AddReadyzCheck("readyz", healthz.Ping)
	if err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")

	err = mgr.Start(ctrl.SetupSignalHandler())
	if err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
