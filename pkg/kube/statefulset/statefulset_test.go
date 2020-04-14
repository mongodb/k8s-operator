package statefulset

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	TestNamespace = "test-ns"
	TestName      = "test-name"
)

func TestGetContainerIndexByName(t *testing.T) {
	containers := []corev1.Container{
		{
			Name: "container-0",
		},
		{
			Name: "container-1",
		},
		{
			Name: "container-2",
		},
	}

	stsBuilder := defaultStatefulSetBuilder().SetPodTemplateSpec(podTemplateWithContainers(containers))
	idx, err := stsBuilder.getContainerIndexByName("container-0")

	assert.NoError(t, err)
	assert.NotEqual(t, -1, idx)
	assert.Equal(t, 0, idx)

	idx, err = stsBuilder.getContainerIndexByName("container-1")

	assert.NoError(t, err)
	assert.NotEqual(t, -1, idx)
	assert.Equal(t, 1, idx)

	idx, err = stsBuilder.getContainerIndexByName("container-2")

	assert.NoError(t, err)
	assert.NotEqual(t, -1, idx)
	assert.Equal(t, 2, idx)

	idx, err = stsBuilder.getContainerIndexByName("doesnt-exist")

	assert.Error(t, err)
	assert.Equal(t, -1, idx)
}

func TestAddVolumeAndMount(t *testing.T) {
	var stsBuilder *Builder
	var sts appsv1.StatefulSet
	var err error
	vmd := VolumeMountData{
		MountPath: "mount-path",
		Name:      "mount-name",
		Volume:    CreateVolumeFromConfigMap("mount-name", "config-map"),
	}

	stsBuilder = defaultStatefulSetBuilder().SetPodTemplateSpec(podTemplateWithContainers([]corev1.Container{{Name: "container-name"}})).AddVolumeAndMount("container-name", vmd)
	sts, err = stsBuilder.Build()

	// assert container was correctly updated with the volumes
	assert.NoError(t, err, "volume should successfully mount when the container exists")
	assert.Len(t, sts.Spec.Template.Spec.Containers[0].VolumeMounts, 1, "volume mount should have been added to the container in the stateful set")
	assert.Equal(t, sts.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name, "mount-name")
	assert.Equal(t, sts.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath, "mount-path")

	// assert the volumes were added to the podspec template
	assert.Len(t, sts.Spec.Template.Spec.Volumes, 1)
	assert.Equal(t, sts.Spec.Template.Spec.Volumes[0].Name, "mount-name")
	assert.NotNil(t, sts.Spec.Template.Spec.Volumes[0].VolumeSource.ConfigMap, "volume should have been configured from a config map source")
	assert.Nil(t, sts.Spec.Template.Spec.Volumes[0].VolumeSource.Secret, "volume should not have been configured from a secret source")

	stsBuilder = defaultStatefulSetBuilder().SetPodTemplateSpec(podTemplateWithContainers([]corev1.Container{{Name: "container-0"}, {Name: "container-1"}})).AddVolumeAndMount("container-0", vmd)
	sts, err = stsBuilder.Build()

	assert.NoError(t, err, "volume should successfully mount when the container exists")

	secretVmd := VolumeMountData{
		MountPath: "mount-path-secret",
		Name:      "mount-name-secret",
		Volume:    CreateVolumeFromSecret("mount-name-secret", "secret"),
	}

	// add a 2nd container to previously defined stsBuilder
	sts, err = stsBuilder.AddVolumeAndMount("container-1", secretVmd).Build()

	assert.NoError(t, err, "volume should successfully mount when the container exists")
	assert.Len(t, sts.Spec.Template.Spec.Containers[1].VolumeMounts, 1, "volume mount should have been added to the container in the stateful set")
	assert.Equal(t, sts.Spec.Template.Spec.Containers[1].VolumeMounts[0].Name, "mount-name-secret")
	assert.Equal(t, sts.Spec.Template.Spec.Containers[1].VolumeMounts[0].MountPath, "mount-path-secret")

	assert.Len(t, sts.Spec.Template.Spec.Volumes, 2)
	assert.Equal(t, sts.Spec.Template.Spec.Volumes[1].Name, "mount-name-secret")
	assert.Nil(t, sts.Spec.Template.Spec.Volumes[1].VolumeSource.ConfigMap, "volume should not have been configured from a config map source")
	assert.NotNil(t, sts.Spec.Template.Spec.Volumes[1].VolumeSource.Secret, "volume should have been configured from a secret source")

}

func TestAddVolumeClaimTemplates(t *testing.T) {
	claim := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: "claim-0",
		},
	}
	mount := corev1.VolumeMount{
		Name: "mount-0",
	}
	sts, err := defaultStatefulSetBuilder().SetPodTemplateSpec(podTemplateWithContainers([]corev1.Container{{Name: "container-name"}})).AddVolumeClaimTemplates([]corev1.PersistentVolumeClaim{claim}).AddVolumeMounts("container-name", []corev1.VolumeMount{mount}).Build()

	assert.NoError(t, err)
	assert.Len(t, sts.Spec.VolumeClaimTemplates, 1)
	assert.Equal(t, sts.Spec.VolumeClaimTemplates[0].Name, "claim-0")
	assert.Len(t, sts.Spec.Template.Spec.Containers[0].VolumeMounts, 1)
	assert.Equal(t, sts.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name, "mount-0")
}

func TestBuildStructImmutable(t *testing.T) {
	labels := map[string]string{"label_1": "a", "label_2": "b"}

	stsBuilder := defaultStatefulSetBuilder().SetLabels(labels)
	var sts appsv1.StatefulSet
	var err error
	sts, err = stsBuilder.Build()
	assert.NoError(t, err)
	assert.Len(t, sts.ObjectMeta.Labels, 2)

	delete(labels, "label_2")
	// checks that modifying the underlying object did not change the built statefulset
	assert.Len(t, sts.ObjectMeta.Labels, 2)

	sts, err = stsBuilder.Build()
	assert.NoError(t, err)
	assert.Len(t, sts.ObjectMeta.Labels, 1)
}

func defaultStatefulSetBuilder() *Builder {
	return NewBuilder().
		SetName(TestName).
		SetNamespace(TestNamespace).
		SetServiceName(fmt.Sprintf("%s-svc", TestName)).
		SetLabels(map[string]string{}).
		SetUpdateStrategy(appsv1.RollingUpdateStatefulSetStrategyType)
}

func podTemplateWithContainers(containers []corev1.Container) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: containers,
		},
	}
}

func TestBuildStatefulSet_SortedEnvVariables(t *testing.T) {
	podTemplateSpec := podTemplateWithContainers([]corev1.Container{{Name: "container-name"}})
	podTemplateSpec.Spec.Containers[0].Env = []corev1.EnvVar{
		{Name: "one", Value: "X"},
		{Name: "two", Value: "Y"},
		{Name: "three", Value: "Z"},
	}
	sts, err := defaultStatefulSetBuilder().SetPodTemplateSpec(podTemplateSpec).Build()
	assert.NoError(t, err)
	expectedVars := []corev1.EnvVar{
		{Name: "one", Value: "X"},
		{Name: "three", Value: "Z"},
		{Name: "two", Value: "Y"},
	}
	assert.Equal(t, expectedVars, sts.Spec.Template.Spec.Containers[0].Env)
}

// The following test functions mainly test that the functional options implementation is sane.
func TestCreateVolumeMountReadOnly(t *testing.T) {
	mount := CreateVolumeMount("this-volume-mount", "my-path")
	assert.False(t, mount.ReadOnly)

	// false is the default
	mount = CreateVolumeMount("this-volume-mount", "my-path", WithReadOnly(false))
	assert.False(t, mount.ReadOnly)

	mount = CreateVolumeMount("this-volume-mount", "/my-path", WithReadOnly(true))
	assert.True(t, mount.ReadOnly)
}

func TestCreateVolumeMountWithSubPath(t *testing.T) {
	mount := CreateVolumeMount("this-volume-mount", "my-path")
	assert.Equal(t, mount.SubPath, "")

	mount = CreateVolumeMount("this-volume-mount", "my-path", WithSubPath(""))
	assert.Equal(t, mount.SubPath, "")

	mount = CreateVolumeMount("this-volume-mount", "my-path", WithSubPath("some-path"))
	assert.Equal(t, mount.SubPath, "some-path")
}

func TestCreateVolumeMountWithMultipleOptions(t *testing.T) {
	mount := CreateVolumeMount("this-volume-mount", "my-path", WithSubPath("our-subpath"), WithReadOnly(true))
	assert.Equal(t, mount.SubPath, "our-subpath")
	assert.True(t, mount.ReadOnly)
}

func TestHaveEqualSpec(t *testing.T) {
	t.Run("Identical StatefulSet", func(t *testing.T) {
		builtSts, _ := defaultStatefulSetBuilder().Build()
		existingSts, _ := defaultStatefulSetBuilder().Build()
		areEqual, err := HaveEqualSpec(builtSts, existingSts)
		assert.NoError(t, err)
		assert.True(t, areEqual, "When both stateful sets are identical, these should be considered equivalent")
	})
	t.Run("Built StatefulSet is different from existing StatefulSet", func(t *testing.T) {
		builtSts, _ := defaultStatefulSetBuilder().SetUpdateStrategy(appsv1.OnDeleteStatefulSetStrategyType).Build()
		existingSts, _ := defaultStatefulSetBuilder().Build()
		areEqual, err := HaveEqualSpec(builtSts, existingSts)
		assert.NoError(t, err)
		assert.False(t, areEqual, "We have specified a field that is different from the existring StatefulSet, so these should be considered different")
	})
	t.Run("Existing StatefulSet has values we don't specify", func(t *testing.T) {
		builtSts, _ := defaultStatefulSetBuilder().Build()
		existingSts, _ := defaultStatefulSetBuilder().Build()
		revHistoryList := int32(30)
		existingSts.Spec.RevisionHistoryLimit = &revHistoryList

		areEqual, err := HaveEqualSpec(builtSts, existingSts)
		assert.NoError(t, err)
		assert.True(t, areEqual, "Specs should be considered equal even though the existing StatefulSet has fields we are not interested in")
	})

	t.Run("Metadata differences", func(t *testing.T) {
		builtSts, _ := defaultStatefulSetBuilder().SetName("different-name").Build()
		existingSts, _ := defaultStatefulSetBuilder().Build()
		areEqual, err := HaveEqualSpec(builtSts, existingSts)
		assert.NoError(t, err)
		assert.True(t, areEqual, "Metadata differences should not be considered, we are just looking at spec")
	})

	t.Run("Change to PodSpecTemplate", func(t *testing.T) {
		builtSts, _ := defaultStatefulSetBuilder().Build()
		existingSts, _ := defaultStatefulSetBuilder().Build()
		t.Run("Same Container added", func(t *testing.T) {
			builtSts.Spec.Template.Spec.Containers = []corev1.Container{{Name: "container-1"}}
			existingSts.Spec.Template.Spec.Containers = []corev1.Container{{Name: "container-1"}}
			areEqual, err := HaveEqualSpec(builtSts, existingSts)
			assert.NoError(t, err)
			assert.True(t, areEqual, "Having the same container should be equal")
		})

		t.Run("Existing StatefulSet has init containers", func(t *testing.T) {
			builtSts.Spec.Template.Spec.Containers = []corev1.Container{{Name: "container-1"}}
			existingSts.Spec.Template.Spec.Containers = []corev1.Container{{Name: "container-1"}}
			existingSts.Spec.Template.Spec.InitContainers = []corev1.Container{{Name: "container-1-init"}}
			areEqual, err := HaveEqualSpec(builtSts, existingSts)
			assert.NoError(t, err)
			assert.True(t, areEqual, "The existing StatefulSet has a field we have not touched in the spec (initContainers), this should be ignored in spec comparison")
		})
		t.Run("Different Container added", func(t *testing.T) {
			builtSts.Spec.Template.Spec.Containers = []corev1.Container{{Name: "container-2"}}
			existingSts.Spec.Template.Spec.Containers = []corev1.Container{{Name: "container-3"}}
			areEqual, err := HaveEqualSpec(builtSts, existingSts)
			assert.NoError(t, err)
			assert.False(t, areEqual, "Metadata differences should not be considered, we are just looking at spec")
		})
		t.Run("Image Change", func(t *testing.T) {
			builtSts.Spec.Template.Spec.Containers = []corev1.Container{{Name: "container-1", Image: "image-1"}}
			existingSts.Spec.Template.Spec.Containers = []corev1.Container{{Name: "container-1", Image: "image-2"}}
			areEqual, err := HaveEqualSpec(builtSts, existingSts)
			assert.NoError(t, err)
			assert.False(t, areEqual, "A single different field in an element in a list should result in the specs being different")
		})
	})
}
