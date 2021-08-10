package cmd

import (
	"emperror.dev/errors"
	"github.com/banzaicloud/helm3-crd-updater/pkg/reconcile"
	"github.com/spf13/cobra"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"
)

var (
	log                   = ctrl.Log.WithName("syncronize-resources")
	yamlFiles             = []string{}
	recreateResources     = false
	reconciliationTimeout = time.Duration(0)
	rootCmd               = &cobra.Command{
		Use:   "sync-resources",
		Short: "Syncronize k8s resources from YAML to the current cluster",
		Run: func(cmd *cobra.Command, args []string) {
			if len(yamlFiles) == 0 {
				log.Error(errors.New("please specify the input manifests"), "")
				os.Exit(1)
			}
			err := reconcile.SyncronizeResources(yamlFiles, reconciliationTimeout, recreateResources)
			if err != nil {
				log.Error(err, "reconciliation failed")
				os.Exit(1)
			}
		},
	}
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.Flags().StringArrayVar(&yamlFiles, "manifest", []string{}, "Name of the YAML files to load manifests from (can be repeated multiple times)")
	rootCmd.Flags().BoolVar(&recreateResources, "allow-recreate-resources", false, "In case of an inmutable field recreate the given resource (dangerous, as recreating a CRD causes all CRs to be deleted)")
	rootCmd.Flags().DurationVar(&reconciliationTimeout, "timeout", 5*time.Minute, "Time out the reconciliation after this amount of time")
}
