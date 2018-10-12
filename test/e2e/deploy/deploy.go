package deploy

import (
	"context"
	"encoding/json"
	"html/template"
	"log"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/Azure/aad-pod-identity/test/e2e/azureidentity"
	"github.com/Azure/aad-pod-identity/test/e2e/util"
	"github.com/pkg/errors"
)

// List is a container that holds all deployment returned from 'kubectl get deploy'
type List struct {
	Deploys []Deploy `json:"items"`
}

// Deploy is used to parse data from 'kubectl get deploy'
type Deploy struct {
	Metadata Metadata `json:"metadata"`
	Spec     Spec     `json:"spec"`
	Status   Status   `json:"status"`
}

// Metadata holds information about a deployment
type Metadata struct {
	Name string `json:"name"`
}

// Spec holds the spec about a deployment
type Spec struct {
	Replicas int `json:"replicas"`
}

// Status holds the status about a deployment
type Status struct {
	AvailableReplicas int `json:"availableReplicas"`
}

// Create will create a demo deployment on a Kubernetes cluster
func Create(subscriptionID, resourceGroup, name, identityBinding, templateOutputPath string) error {
	clientID, err := azureidentity.GetClientID(resourceGroup, identityBinding)
	if err != nil {
		return err
	}

	t, err := template.New("deployment.yaml").ParseFiles(path.Join("template", "deployment.yaml"))
	if err != nil {
		return err
	}

	deployFilePath := path.Join(templateOutputPath, name+"-deployment.yaml")
	deployFile, err := os.Create(deployFilePath)
	if err != nil {
		return err
	}
	defer deployFile.Close()

	deployData := struct {
		SubscriptionID  string
		ResourceGroup   string
		ClientID        string
		Name            string
		IdentityBinding string
	}{
		subscriptionID,
		resourceGroup,
		clientID,
		name,
		identityBinding,
	}
	if err := t.Execute(deployFile, deployData); err != nil {
		return err
	}

	cmd := exec.Command("kubectl", "apply", "-f", deployFilePath)
	util.PrintCommand(cmd)
	_, err = cmd.CombinedOutput()
	if err != nil {
		return err
	}

	return nil
}

// Delete will delete a deployment on a Kubernetes cluster
func Delete(name, templateOutputPath string) error {
	cmd := exec.Command("kubectl", "delete", "-f", path.Join(templateOutputPath, name+"-deployment.yaml"), "--ignore-not-found")
	util.PrintCommand(cmd)
	_, err := cmd.CombinedOutput()
	return err
}

// GetAll will return a list of deployment on a Kubernetes cluster
func GetAll() (*List, error) {
	cmd := exec.Command("kubectl", "get", "deploy", "-ojson")
	util.PrintCommand(cmd)
	out, err := cmd.CombinedOutput()

	nl := List{}
	err = json.Unmarshal(out, &nl)
	if err != nil {
		log.Printf("Error unmarshalling nodes json:%s", err)
		return nil, err
	}

	return &nl, nil
}

// IsAvailableReplicasMatchDesired will return a boolean that indicate whether the number
// of available replicas of a deployment matches the desired number of replicas
func IsAvailableReplicasMatchDesired(name string) (bool, error) {
	dl, err := GetAll()
	if err != nil {
		return false, err
	}

	for _, deploy := range dl.Deploys {
		if deploy.Metadata.Name == name {
			return deploy.Status.AvailableReplicas == deploy.Spec.Replicas, nil
		}
	}

	return false, nil
}

// WaitOnReady will block until the number of replicas of a deployment is equal to the specified amount
func WaitOnReady(name string) (bool, error) {
	successChannel, errorChannel := make(chan bool, 1), make(chan error)
	duration := 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				errorChannel <- errors.Errorf("Timeout exceeded (%s) while waiting for deployment (%s) to be availabe", duration.String(), name)
				return
			default:
				match, err := IsAvailableReplicasMatchDesired(name)
				if err != nil {
					errorChannel <- err
					return
				}
				if match {
					successChannel <- true
				}
				time.Sleep(3 * time.Second)
			}
		}
	}()

	for {
		select {
		case err := <-errorChannel:
			return false, err
		case success := <-successChannel:
			return success, nil
		}
	}
}
