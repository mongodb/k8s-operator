package podtemplatespec

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mongodb/mongodb-kubernetes-operator/pkg/kube/container"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestPodTemplateSpec(t *testing.T) {
	volumeMount1 := corev1.VolumeMount{
		Name: "vol-1",
	}
	volumeMount2 := corev1.VolumeMount{
		Name: "vol-2",
	}

	p := New(
		WithVolume(corev1.Volume{
			Name: "vol-1",
		}),
		WithVolume(corev1.Volume{
			Name: "vol-2",
		}),
		WithFsGroup(100),
		WithImagePullSecrets("pull-secrets"),
		WithInitContainerByIndex(0, container.Apply(
			container.WithName("init-container-0"),
			container.WithImage("init-image"),
			container.WithVolumeMounts([]corev1.VolumeMount{volumeMount1}),
		)),
		WithContainerByIndex(0, container.Apply(
			container.WithName("container-0"),
			container.WithImage("image"),
			container.WithVolumeMounts([]corev1.VolumeMount{volumeMount1}),
		)),
		WithContainerByIndex(1, container.Apply(
			container.WithName("container-1"),
			container.WithImage("image"),
		)),
		WithVolumeMounts("init-container-0", volumeMount2),
		WithVolumeMounts("container-0", volumeMount2),
		WithVolumeMounts("container-1", volumeMount1, volumeMount2),
	)

	assert.Len(t, p.Spec.Volumes, 2)
	assert.Equal(t, p.Spec.Volumes[0].Name, "vol-1")
	assert.Equal(t, p.Spec.Volumes[1].Name, "vol-2")

	expected := int64(100)
	assert.Equal(t, &expected, p.Spec.SecurityContext.FSGroup)

	assert.Len(t, p.Spec.ImagePullSecrets, 1)
	assert.Equal(t, "pull-secrets", p.Spec.ImagePullSecrets[0].Name)

	assert.Len(t, p.Spec.InitContainers, 1)
	assert.Equal(t, "init-container-0", p.Spec.InitContainers[0].Name)
	assert.Equal(t, "init-image", p.Spec.InitContainers[0].Image)
	assert.Equal(t, []corev1.VolumeMount{volumeMount1, volumeMount2}, p.Spec.InitContainers[0].VolumeMounts)

	assert.Len(t, p.Spec.Containers, 2)

	assert.Equal(t, "container-0", p.Spec.Containers[0].Name)
	assert.Equal(t, "image", p.Spec.Containers[0].Image)
	assert.Equal(t, []corev1.VolumeMount{volumeMount1, volumeMount2}, p.Spec.Containers[0].VolumeMounts)

	assert.Equal(t, "container-1", p.Spec.Containers[1].Name)
	assert.Equal(t, "image", p.Spec.Containers[1].Image)
	assert.Equal(t, []corev1.VolumeMount{volumeMount1, volumeMount2}, p.Spec.Containers[0].VolumeMounts)
}

func TestPodTemplateSpec_MultipleEditsToContainer(t *testing.T) {
	p := New(
		WithContainerByIndex(0,
			container.Apply(
				container.WithName("container-0"),
			)),
		WithContainerByIndex(0,
			container.Apply(
				container.WithImage("image"),
			)),
		WithContainerByIndex(0,
			container.Apply(
				container.WithImagePullPolicy(corev1.PullAlways),
			)),
		WithContainer("container-0", container.Apply(
			container.WithCommand([]string{"cmd"}),
		)),
	)

	assert.Len(t, p.Spec.Containers, 1)
	c := p.Spec.Containers[0]
	assert.Equal(t, "container-0", c.Name)
	assert.Equal(t, "image", c.Image)
	assert.Equal(t, corev1.PullAlways, c.ImagePullPolicy)
	assert.Equal(t, "cmd", c.Command[0])
}

func TestMerge(t *testing.T) {
	defaultSpec := getDefaultPodSpec()
	customSpec := getCustomPodSpec()

	mergedSpec, err := MergePodTemplateSpecs(defaultSpec, customSpec)
	assert.NoError(t, err)

	initContainerDefault := getDefaultContainer()
	initContainerDefault.Name = "init-container-default"

	initContainerCustom := getCustomContainer()
	initContainerCustom.Name = "init-container-custom"

	expected := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-default-name",
			Namespace: "my-default-namespace",
			Labels: map[string]string{
				"app":    "operator",
				"custom": "some",
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"node-0": "node-0",
				"node-1": "node-1",
			},
			ServiceAccountName:            "my-service-account-override",
			TerminationGracePeriodSeconds: int64Ref(11),
			ActiveDeadlineSeconds:         int64Ref(10),
			NodeName:                      "my-node-name",
			RestartPolicy:                 corev1.RestartPolicyAlways,
			Containers: []corev1.Container{
				getDefaultContainer(),
				getCustomContainer(),
			},
			InitContainers: []corev1.Container{
				initContainerDefault,
				initContainerCustom,
			},
			Affinity: affinity("zone", "custom"),
		},
	}
	assert.Equal(t, expected, mergedSpec)
}

func TestMergeFromEmpty(t *testing.T) {
	defaultPodSpec := corev1.PodTemplateSpec{}
	customPodSpecTemplate := getCustomPodSpec()

	mergedPodTemplateSpec, err := MergePodTemplateSpecs(defaultPodSpec, customPodSpecTemplate)

	assert.NoError(t, err)
	assert.Equal(t, customPodSpecTemplate, mergedPodTemplateSpec)
}

func TestMergeWithEmpty(t *testing.T) {
	defaultPodSpec := getDefaultPodSpec()
	customPodSpecTemplate := corev1.PodTemplateSpec{}

	mergedPodTemplateSpec, err := MergePodTemplateSpecs(defaultPodSpec, customPodSpecTemplate)

	assert.NoError(t, err)
	assert.Equal(t, defaultPodSpec, mergedPodTemplateSpec)
}

func TestMultipleMerges(t *testing.T) {
	defaultPodSpec := getDefaultPodSpec()
	customPodSpecTemplate := getCustomPodSpec()

	referenceSpec, err := MergePodTemplateSpecs(defaultPodSpec, customPodSpecTemplate)
	assert.NoError(t, err)

	mergedSpec := defaultPodSpec

	// multiple merges must give the same result
	for i := 0; i < 3; i++ {
		mergedSpec, err := MergePodTemplateSpecs(mergedSpec, customPodSpecTemplate)
		assert.NoError(t, err)
		assert.Equal(t, referenceSpec, mergedSpec)
	}
}

func TestMergeContainer(t *testing.T) {
	vol0 := corev1.VolumeMount{Name: "container-0.volume-mount-0"}
	sideCarVol := corev1.VolumeMount{Name: "container-1.volume-mount-0"}

	anotherVol := corev1.VolumeMount{Name: "another-mount"}

	overrideDefaultContainer := corev1.Container{Name: "container-0"}
	overrideDefaultContainer.Image = "overridden"
	overrideDefaultContainer.ReadinessProbe = &corev1.Probe{PeriodSeconds: 20}

	otherDefaultContainer := getDefaultContainer()
	otherDefaultContainer.Name = "default-side-car"
	otherDefaultContainer.VolumeMounts = []corev1.VolumeMount{sideCarVol}

	overrideOtherDefaultContainer := otherDefaultContainer
	overrideOtherDefaultContainer.Env = []corev1.EnvVar{{Name: "env_var", Value: "xxx"}}
	overrideOtherDefaultContainer.VolumeMounts = []corev1.VolumeMount{anotherVol}

	defaultSpec := getDefaultPodSpec()
	defaultSpec.Spec.Containers = []corev1.Container{getDefaultContainer(), otherDefaultContainer}

	customSpec := getCustomPodSpec()
	customSpec.Spec.Containers = []corev1.Container{getCustomContainer(), overrideDefaultContainer, overrideOtherDefaultContainer}

	mergedSpec, err := MergePodTemplateSpecs(defaultSpec, customSpec)
	assert.NoError(t, err)

	assert.Len(t, mergedSpec.Spec.Containers, 3)
	assert.Equal(t, getCustomContainer(), mergedSpec.Spec.Containers[2])

	firstExpected := corev1.Container{
		Name:         "container-0",
		VolumeMounts: []corev1.VolumeMount{vol0},
		Image:        "overridden",
		ReadinessProbe: &corev1.Probe{
			// only "periodSeconds" was overwritten - other fields stayed untouched
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{Path: "/foo"},
			},
			PeriodSeconds: 20,
		},
	}
	assert.Equal(t, firstExpected, mergedSpec.Spec.Containers[0])

	secondExpected := corev1.Container{
		Name:         "default-side-car",
		Image:        "image-0",
		VolumeMounts: []corev1.VolumeMount{sideCarVol, anotherVol},
		Env: []corev1.EnvVar{
			corev1.EnvVar{
				Name:  "env_var",
				Value: "xxx",
			},
		},
		ReadinessProbe: otherDefaultContainer.ReadinessProbe,
	}
	assert.Equal(t, secondExpected, mergedSpec.Spec.Containers[1])
}

func TestMergeVolumeMounts(t *testing.T) {
	vol0 := corev1.VolumeMount{Name: "container-0.volume-mount-0"}
	vol1 := corev1.VolumeMount{Name: "another-mount"}
	volumeMounts := []corev1.VolumeMount{vol0, vol1}

	mergedVolumeMounts, err := mergeVolumeMounts(nil, volumeMounts)
	assert.NoError(t, err)
	assert.Equal(t, []corev1.VolumeMount{vol0, vol1}, mergedVolumeMounts)

	vol2 := vol1
	vol2.MountPath = "/somewhere"
	mergedVolumeMounts, err = mergeVolumeMounts([]corev1.VolumeMount{vol2}, []corev1.VolumeMount{vol0, vol1})
	assert.NoError(t, err)
	assert.Equal(t, []corev1.VolumeMount{vol2, vol0}, mergedVolumeMounts)
}

func int64Ref(i int64) *int64 {
	return &i
}

func getDefaultPodSpec() corev1.PodTemplateSpec {
	initContainer := getDefaultContainer()
	initContainer.Name = "init-container-default"
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-default-name",
			Namespace: "my-default-namespace",
			Labels:    map[string]string{"app": "operator"},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"node-0": "node-0",
			},
			ServiceAccountName:            "my-default-service-account",
			TerminationGracePeriodSeconds: int64Ref(12),
			ActiveDeadlineSeconds:         int64Ref(10),
			Containers:                    []corev1.Container{getDefaultContainer()},
			InitContainers:                []corev1.Container{initContainer},
			Affinity:                      affinity("hostname", "default"),
		},
	}
}

func getCustomPodSpec() corev1.PodTemplateSpec {
	initContainer := getCustomContainer()
	initContainer.Name = "init-container-custom"
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"custom": "some"},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"node-1": "node-1",
			},
			ServiceAccountName:            "my-service-account-override",
			TerminationGracePeriodSeconds: int64Ref(11),
			NodeName:                      "my-node-name",
			RestartPolicy:                 corev1.RestartPolicyAlways,
			Containers:                    []corev1.Container{getCustomContainer()},
			InitContainers:                []corev1.Container{initContainer},
			Affinity:                      affinity("zone", "custom"),
		},
	}
}

func affinity(antiAffinityKey, nodeAffinityKey string) *corev1.Affinity {
	return &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
				PodAffinityTerm: corev1.PodAffinityTerm{
					TopologyKey: antiAffinityKey,
				},
			}},
		},
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
				MatchFields: []corev1.NodeSelectorRequirement{{
					Key: nodeAffinityKey,
				}},
			}}},
		},
	}
}

func getDefaultContainer() corev1.Container {
	return corev1.Container{
		Name:  "container-0",
		Image: "image-0",
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{HTTPGet: &corev1.HTTPGetAction{
				Path: "/foo",
			}},
			PeriodSeconds: 10,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name: "container-0.volume-mount-0",
			},
		},
	}
}

func getCustomContainer() corev1.Container {
	return corev1.Container{
		Name:  "container-1",
		Image: "image-1",
	}
}
