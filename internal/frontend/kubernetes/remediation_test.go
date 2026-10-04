// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"context"
	"fmt"
	"testing"
)

func TestPodSecurityInheritanceDoesNotLeakBetweenContainers(t *testing.T) {
	data := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: p
  labels: {app: p}
spec:
  securityContext: {runAsUser: 1000, runAsGroup: 2000}
  containers:
  - name: first
    image: busybox
    securityContext: {runAsUser: 3000, readOnlyRootFilesystem: true}
  - name: second
    image: busybox
  initContainers:
  - name: init
    image: busybox
`)
	r, err := Compile(context.Background(), data, Options{})
	if err != nil || r.Diagnostics.HasErrors() {
		t.Fatalf("%v %+v", err, r.Diagnostics)
	}
	w := r.Application.Workloads[0]
	if w.Labels["app"] != "p" {
		t.Fatal("Pod labels lost")
	}
	if w.Template.Containers[0].User != "3000:2000" || w.Template.Containers[1].User != "1000:2000" || w.Template.InitContainers[0].User != "1000:2000" {
		t.Fatalf("%+v", w.Template)
	}
	if w.Template.Containers[1].SecurityProfile.ReadOnlyRootFilesystem || w.Template.SecurityProfile.ReadOnlyRootFilesystem {
		t.Fatal("container policy leaked")
	}
}

func TestUnsupportedSecurityIsRejected(t *testing.T) {
	for _, policy := range []string{"runAsNonRoot: true", "runAsUser: -1", "runAsGroup: 4294967296", "seccompProfile: {type: RuntimeDefault}", "capabilities: {drop: [ALL]}", "allowPrivilegeEscalation: true"} {
		data := fmt.Sprintf("apiVersion: v1\nkind: Pod\nmetadata: {name: p}\nspec:\n  containers:\n  - name: app\n    image: busybox\n    securityContext: {%s}\n", policy)
		r, err := Compile(context.Background(), []byte(data), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !r.Diagnostics.HasErrors() {
			t.Fatalf("accepted %s", policy)
		}
	}
}

func TestPVCAliasesResolveToOneClaim(t *testing.T) {
	for _, alias := range []string{"data", "alias"} {
		data := fmt.Sprintf(`apiVersion: v1
kind: PersistentVolumeClaim
metadata: {name: data}
spec:
  accessModes: [ReadWriteOnce]
  resources: {requests: {storage: 64Mi}}
---
apiVersion: v1
kind: Pod
metadata: {name: p}
spec:
  volumes:
  - name: %s
    persistentVolumeClaim: {claimName: data, readOnly: true}
  - name: second
    persistentVolumeClaim: {claimName: data}
  containers:
  - name: app
    image: busybox
    volumeMounts:
    - {name: %s, mountPath: /data}
    - {name: second, mountPath: /other}
`, alias, alias)
		r, err := Compile(context.Background(), []byte(data), Options{})
		if err != nil || r.Diagnostics.HasErrors() {
			t.Fatalf("%v %+v", err, r.Diagnostics)
		}
		if len(r.Application.Volumes) != 1 || r.Application.Volumes[0].Name != "data" || r.Application.Volumes[0].Capacity != 64<<20 {
			t.Fatalf("%+v", r.Application.Volumes)
		}
		mounts := r.Application.Workloads[0].Template.Containers[0].Mounts
		if mounts[0].Volume != "data" || mounts[1].Volume != "data" || !mounts[0].ReadOnly {
			t.Fatalf("%+v", mounts)
		}
	}
}
