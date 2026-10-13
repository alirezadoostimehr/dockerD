package runtime

import "testing"

func TestInfrastructureIdentityAndWorkloads(t *testing.T) {
	c := []Container{{ID: "system", State: "running", Labels: map[string]string{"io.kubernetes.pod.uid": "trusted"}}, {ID: "user", State: "paused"}, {ID: "stopped", State: "exited"}, {ID: "spoof", State: "running", Labels: map[string]string{"io.kubernetes.pod.namespace": "kube-system"}}}
	p := []Pod{{Metadata: Metadata{Name: "pending", UID: "user"}}, {Metadata: Metadata{Name: "system", UID: "trusted"}}}
	p[0].Status.Phase = "Pending"
	p[1].Status.Phase = "Running"
	b := WorkloadBlockers(c, p, map[string]bool{"trusted": true})
	if len(b) != 3 {
		t.Fatalf("expected paused, spoof and pending blockers: %+v", b)
	}
}
