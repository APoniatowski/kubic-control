// Copyright 2019, 2020 Thorsten Kukuk
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package deployment

import (
	"os"
	"strings"

	"github.com/thkukuk/kubic-control/pkg/tools"
	"gopkg.in/ini.v1"
)

const (
	StateDir = "/var/lib/kubic-control"
)

func setupMetalLB(iprange string) (bool, string) {

	f, err := os.Create(StateDir + "/kustomize/metallb/overlay/kustomization.yaml")
	if err != nil {
		return false, err.Error()
	}
	defer f.Close()

	_, err = f.WriteString("resources:\n  - ../base\n  - layer2-config.yaml")
	if err != nil {
		return false, err.Error()
	}
	f.Close()

	f, err = os.Create(StateDir + "/kustomize/metallb/overlay/layer2-config.yaml")
	if err != nil {
		return false, err.Error()
	}
	defer f.Close()

	_, err = f.WriteString("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  namespace: metallb-system\n  name: config\ndata:\n  config: |\n    address-pools:\n    - name: my-ip-space\n      protocol: layer2\n      addresses:\n      - " + iprange)
	if err != nil {
		return false, err.Error()
	}
	f.Close()

	return true, ""
}

func setupHelloKubic(arg string) (bool, string) {

	f, err := os.Create(StateDir + "/kustomize/hello-kubic/overlay/kustomization.yaml")
	if err != nil {
		return false, err.Error()
	}
	defer f.Close()

	if strings.EqualFold(arg, "NodePort") {
		// Use NodePort to make the service available
		_, err = f.WriteString("resources:\n  - ../base\npatchesStrategicMerge:\n  - patch_NodePort.yaml")
		if err != nil {
			return false, err.Error()
		}
		f.Close()

		f, err = os.Create(StateDir + "/kustomize/hello-kubic/overlay/patch_NodePort.yaml")
		if err != nil {
			return false, err.Error()
		}
		defer f.Close()

		_, err = f.WriteString("apiVersion: v1\nkind: Service\nmetadata:\n  name: hello-kubic\nspec:\n  type: NodePort")
		if err != nil {
			return false, err.Error()
		}
		f.Close()
	} else if strings.EqualFold(arg, "LoadBalancer") {
		// LoadBalancer without prefered IP
		_, err = f.WriteString("resources:\n  - ../base")
		if err != nil {
			return false, err.Error()
		}
		f.Close()
	} else {
		// LoadBalancer with prefered IP
		_, err = f.WriteString("resources:\n  - ../base\npatchesStrategicMerge:\n  - patch_LoadBalancerIP.yaml")
		if err != nil {
			return false, err.Error()
		}
		f.Close()

		f, err = os.Create(StateDir + "/kustomize/hello-kubic/overlay/patch_LoadBalancerIP.yaml")
		if err != nil {
			return false, err.Error()
		}
		defer f.Close()

		_, err = f.WriteString("apiVersion: v1\nkind: Service\nmetadata:\n  name: hello-kubic\nspec:\n  type: LoadBalancer\n  loadBalancerIP: " + arg)
		if err != nil {
			return false, err.Error()
		}
		f.Close()
	}

	return true, ""
}

func DeployKustomize(service string, argument string) (bool, string) {

	yamlDidExist := false
	if _, err := os.Stat(StateDir + "/kustomize/" + service + "/" + service + ".yaml"); err == nil {
		yamlDidExist = true
	}

	os.RemoveAll(StateDir + "/kustomize/" + service)
	err := os.MkdirAll(StateDir+"/kustomize/"+service+"/overlay",
		os.ModePerm)
	if err != nil {
		return false, "Cannot create " + StateDir + "/kustomize/" + service + "/overlay: " + err.Error()
	}
	err = os.Symlink("/usr/share/k8s-yaml/"+service,
		StateDir+"/kustomize/"+service+"/base")
	if err != nil {
		return false, "Cannot link " + service +
			" base directory: " + err.Error()
	}

	switch service {
	case "metallb":
		retval, message := setupMetalLB(argument)
		if retval != true {
			os.RemoveAll(StateDir + "/kustomize/" + service)
			return false, message
		}
	case "hello-kubic":
		retval, message := setupHelloKubic(argument)
		if retval != true {
			os.RemoveAll(StateDir + "/kustomize/" + service)
			return false, message
		}
	}
	retval, message := tools.ExecuteCmd("kustomize", "build",
		StateDir+"/kustomize/"+service+"/overlay")
	if retval != true {
		os.RemoveAll(StateDir + "/kustomize/" + service)
		return false, message
	}

	f, err := os.Create(StateDir + "/kustomize/" + service + "/" + service + ".yaml")
	if err != nil {
		return false, err.Error()
	}
	defer f.Close()
	_, err = f.WriteString(message)
	if err != nil {
		return false, err.Error()
	}
	f.Close()

	result, err := tools.Sha256sum_f(StateDir + "/kustomize/" + service + "/" + service + ".yaml")
	retval, message = tools.ExecuteCmd("kubectl",
		"--kubeconfig=/etc/kubernetes/admin.conf", "apply", "-f",
		StateDir+"/kustomize/"+service+"/"+service+".yaml")
	if retval != true {
		return false, message
	}

	if strings.EqualFold(service, "metallb") && !yamlDidExist {
		retval, message = tools.ExecuteCmd("kubectl",
			"--kubeconfig=/etc/kubernetes/admin.conf", "create",
			"secret", "generic", "-n", "metallb-system",
			"memberlist", "--from-literal=secretkey=\"$(openssl rand -base64 128)\"")
		if retval != true {
			return false, message
		}
	}

	cfg, err := ini.LooseLoad(StateDir + "/k8s-kustomize.conf")
	if err != nil {
		return false, "Cannot load k8s-kustomize.conf: " + err.Error()
	}

	cfg.Section("").Key(service).SetValue(result)
	err = cfg.SaveTo(StateDir + "/k8s-kustomize.conf")
	if err != nil {
		return false, "Cannot write k8s-kustomize.conf: " + err.Error()
	}

	return true, ""
}
