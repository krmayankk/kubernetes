/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package statefulset

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/apimachinery/pkg/util/intstr"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/kubernetes/pkg/apis/apps"
	api "k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/features"
)

func TestStatefulSetStrategy(t *testing.T) {
	ctx := genericapirequest.NewDefaultContext()
	if !Strategy.NamespaceScoped() {
		t.Errorf("StatefulSet must be namespace scoped")
	}
	if Strategy.AllowCreateOnUpdate() {
		t.Errorf("StatefulSet should not allow create on update")
	}

	validSelector := map[string]string{"a": "b"}
	validPodTemplate := api.PodTemplate{
		Template: api.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: validSelector,
			},
			Spec: api.PodSpec{
				RestartPolicy: api.RestartPolicyAlways,
				DNSPolicy:     api.DNSClusterFirst,
				Containers:    []api.Container{{Name: "abc", Image: "image", ImagePullPolicy: "IfNotPresent"}},
			},
		},
	}
	ps := &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "abc", Namespace: metav1.NamespaceDefault},
		Spec: apps.StatefulSetSpec{
			PodManagementPolicy: apps.OrderedReadyPodManagement,
			Selector:            &metav1.LabelSelector{MatchLabels: validSelector},
			Template:            validPodTemplate.Template,
			UpdateStrategy:      apps.StatefulSetUpdateStrategy{Type: apps.RollingUpdateStatefulSetStrategyType},
		},
		Status: apps.StatefulSetStatus{Replicas: 3},
	}

	Strategy.PrepareForCreate(ctx, ps)
	if ps.Status.Replicas != 0 {
		t.Error("StatefulSet should not allow setting status.replicas on create")
	}
	errs := Strategy.Validate(ctx, ps)
	if len(errs) != 0 {
		t.Errorf("unexpected error validating %v", errs)
	}

	// Just Spec.Replicas is allowed to change
	validPs := &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: ps.Name, Namespace: ps.Namespace, ResourceVersion: "1", Generation: 1},
		Spec: apps.StatefulSetSpec{
			PodManagementPolicy: apps.OrderedReadyPodManagement,
			Selector:            ps.Spec.Selector,
			Template:            validPodTemplate.Template,
			UpdateStrategy:      apps.StatefulSetUpdateStrategy{Type: apps.RollingUpdateStatefulSetStrategyType},
		},
		Status: apps.StatefulSetStatus{Replicas: 4},
	}
	Strategy.PrepareForUpdate(ctx, validPs, ps)
	errs = Strategy.ValidateUpdate(ctx, validPs, ps)
	if len(errs) != 0 {
		t.Errorf("updating spec.Replicas is allowed on a statefulset: %v", errs)
	}

	validPs.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"a": "bar"}}
	Strategy.PrepareForUpdate(ctx, validPs, ps)
	errs = Strategy.ValidateUpdate(ctx, validPs, ps)
	if len(errs) == 0 {
		t.Errorf("expected a validation error since updates are disallowed on statefulsets.")
	}
}

func TestStatefulsetDefaultGarbageCollectionPolicy(t *testing.T) {
	// Make sure we correctly implement the interface.
	// Otherwise a typo could silently change the default.
	var gcds rest.GarbageCollectionDeleteStrategy = Strategy
	tests := []struct {
		requestInfo      genericapirequest.RequestInfo
		expectedGCPolicy rest.GarbageCollectionPolicy
		isNilRequestInfo bool
	}{
		{
			genericapirequest.RequestInfo{
				APIGroup:   "apps",
				APIVersion: "v1beta1",
				Resource:   "statefulsets",
			},
			rest.OrphanDependents,
			false,
		},
		{
			genericapirequest.RequestInfo{
				APIGroup:   "apps",
				APIVersion: "v1beta2",
				Resource:   "statefulsets",
			},
			rest.OrphanDependents,
			false,
		},
		{
			genericapirequest.RequestInfo{
				APIGroup:   "apps",
				APIVersion: "v1",
				Resource:   "statefulsets",
			},
			rest.DeleteDependents,
			false,
		},
		{
			expectedGCPolicy: rest.DeleteDependents,
			isNilRequestInfo: true,
		},
	}

	for _, test := range tests {
		context := genericapirequest.NewContext()
		if !test.isNilRequestInfo {
			context = genericapirequest.WithRequestInfo(context, &test.requestInfo)
		}
		if got, want := gcds.DefaultGarbageCollectionPolicy(context), test.expectedGCPolicy; got != want {
			t.Errorf("%s/%s: DefaultGarbageCollectionPolicy() = %#v, want %#v", test.requestInfo.APIGroup,
				test.requestInfo.APIVersion, got, want)
		}
	}
}

func TestStatefulSetStatusStrategy(t *testing.T) {
	ctx := genericapirequest.NewDefaultContext()
	if !StatusStrategy.NamespaceScoped() {
		t.Errorf("StatefulSet must be namespace scoped")
	}
	if StatusStrategy.AllowCreateOnUpdate() {
		t.Errorf("StatefulSet should not allow create on update")
	}
	validSelector := map[string]string{"a": "b"}
	validPodTemplate := api.PodTemplate{
		Template: api.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: validSelector,
			},
			Spec: api.PodSpec{
				RestartPolicy: api.RestartPolicyAlways,
				DNSPolicy:     api.DNSClusterFirst,
				Containers:    []api.Container{{Name: "abc", Image: "image", ImagePullPolicy: "IfNotPresent"}},
			},
		},
	}
	oldPS := &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "abc", Namespace: metav1.NamespaceDefault, ResourceVersion: "10"},
		Spec: apps.StatefulSetSpec{
			Replicas:       3,
			Selector:       &metav1.LabelSelector{MatchLabels: validSelector},
			Template:       validPodTemplate.Template,
			UpdateStrategy: apps.StatefulSetUpdateStrategy{Type: apps.RollingUpdateStatefulSetStrategyType},
		},
		Status: apps.StatefulSetStatus{
			Replicas: 1,
		},
	}
	newPS := &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "abc", Namespace: metav1.NamespaceDefault, ResourceVersion: "9"},
		Spec: apps.StatefulSetSpec{
			Replicas:       1,
			Selector:       &metav1.LabelSelector{MatchLabels: validSelector},
			Template:       validPodTemplate.Template,
			UpdateStrategy: apps.StatefulSetUpdateStrategy{Type: apps.RollingUpdateStatefulSetStrategyType},
		},
		Status: apps.StatefulSetStatus{
			Replicas: 2,
		},
	}
	StatusStrategy.PrepareForUpdate(ctx, newPS, oldPS)
	if newPS.Status.Replicas != 2 {
		t.Errorf("StatefulSet status updates should allow change of pods: %v", newPS.Status.Replicas)
	}
	if newPS.Spec.Replicas != 3 {
		t.Errorf("StatefulSet status updates should not clobber spec: %v", newPS.Spec)
	}
	errs := StatusStrategy.ValidateUpdate(ctx, newPS, oldPS)
	if len(errs) != 0 {
		t.Errorf("unexpected error %v", errs)
	}
}

func makeStatefulSetWithMaxUnavailable(maxUnavailable *int) *apps.StatefulSet {
	rollingUpdate := apps.RollingUpdateStatefulSetStrategy{}
	if maxUnavailable != nil {
		maxUnavailableIntStr := intstr.FromInt(*maxUnavailable)
		rollingUpdate = apps.RollingUpdateStatefulSetStrategy{
			MaxUnavailable: &maxUnavailableIntStr,
		}
	}

	return &apps.StatefulSet{
		Spec: apps.StatefulSetSpec{
			UpdateStrategy: apps.StatefulSetUpdateStrategy{
				Type:          apps.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &rollingUpdate,
			},
		},
	}
}

func TestDropDisabledField(t *testing.T) {
	maxUnavailable := 3
	testCases := []struct {
		name                 string
		enableMaxUnavailable bool
		sts                  *apps.StatefulSet
		oldSts               *apps.StatefulSet
		expectedSts          *apps.StatefulSet
	}{
		{
			name:                 "MaxUnavailable not enabled, field not used",
			enableMaxUnavailable: false,
			sts:                  makeStatefulSetWithMaxUnavailable(nil),
			oldSts:               nil,
			expectedSts:          makeStatefulSetWithMaxUnavailable(nil),
		},
		{
			name:                 "MaxUnavailable not enabled, field used in new, not in old",
			enableMaxUnavailable: false,
			sts:                  makeStatefulSetWithMaxUnavailable(&maxUnavailable),
			oldSts:               nil,
			expectedSts:          makeStatefulSetWithMaxUnavailable(nil),
		},
		{
			name:                 "MaxUnavailable not enabled, field used in old and new",
			enableMaxUnavailable: false,
			sts:                  makeStatefulSetWithMaxUnavailable(&maxUnavailable),
			oldSts:               makeStatefulSetWithMaxUnavailable(&maxUnavailable),
			expectedSts:          makeStatefulSetWithMaxUnavailable(&maxUnavailable),
		},
		{
			name:                 "MaxUnavailable enabled, field used",
			enableMaxUnavailable: true,
			sts:                  makeStatefulSetWithMaxUnavailable(&maxUnavailable),
			oldSts:               nil,
			expectedSts:          makeStatefulSetWithMaxUnavailable(&maxUnavailable),
		},

		/* add more tests for other dropped fields as needed */
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.MaxUnavailableStatefulSet, tc.enableMaxUnavailable)()
			old := tc.oldSts.DeepCopy()
			dropStatefulSetDisabledFields(tc.sts, tc.oldSts)

			// old node  should never be changed
			if !reflect.DeepEqual(tc.oldSts, old) {
				t.Errorf("%v: old statefulset changed: %v", tc.name, diff.ObjectReflectDiff(tc.oldSts, old))
			}

			if !reflect.DeepEqual(tc.sts, tc.expectedSts) {
				t.Errorf("%v: unexpected statefulset spec: %v", tc.name, diff.ObjectReflectDiff(tc.sts, tc.expectedSts))
			}
		})
	}

}
