package main

import (
	"github.com/banzaicloud/helm3-crd-updater/cmd"
	"github.com/banzaicloud/operator-tools/pkg/logger"
	"k8s.io/klog/v2"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
)

var log = ctrl.Log.WithName("syncronize-resources")

func main() {
	ctrl.SetLogger(logger.New()) // logger.Truncate()
	klog.InitFlags(nil)

	err := cmd.Execute()
	if err != nil {
		log.Error(err, "reconciliation failed")
		os.Exit(1)
	}
}
