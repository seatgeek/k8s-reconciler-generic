package k8sutil

import (
	"github.com/seatgeek/k8s-reconciler-generic/apiobjects/apiutils"
	kv1 "k8s.io/api/core/v1"
	kr "k8s.io/apimachinery/pkg/api/resource"
	kmetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func UpsertVolume(volumes *[]kv1.Volume, volume kv1.Volume) {
	for i := range *volumes {
		if (*volumes)[i].Name == volume.Name {
			(*volumes)[i] = volume
			return
		}
	}
	*volumes = append(*volumes, volume)
}

func PVCVolume(name, pvcName string, readOnly bool) kv1.Volume {
	return kv1.Volume{
		Name: name,
		VolumeSource: kv1.VolumeSource{PersistentVolumeClaim: &kv1.PersistentVolumeClaimVolumeSource{
			ClaimName: pvcName,
			ReadOnly:  readOnly,
		}},
	}
}

func SecretVolume(name, secretName string, mode int32, items ...kv1.KeyToPath) kv1.Volume {
	return kv1.Volume{
		Name: name,
		VolumeSource: kv1.VolumeSource{Secret: &kv1.SecretVolumeSource{
			SecretName:  secretName,
			DefaultMode: &mode,
			Optional:    apiutils.Boolptr(false),
			Items:       items,
		}},
	}
}

func EmptyDirVolume(name string) kv1.Volume {
	return kv1.Volume{
		Name:         name,
		VolumeSource: kv1.VolumeSource{EmptyDir: &kv1.EmptyDirVolumeSource{}},
	}
}

func NewResources(cpuReq, cpuLim, memReq, memLim string) *kv1.ResourceRequirements {
	return &kv1.ResourceRequirements{
		Requests: map[kv1.ResourceName]kr.Quantity{
			kv1.ResourceCPU:    kr.MustParse(cpuReq),
			kv1.ResourceMemory: kr.MustParse(memReq),
		},
		Limits: map[kv1.ResourceName]kr.Quantity{
			kv1.ResourceCPU:    kr.MustParse(cpuLim),
			kv1.ResourceMemory: kr.MustParse(memLim),
		},
	}
}

func CombineVolumes(vss ...[]kv1.Volume) []kv1.Volume {
	var out []kv1.Volume
	for _, vs := range vss {
		for _, v := range vs {
			UpsertVolume(&out, v)
		}
	}
	return out
}

func Intstrptr(v intstr.IntOrString) *intstr.IntOrString {
	return &v
}

func IdentityPort(name string, protocol kv1.Protocol, exposedPort int32) kv1.ServicePort {
	return kv1.ServicePort{
		Name:       name,
		Protocol:   protocol,
		Port:       exposedPort,
		TargetPort: intstr.FromString(name),
	}
}

func LabelObject(om kmetav1.ObjectMeta, extraLabels map[string]string) kmetav1.ObjectMeta {
	om.Labels = apiutils.CombineMaps(om.Labels, extraLabels)
	return om
}

func CombineContainers(ccs ...[]kv1.Container) []kv1.Container {
	var res []kv1.Container
	for _, cc := range ccs {
		for _, c := range cc {
			UpsertContainer(&res, c)
		}
	}
	return res
}

func UpsertContainer(containers *[]kv1.Container, container kv1.Container) {
	for i := range *containers {
		if (*containers)[i].Name == container.Name {
			(*containers)[i] = container
			return
		}
	}
	*containers = append(*containers, container)
}

func FindContainer(name string, containerLists ...[]kv1.Container) *kv1.Container {
	for _, list := range containerLists {
		for i := range list {
			if list[i].Name == name {
				return &(list[i])
			}
		}
	}
	return nil
}

// MakeIdentityService creates a headless ClusterIP service with identity port mapping
func MakeIdentityService(om kmetav1.ObjectMeta, includeNonReady bool, ports ...kv1.ContainerPort) *kv1.Service {
	sports := make([]kv1.ServicePort, len(ports))
	for i, port := range ports {
		sports[i] = IdentityPort(port.Name, port.Protocol, port.ContainerPort)
	}
	s := &kv1.Service{
		ObjectMeta: om,
		Spec: kv1.ServiceSpec{
			Type:                     kv1.ServiceTypeClusterIP,
			ClusterIP:                "None",
			Selector:                 om.Labels,
			PublishNotReadyAddresses: includeNonReady,
			Ports:                    sports,
		},
	}
	return s
}
