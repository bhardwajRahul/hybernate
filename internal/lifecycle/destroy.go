/*
Copyright 2026.

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

package lifecycle

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/okedeji/hybernate/api/v1alpha1"
)

// Destroyer handles workload deletion and optional PVC retention cleanup.
type Destroyer struct {
	client client.Client
	clock  func() metav1.Time
}

func NewDestroyer(c client.Client) *Destroyer {
	return &Destroyer{
		client: c,
		clock:  metav1.Now,
	}
}

// Destroy deletes the target workload and records the destruction timestamp.
// If PVC retention is configured, it sets an expiry time. PVCs are NOT deleted
// here — they persist until CleanupPVCs is called after retention expires.
func (d *Destroyer) Destroy(ctx context.Context, workload *v1alpha1.ManagedWorkload) (bool, error) {
	if workload.Status.Destroy != nil && workload.Status.Destroy.DestroyedAt != nil {
		return true, nil
	}

	// A missing target counts as destroyed: a previous attempt may have
	// deleted it and then failed to record that in status.
	target, err := getTarget(ctx, d.client, workload)
	switch {
	case apierrors.IsNotFound(err):
	case err != nil:
		return false, fmt.Errorf("getting target workload: %w", err)
	default:
		if err := d.client.Delete(ctx, target); client.IgnoreNotFound(err) != nil {
			return false, fmt.Errorf("deleting %s %s: %w", workload.Spec.Target.Kind, target.GetName(), err)
		}
	}

	// Update in place to keep the resource snapshot the controller recorded
	// before deleting.
	if workload.Status.Destroy == nil {
		workload.Status.Destroy = &v1alpha1.DestroyStatus{}
	}
	now := d.clock()
	workload.Status.Destroy.DestroyedAt = &now
	if workload.Spec.Destroy != nil && workload.Spec.Destroy.PVCRetention != nil {
		expiry := metav1.NewTime(now.Add(workload.Spec.Destroy.PVCRetention.Duration))
		workload.Status.Destroy.PVCRetentionExpiresAt = &expiry
	}
	return true, nil
}

// CleanupPVCs deletes PVCs associated with the workload after the retention
// period has expired. Returns true when cleanup is complete or not needed.
func (d *Destroyer) CleanupPVCs(ctx context.Context, workload *v1alpha1.ManagedWorkload) (bool, error) {
	if workload.Status.Destroy == nil {
		return true, nil
	}

	expiry := workload.Status.Destroy.PVCRetentionExpiresAt
	if expiry == nil {
		return true, nil
	}

	now := d.clock()
	if now.Time.Before(expiry.Time) {
		remaining := expiry.Time.Sub(now.Time).Round(time.Second)
		return false, fmt.Errorf("pvc retention expires in %s", remaining)
	}

	pvcs, err := d.findWorkloadPVCs(ctx, workload)
	if err != nil {
		return false, fmt.Errorf("finding pvcs: %w", err)
	}

	for i := range pvcs {
		if err := d.client.Delete(ctx, &pvcs[i]); err != nil {
			return false, fmt.Errorf("deleting pvc %s: %w", pvcs[i].Name, err)
		}
	}

	workload.Status.Destroy.PVCRetentionExpiresAt = nil
	return true, nil
}

func (d *Destroyer) findWorkloadPVCs(ctx context.Context, workload *v1alpha1.ManagedWorkload) ([]corev1.PersistentVolumeClaim, error) {
	var pvcs corev1.PersistentVolumeClaimList
	if err := d.client.List(ctx, &pvcs,
		client.InNamespace(workload.Namespace),
		client.MatchingLabelsSelector{Selector: labels.SelectorFromSet(labels.Set{
			"app.kubernetes.io/name": workload.Spec.Target.Name,
		})},
	); err != nil {
		return nil, fmt.Errorf("listing pvcs: %w", err)
	}
	return pvcs.Items, nil
}
