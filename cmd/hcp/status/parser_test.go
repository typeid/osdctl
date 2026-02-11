package status

import (
	"testing"
	"time"
)

func TestParseFeedbackValues(t *testing.T) {
	values := []FeedbackValue{
		{Name: "Available-Status", FieldValue: FieldValue{Type: "String", String: "True"}},
		{Name: "Available-Reason", FieldValue: FieldValue{Type: "String", String: "AsExpected"}},
		{Name: "Available-Message", FieldValue: FieldValue{Type: "String", String: "The hosted control plane is available"}},
		{Name: "Progressing-Status", FieldValue: FieldValue{Type: "String", String: "False"}},
		{Name: "Progressing-Reason", FieldValue: FieldValue{Type: "String", String: "AsExpected"}},
		{Name: "Version-Current", FieldValue: FieldValue{Type: "String", String: "4.21.0"}},
		{Name: "Replicas", FieldValue: FieldValue{Type: "Integer", Integer: 2}},
	}

	conditions, extras := parseFeedbackValues(values)

	if len(conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(conditions))
	}

	if conditions[0].Type != "Available" {
		t.Errorf("expected first condition to be Available, got %s", conditions[0].Type)
	}
	if conditions[0].Status != "True" {
		t.Errorf("expected Available Status=True, got %s", conditions[0].Status)
	}
	if conditions[0].Reason != "AsExpected" {
		t.Errorf("expected Available Reason=AsExpected, got %s", conditions[0].Reason)
	}
	if conditions[0].Message != "The hosted control plane is available" {
		t.Errorf("unexpected Available Message: %s", conditions[0].Message)
	}

	if conditions[1].Type != "Progressing" {
		t.Errorf("expected second condition to be Progressing, got %s", conditions[1].Type)
	}
	if conditions[1].Status != "False" {
		t.Errorf("expected Progressing Status=False, got %s", conditions[1].Status)
	}

	if v, ok := extras["Version-Current"]; !ok || v != "4.21.0" {
		t.Errorf("expected extras Version-Current=4.21.0, got %v", extras)
	}
	if v, ok := extras["Replicas"]; !ok || v != "2" {
		t.Errorf("expected extras Replicas=2, got %v", extras)
	}
}

func TestParseHostedClusterFeedback(t *testing.T) {
	mainMW := `{
		"metadata": {
			"name": "test-cluster-id",
			"labels": {
				"api.openshift.com/management-cluster": "hs-mc-test123"
			}
		},
		"status": {
			"conditions": [
				{"type": "Applied", "status": "True"},
				{"type": "Available", "status": "True"}
			],
			"resourceStatus": {
				"manifests": [
					{
						"resourceMeta": {"kind": "HostedCluster", "name": "test-cluster"},
						"statusFeedback": {
							"values": [
								{"name": "Available-Status", "fieldValue": {"type": "String", "string": "True"}},
								{"name": "Available-Message", "fieldValue": {"type": "String", "string": "The hosted control plane is available"}},
								{"name": "Degraded-Status", "fieldValue": {"type": "String", "string": "False"}},
								{"name": "Degraded-Message", "fieldValue": {"type": "String", "string": "The hosted cluster is not degraded"}},
								{"name": "Version-Current", "fieldValue": {"type": "String", "string": "4.21.0"}},
								{"name": "Version-Desired", "fieldValue": {"type": "String", "string": "4.21.0"}},
								{"name": "Version-Status", "fieldValue": {"type": "String", "string": "Completed"}}
							]
						}
					}
				]
			}
		}
	}`

	result, err := parseMainManifestWork(mainMW)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.MgmtCluster != "hs-mc-test123" {
		t.Errorf("expected management cluster hs-mc-test123, got %s", result.MgmtCluster)
	}

	if result.Version.Current != "4.21.0" {
		t.Errorf("expected current version 4.21.0, got %s", result.Version.Current)
	}
	if result.Version.Desired != "4.21.0" {
		t.Errorf("expected desired version 4.21.0, got %s", result.Version.Desired)
	}
	if result.Version.Status != "Completed" {
		t.Errorf("expected version status Completed, got %s", result.Version.Status)
	}

	if len(result.Conditions) < 2 {
		t.Fatalf("expected at least 2 conditions, got %d", len(result.Conditions))
	}

	// Check that conditions are parsed correctly
	availableFound := false
	degradedFound := false
	for _, c := range result.Conditions {
		if c.Type == "Available" {
			availableFound = true
			if c.Status != "True" {
				t.Errorf("Expected Available=True, got %s", c.Status)
			}
		}
		if c.Type == "Degraded" {
			degradedFound = true
			if c.Status != "False" {
				t.Errorf("Expected Degraded=False, got %s", c.Status)
			}
		}
	}
	if !availableFound {
		t.Error("Available condition not found")
	}
	if !degradedFound {
		t.Error("Degraded condition not found")
	}

	// No certificate in this test
	if result.Certificate != nil {
		t.Error("expected no certificate in this test")
	}
}

func TestParseMainManifestWorkWithCertificate(t *testing.T) {
	mainMW := `{
		"metadata": {
			"name": "test-cluster-id",
			"labels": {
				"api.openshift.com/management-cluster": "hs-mc-test123"
			}
		},
		"status": {
			"conditions": [
				{"type": "Applied", "status": "True"},
				{"type": "Available", "status": "True"}
			],
			"resourceStatus": {
				"manifests": [
					{
						"resourceMeta": {"kind": "HostedCluster", "name": "test-cluster"},
						"statusFeedback": {
							"values": [
								{"name": "Available-Status", "fieldValue": {"type": "String", "string": "True"}},
								{"name": "Version-Current", "fieldValue": {"type": "String", "string": "4.21.0"}}
							]
						}
					},
					{
						"resourceMeta": {"kind": "Certificate", "name": "cluster-api-cert"},
						"statusFeedback": {
							"values": [
								{"name": "Ready-Status", "fieldValue": {"type": "String", "string": "True"}},
								{"name": "Ready-Reason", "fieldValue": {"type": "String", "string": "Ready"}},
								{"name": "Ready-Message", "fieldValue": {"type": "String", "string": "Certificate is up to date and has not expired"}},
								{"name": "Ready-LastTransitionTime", "fieldValue": {"type": "String", "string": "2026-05-07T12:00:00Z"}}
							]
						}
					}
				]
			}
		}
	}`

	result, err := parseMainManifestWork(mainMW)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.MgmtCluster != "hs-mc-test123" {
		t.Errorf("expected management cluster hs-mc-test123, got %s", result.MgmtCluster)
	}

	if result.Version.Current != "4.21.0" {
		t.Errorf("expected current version 4.21.0, got %s", result.Version.Current)
	}

	if len(result.Conditions) == 0 {
		t.Error("expected at least one condition")
	}

	// Check certificate parsing - should be present but without detailed status
	if result.Certificate == nil {
		t.Fatal("expected certificate to be detected")
	}

	// Certificate should be detected but without detailed status due to missing ACM feedback rules
	if result.Certificate.Ready != nil {
		t.Error("expected Ready to be nil (unknown) since feedback rules not implemented")
	}

	if !result.Certificate.NotAfter.IsZero() {
		t.Error("expected NotAfter to be zero for ManifestWork certificate without feedback rules")
	}

	if !result.Certificate.RenewalTime.IsZero() {
		t.Error("expected RenewalTime to be zero for ManifestWork certificate without feedback rules")
	}

	if len(result.Certificate.DNSNames) != 0 {
		t.Errorf("expected no DNS names for ManifestWork certificate without feedback rules, got %d", len(result.Certificate.DNSNames))
	}
}

func TestParseHostedClusterWithFalseConditions(t *testing.T) {
	mainMW := `{
		"metadata": {"name": "test-cluster-id", "labels": {}},
		"status": {
			"conditions": [],
			"resourceStatus": {
				"manifests": [
					{
						"resourceMeta": {"kind": "HostedCluster", "name": "test-cluster"},
						"statusFeedback": {
							"values": [
								{"name": "ClusterVersionSucceeding-Status", "fieldValue": {"type": "String", "string": "False"}},
								{"name": "ClusterVersionSucceeding-Message", "fieldValue": {"type": "String", "string": "Cluster operator monitoring is not available"}},
								{"name": "Available-Status", "fieldValue": {"type": "String", "string": "False"}},
								{"name": "Available-Message", "fieldValue": {"type": "String", "string": "Control plane is down"}}
							]
						}
					}
				]
			}
		}
	}`

	result, err := parseMainManifestWork(mainMW)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cvoFound := false
	availableFound := false
	for _, c := range result.Conditions {
		if c.Type == "ClusterVersionSucceeding" {
			cvoFound = true
			if c.Status != "False" {
				t.Errorf("Expected ClusterVersionSucceeding=False, got %s", c.Status)
			}
			if c.Message != "Cluster operator monitoring is not available" {
				t.Errorf("Unexpected message: %s", c.Message)
			}
		}
		if c.Type == "Available" {
			availableFound = true
			if c.Status != "False" {
				t.Errorf("Expected Available=False, got %s", c.Status)
			}
		}
	}
	if !cvoFound {
		t.Error("ClusterVersionSucceeding condition not found")
	}
	if !availableFound {
		t.Error("Available condition not found")
	}
}

func TestParseNodePoolFeedback(t *testing.T) {
	npMW := `{
		"metadata": {"name": "test-cluster-id-workers"},
		"status": {
			"conditions": [
				{"type": "Applied", "status": "True"},
				{"type": "Available", "status": "True"}
			],
			"resourceStatus": {
				"manifests": [
					{
						"resourceMeta": {"kind": "NodePool", "name": "test-workers"},
						"statusFeedback": {
							"values": [
								{"name": "Ready-Status", "fieldValue": {"type": "String", "string": "True"}},
								{"name": "AllMachinesReady-Status", "fieldValue": {"type": "String", "string": "True"}},
								{"name": "AllNodesHealthy-Status", "fieldValue": {"type": "String", "string": "True"}},
								{"name": "UpdatingVersion-Status", "fieldValue": {"type": "String", "string": "False"}},
								{"name": "Replicas", "fieldValue": {"type": "Integer", "integer": 3}},
								{"name": "Version", "fieldValue": {"type": "String", "string": "4.21.0"}}
							]
						}
					}
				]
			}
		}
	}`

	nodePools, err := parseManifestWorkNodePools(npMW)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(nodePools) != 1 {
		t.Fatalf("expected 1 nodepool, got %d", len(nodePools))
	}

	np := nodePools[0]
	if np.Name != "test-workers" {
		t.Errorf("expected nodepool name test-workers, got %s", np.Name)
	}
	if np.Replicas != 3 {
		t.Errorf("expected 3 replicas, got %d", np.Replicas)
	}
	if np.Version != "4.21.0" {
		t.Errorf("expected version 4.21.0, got %s", np.Version)
	}

	// Check specific conditions are parsed correctly
	readyFound := false
	updatingFound := false
	for _, c := range np.Conditions {
		if c.Type == "Ready" {
			readyFound = true
			if c.Status != "True" {
				t.Errorf("Expected Ready=True, got %s", c.Status)
			}
		}
		if c.Type == "UpdatingVersion" {
			updatingFound = true
			if c.Status != "False" {
				t.Errorf("Expected UpdatingVersion=False, got %s", c.Status)
			}
		}
	}
	if !readyFound {
		t.Error("Ready condition not found")
	}
	if !updatingFound {
		t.Error("UpdatingVersion condition not found")
	}
}

func TestParseNodePoolUpdating(t *testing.T) {
	npMW := `{
		"metadata": {"name": "test-cluster-id-workers"},
		"status": {
			"conditions": [],
			"resourceStatus": {
				"manifests": [
					{
						"resourceMeta": {"kind": "NodePool", "name": "test-workers"},
						"statusFeedback": {
							"values": [
								{"name": "Ready-Status", "fieldValue": {"type": "String", "string": "False"}},
								{"name": "Ready-Message", "fieldValue": {"type": "String", "string": "Not all replicas are ready"}},
								{"name": "UpdatingVersion-Status", "fieldValue": {"type": "String", "string": "True"}},
								{"name": "UpdatingVersion-Message", "fieldValue": {"type": "String", "string": "Updating to 4.21.1"}},
								{"name": "Replicas", "fieldValue": {"type": "Integer", "integer": 2}},
								{"name": "Version", "fieldValue": {"type": "String", "string": "4.21.0"}}
							]
						}
					}
				]
			}
		}
	}`

	nodePools, err := parseManifestWorkNodePools(npMW)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(nodePools) != 1 {
		t.Fatalf("expected 1 nodepool, got %d", len(nodePools))
	}

	np := nodePools[0]
	readyFound := false
	updatingFound := false
	for _, c := range np.Conditions {
		if c.Type == "Ready" {
			readyFound = true
			if c.Status != "False" {
				t.Errorf("Expected Ready=False, got %s", c.Status)
			}
			if c.Message != "Not all replicas are ready" {
				t.Errorf("Unexpected message: %s", c.Message)
			}
		}
		if c.Type == "UpdatingVersion" {
			updatingFound = true
			if c.Status != "True" {
				t.Errorf("Expected UpdatingVersion=True, got %s", c.Status)
			}
			if c.Message != "Updating to 4.21.1" {
				t.Errorf("Unexpected message: %s", c.Message)
			}
		}
	}
	if !readyFound {
		t.Error("Ready condition not found")
	}
	if !updatingFound {
		t.Error("UpdatingVersion condition not found")
	}
}

func TestParseCertificate(t *testing.T) {
	certJSON := `{
		"spec": {
			"dnsNames": [
				"*.apps.rosa.test-cluster.ake1.s3.devshift.org",
				"*.test-id.rosa.test-cluster.ake1.s3.devshift.org"
			]
		},
		"status": {
			"conditions": [
				{"type": "Ready", "status": "True"}
			],
			"notAfter": "2026-05-07T12:00:00Z",
			"renewalTime": "2026-04-07T12:00:00Z"
		}
	}`

	cert, err := parseCertificate(certJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cert.Ready == nil || !*cert.Ready {
		t.Error("expected cert to be ready")
	}

	expectedNotAfter := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	if !cert.NotAfter.Equal(expectedNotAfter) {
		t.Errorf("expected notAfter %v, got %v", expectedNotAfter, cert.NotAfter)
	}

	expectedRenewal := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	if !cert.RenewalTime.Equal(expectedRenewal) {
		t.Errorf("expected renewalTime %v, got %v", expectedRenewal, cert.RenewalTime)
	}

	if len(cert.DNSNames) != 2 {
		t.Fatalf("expected 2 DNS names, got %d", len(cert.DNSNames))
	}
}

func TestParseCertificateNotReady(t *testing.T) {
	certJSON := `{
		"spec": {"dnsNames": ["*.example.com"]},
		"status": {
			"conditions": [
				{"type": "Ready", "status": "False"}
			]
		}
	}`

	cert, err := parseCertificate(certJSON)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cert.Ready != nil && *cert.Ready {
		t.Error("expected cert to not be ready")
	}
}

func TestParseCertificateMalformed(t *testing.T) {
	_, err := parseCertificate("{not valid json")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestOrderConditions(t *testing.T) {
	// Verify that parseFeedbackValues preserves insertion order of conditions
	values := []FeedbackValue{
		{Name: "ZetaCondition-Status", FieldValue: FieldValue{Type: "String", String: "True"}},
		{Name: "Available-Status", FieldValue: FieldValue{Type: "String", String: "False"}},
		{Name: "Progressing-Status", FieldValue: FieldValue{Type: "String", String: "Unknown"}},
		{Name: "AlphaCondition-Status", FieldValue: FieldValue{Type: "String", String: "False"}},
		{Name: "BetaCondition-Status", FieldValue: FieldValue{Type: "String", String: "True"}},
	}

	conditions, _ := parseFeedbackValues(values)

	if len(conditions) != 5 {
		t.Fatalf("expected 5 conditions, got %d", len(conditions))
	}

	// Should be in original order from parseFeedbackValues (order added to conditionMap)
	expected := []struct {
		name   string
		status string
	}{
		{"ZetaCondition", "True"},
		{"Available", "False"},
		{"Progressing", "Unknown"},
		{"AlphaCondition", "False"},
		{"BetaCondition", "True"},
	}

	for i, exp := range expected {
		if conditions[i].Type != exp.name {
			t.Errorf("position %d: expected %s, got %s", i, exp.name, conditions[i].Type)
		}
		if conditions[i].Status != exp.status {
			t.Errorf("position %d: expected status %s, got %s", i, exp.status, conditions[i].Status)
		}
	}
}

func TestParseManifestWorkSyncStatus(t *testing.T) {
	jsonStr := `{
		"metadata": {"name": "test-cluster-id"},
		"status": {
			"conditions": [
				{"type": "Applied", "status": "True", "lastTransitionTime": "2026-02-07T12:00:00Z"},
				{"type": "Available", "status": "True", "lastTransitionTime": "2026-02-07T12:05:00Z"}
			]
		}
	}`

	mws, err := parseManifestWorkSyncStatus(jsonStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mws.Name != "test-cluster-id" {
		t.Errorf("expected name test-cluster-id, got %s", mws.Name)
	}
	if !mws.Applied {
		t.Error("expected Applied=true")
	}
	if !mws.Available {
		t.Error("expected Available=true")
	}

	// Should use the most recent timestamp (Available at 12:05)
	expectedTime := time.Date(2026, 2, 7, 12, 5, 0, 0, time.UTC)
	if !mws.LastSyncTime.Equal(expectedTime) {
		t.Errorf("expected LastSyncTime %v, got %v", expectedTime, mws.LastSyncTime)
	}
}

func TestParseManifestWorkSyncStatusNotApplied(t *testing.T) {
	jsonStr := `{
		"metadata": {"name": "test-cluster-id"},
		"status": {
			"conditions": [
				{"type": "Applied", "status": "False", "lastTransitionTime": "2026-02-07T11:00:00Z"},
				{"type": "Available", "status": "False", "lastTransitionTime": "2026-02-07T11:30:00Z"}
			]
		}
	}`

	mws, err := parseManifestWorkSyncStatus(jsonStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mws.Applied {
		t.Error("expected Applied=false")
	}
	if mws.Available {
		t.Error("expected Available=false")
	}

	// Should still capture the most recent timestamp even for false conditions
	expectedTime := time.Date(2026, 2, 7, 11, 30, 0, 0, time.UTC)
	if !mws.LastSyncTime.Equal(expectedTime) {
		t.Errorf("expected LastSyncTime %v, got %v", expectedTime, mws.LastSyncTime)
	}
}

func TestParseLiveResources(t *testing.T) {
	resources := map[string]string{
		"manifest_work-testid": `{
			"metadata": {
				"name": "testid",
				"labels": {"api.openshift.com/management-cluster": "hs-mc-abc"}
			},
			"status": {
				"conditions": [
					{"type": "Applied", "status": "True", "lastTransitionTime": "2026-02-07T10:00:00Z"},
					{"type": "Available", "status": "True", "lastTransitionTime": "2026-02-07T10:05:00Z"}
				],
				"resourceStatus": {
					"manifests": [
						{
							"resourceMeta": {"kind": "HostedCluster", "name": "my-cluster"},
							"statusFeedback": {
								"values": [
									{"name": "Available-Status", "fieldValue": {"type": "String", "string": "True"}},
									{"name": "Version-Current", "fieldValue": {"type": "String", "string": "4.21.0"}},
									{"name": "Version-Desired", "fieldValue": {"type": "String", "string": "4.21.0"}},
									{"name": "Version-Status", "fieldValue": {"type": "String", "string": "Completed"}}
								]
							}
						}
					]
				}
			}
		}`,
		"manifest_work-testid-workers": `{
			"metadata": {"name": "testid-workers"},
			"status": {
				"conditions": [
					{"type": "Applied", "status": "True", "lastTransitionTime": "2026-02-07T10:10:00Z"},
					{"type": "Available", "status": "True", "lastTransitionTime": "2026-02-07T10:15:00Z"}
				],
				"resourceStatus": {
					"manifests": [
						{
							"resourceMeta": {"kind": "NodePool", "name": "workers"},
							"statusFeedback": {
								"values": [
									{"name": "Ready-Status", "fieldValue": {"type": "String", "string": "True"}},
									{"name": "Replicas", "fieldValue": {"type": "Integer", "integer": 2}},
									{"name": "Version", "fieldValue": {"type": "String", "string": "4.21.0"}}
								]
							}
						}
					]
				}
			}
		}`,
		"certificate-testid": `{
			"spec": {"dnsNames": ["*.apps.example.com"]},
			"status": {
				"conditions": [{"type": "Ready", "status": "True"}],
				"notAfter": "2026-05-07T12:00:00Z",
				"renewalTime": "2026-04-07T12:00:00Z"
			}
		}`,
	}

	status, err := parseLiveResources(resources, "testid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.ManagementCluster != "hs-mc-abc" {
		t.Errorf("expected management cluster hs-mc-abc, got %s", status.ManagementCluster)
	}

	if len(status.ManifestWorks) != 2 {
		t.Errorf("expected 2 manifest works, got %d", len(status.ManifestWorks))
	}

	if status.Version.Current != "4.21.0" {
		t.Errorf("expected version 4.21.0, got %s", status.Version.Current)
	}

	if len(status.NodePools) != 1 {
		t.Errorf("expected 1 nodepool, got %d", len(status.NodePools))
	}

	if status.IngressCertificate == nil {
		t.Fatal("expected ingress certificate to be parsed")
	}
	if status.IngressCertificate.Ready == nil || !*status.IngressCertificate.Ready {
		t.Error("expected ingress certificate to be ready")
	}
}
