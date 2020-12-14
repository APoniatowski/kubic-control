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
	log "github.com/sirupsen/logrus"
	"github.com/thkukuk/kubic-control/pkg/tools"
	"gopkg.in/ini.v1"
)

func UpdateAll(forced bool) (bool, string) {

	cfg, err := ini.Load("/var/lib/kubic-control/k8s-yaml.conf")
	if err != nil {
		return false, "Cannot load k8s-yaml.conf: " + err.Error()
	}

	keys := cfg.Section("").KeyStrings()
	for _, key := range keys {
		if forced {
			// force, so always update even if not changed
			success, message := UpdateFile(key)
			if success != true {
				return success, message
			}
		} else {
			value := cfg.Section("").Key(key).String()
			hash, _ := tools.Sha256sum_f(key)

			if hash != value {
				log.Infof("%s has changed, updating", key)
				success, message := UpdateFile(key)
				if success != true {
					return success, message
				}
			} else {
				log.Infof("%s has not changed, ignoring", key)
			}
		}
	}

	// Update kustomize installed services
	cfg, err = ini.Load("/var/lib/kubic-control/k8s-kustomize.conf")
	if err != nil {
		return false, "Cannot load k8s-kustomize.conf: " + err.Error()
	}

	keys = cfg.Section("").KeyStrings()
	for _, key := range keys {
		if forced {
			// force, so always update even if not changed
			success, message := UpdateKustomize(key)
			if success != true {
				return success, message
			}
		} else {
			retval, message := tools.ExecuteCmd("kustomize", "build",
				StateDir+"/kustomize/"+key+"/overlay")
			if retval != true {
				return retval, message
			}

			value := cfg.Section("").Key(key).String()
			hash, _ := tools.Sha256sum_f(message)

			if hash != value {
				log.Infof("%s has changed, updating", key)
				success, message := UpdateKustomize(key)
				if success != true {
					return success, message
				}
			} else {
				log.Infof("%s has not changed, ignoring", key)
			}
		}
	}

	// Update helm installed services
	cfg, err = ini.Load("/var/lib/kubic-control/k8s-helm.conf")
	if err != nil {
		return false, "Cannot load k8s-helm.conf: " + err.Error()
	}

	keys = cfg.Section("").KeyStrings()
	for _, chartName := range keys {
		releaseName := cfg.Section("").Key(chartName + ".releaseName").String()
		valuesPath := cfg.Section("").Key(chartName + ".valuesPath").String()
		namespace := cfg.Section("").Key(chartName + ".namespace").String()
		if forced {
			// force, so always update even if not changed
			err = UpdateHelm(chartName, releaseName, valuesPath, namespace)
			if err != nil {
				return false, err.Error()
			}
		} else {
			hash := cfg.Section("").Key(chartName).String()
			needsUpdate, err := checkHelmUpdate(chartName, releaseName, valuesPath, namespace, hash)
			if err != nil {
				return false, err.Error()
			}
			if needsUpdate {
				log.Infof("%s has changed, updating")
				err = UpdateHelm(chartName, releaseName, valuesPath, namespace)
				if err != nil {
					return false, err.Error()
				}
			} else {
				log.Infof("%s has not changed, ignoring")
			}
		}
	}

	return true, ""
}
