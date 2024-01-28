package genrec

import (
	"emperror.dev/errors"
	json "github.com/json-iterator/go"
	"reflect"
	"strings"
)

func isPDB(resource map[string]interface{}) bool {
	if av, ok := resource["apiVersion"].(string); ok {
		if strings.HasPrefix(av, "policy/") && resource["kind"] == "PodDisruptionBudget" {
			return true
		}
	}
	return false
}

func getPDBSelector(resource map[string]interface{}) interface{} {
	if spec, ok := resource["spec"]; ok {
		if spec, ok := spec.(map[string]interface{}); ok {
			if selector, ok := spec["selector"]; ok {
				return selector
			}
		}
	}
	return nil
}

func deletePDBSelector(resource map[string]interface{}) ([]byte, error) {
	if spec, ok := resource["spec"]; ok {
		if spec, ok := spec.(map[string]interface{}); ok {
			delete(spec, "selector")
		}
	}

	obj, err := json.ConfigCompatibleWithStandardLibrary.Marshal(resource)
	if err != nil {
		return []byte{}, errors.Wrap(err, "could not marshal byte sequence")
	}

	return obj, nil
}

func ignorePDBSelector(current, modified []byte) ([]byte, []byte, error) {
	currentResource := map[string]interface{}{}
	if err := json.Unmarshal(current, &currentResource); err != nil {
		return []byte{}, []byte{}, errors.Wrap(err, "could not unmarshal byte sequence for current")
	}

	modifiedResource := map[string]interface{}{}
	if err := json.Unmarshal(modified, &modifiedResource); err != nil {
		return []byte{}, []byte{}, errors.Wrap(err, "could not unmarshal byte sequence for modified")
	}

	if isPDB(currentResource) && reflect.DeepEqual(getPDBSelector(currentResource), getPDBSelector(modifiedResource)) {
		var err error
		current, err = deletePDBSelector(currentResource)
		if err != nil {
			return nil, nil, errors.Wrap(err, "delete pdb selector from current")
		}
		modified, err = deletePDBSelector(modifiedResource)
		if err != nil {
			return nil, nil, errors.Wrap(err, "delete pdb selector from modified")
		}
	}

	return current, modified, nil
}
