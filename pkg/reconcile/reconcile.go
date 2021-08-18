package reconcile

import (
	"emperror.dev/errors"
	"github.com/banzaicloud/operator-tools/pkg/reconciler"
	"github.com/banzaicloud/operator-tools/pkg/resources"
	"github.com/banzaicloud/operator-tools/pkg/utils"
	"github.com/banzaicloud/operator-tools/pkg/wait"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"

	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	scheme = runtime.NewScheme()
	log    = ctrl.Log.WithName("syncronize-resources")
)

func parseObjectsFromManifests(inputFiles []string) (utils.RuntimeObjects, error) {
	parsedObjects := []runtime.Object{}
	parser := resources.NewObjectParser(scheme)
	for _, fn := range inputFiles {
		log.Info("reading manifest", "path", fn)
		contents, err := ioutil.ReadFile(fn)
		if err != nil {
			return nil, errors.WrapIfWithDetails(err, "error reading manifest file", "manifest_file", fn)
		}

		manifestParsedObjects, err := parser.ParseYAMLManifest(string(contents))
		if err != nil {
			return nil, errors.WrapIfWithDetails(err, "cannot parse manifest", "manifest_file", fn)
		}

		parsedObjects = append(parsedObjects, manifestParsedObjects...)
	}

	return parsedObjects, nil
}

func SyncronizeResources(inputFiles []string, desiredState reconciler.StaticDesiredState, reconclilationTimeout time.Duration, allowRecreate bool) error {
	log.Info("syncronizing resources", "input_files", inputFiles, "allow_recreate", allowRecreate)

	parsedObjects, err := parseObjectsFromManifests(inputFiles)
	if err != nil {
		return err
	}

	resourceSortOrder := utils.InstallResourceOrder
	if desiredState == reconciler.StateAbsent {
		resourceSortOrder = utils.UninstallResourceOrder
	}

	parsedObjects.Sort(resourceSortOrder)
	log.Info("connecting to the Kubernetes API server")
	client, err := runtimeClient.New(ctrl.GetConfigOrDie(), runtimeClient.Options{})
	if err != nil {
		return err
	}

	recreateOption := reconciler.WithRecreateEnabledForNothing()
	if allowRecreate {
		recreateOption = reconciler.WithRecreateEnabledForAll()
	}

	resourceReconciler := reconciler.NewReconcilerWith(client,
		reconciler.WithLog(ctrl.Log.WithName("syncronize-resources")),
		reconciler.WithRecreateImmediately(), // We are not supporting requeing, let's try to force
		recreateOption)

	reconciliationStartedAt := time.Now()
	for {
		shouldRetry := false
		for _, object := range parsedObjects {
			result, err := resourceReconciler.ReconcileResource(object, desiredState)
			if err != nil {
				return errors.WrapIf(err, "cannot reconcile resource")
			}
			if result != nil {
				log.Info("waiting on dependant items to be GCd, retrying the reconciliation")
				shouldRetry = true
				break
			}

			if desiredState == reconciler.StateAbsent {
				err = waitForObjectDeletion(client, object, reconclilationTimeout)
				if err != nil {
					return errors.WrapIf(err, "deletion did not complete")
				}
			}
		}
		if !shouldRetry {
			break
		}
		if time.Since(reconciliationStartedAt) > reconclilationTimeout {
			return errors.New("reconciliation timeout")
		}
		time.Sleep(5 * time.Second)
	}

	log.Info("reconciliation complete")
	return nil
}

func waitForObjectDeletion(client runtimeClient.Client, object runtime.Object, timeout time.Duration) error {
	rcc := wait.NewResourceConditionChecks(client, wait.Backoff{
		Duration: time.Second,
		Factor:   1,
		Steps:    9999,
		Cap:      timeout,
	}, log.WithName("wait"), nil)
	err := rcc.WaitForResources("removal", []runtime.Object{object}, wait.NonExistsConditionCheck)
	if err != nil {
		return err
	}
	return nil
}
