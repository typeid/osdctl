package status

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// parseLiveResources processes the resources map from the OCM live endpoint
// and returns an HCPStatus struct.
func parseLiveResources(resources map[string]string, clusterID string) (*HCPStatus, error) {
	status := &HCPStatus{}

	var mainMWKey string
	manifestWorkKeys := []string{}

	// First pass: identify all manifest_work keys
	for key := range resources {
		if strings.HasPrefix(key, "manifest_work-") {
			manifestWorkKeys = append(manifestWorkKeys, key)
		}
	}
	sort.Strings(manifestWorkKeys)

	// The main ManifestWork is always named manifest_work-<cluster_internal_id>
	mainMWKey = "manifest_work-" + clusterID

	// Parse ManifestWork sync status for all ManifestWorks
	for _, key := range manifestWorkKeys {
		jsonStr := resources[key]
		mws, err := parseManifestWorkSyncStatus(jsonStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ManifestWork sync status for %s: %w", key, err)
		}
		status.ManifestWorks = append(status.ManifestWorks, mws)
	}

	// Parse main ManifestWork for HostedCluster conditions and version
	if mainMWJSON, exists := resources[mainMWKey]; exists {
		result, err := parseMainManifestWork(mainMWJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to parse main ManifestWork: %w", err)
		}
		status.HostedClusterConditions = result.Conditions
		status.Version = result.Version
		status.ManagementCluster = result.MgmtCluster
		if result.Certificate != nil {
			status.APIServerCertificate = result.Certificate
		}
	}

	// Parse all ManifestWorks for NodePool resources
	for _, key := range manifestWorkKeys {
		nodePools, err := parseManifestWorkNodePools(resources[key])
		if err != nil {
			return nil, fmt.Errorf("failed to parse NodePool from %s: %w", key, err)
		}
		status.NodePools = append(status.NodePools, nodePools...)
	}

	// Parse ingress certificate (standalone resource)
	for key, jsonStr := range resources {
		if strings.HasPrefix(key, "certificate-") {
			cert, err := parseCertificate(jsonStr)
			if err != nil {
				return nil, fmt.Errorf("failed to parse ingress certificate: %w", err)
			}
			status.IngressCertificate = cert
			break
		}
	}

	return status, nil
}

// parseManifestWorkSyncStatus extracts Applied and Available conditions from
// a ManifestWork's top-level status.
func parseManifestWorkSyncStatus(jsonStr string) (ManifestWorkSync, error) {
	var mw struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			Conditions []struct {
				Type               string `json:"type"`
				Status             string `json:"status"`
				LastTransitionTime string `json:"lastTransitionTime"`
			} `json:"conditions"`
		} `json:"status"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &mw); err != nil {
		return ManifestWorkSync{}, fmt.Errorf("invalid JSON: %w", err)
	}

	result := ManifestWorkSync{Name: mw.Metadata.Name}
	var mostRecentTime time.Time

	for _, c := range mw.Status.Conditions {
		switch c.Type {
		case "Applied":
			result.Applied = c.Status == "True"
		case "Available":
			result.Available = c.Status == "True"
		}

		// Track the most recent transition time from any condition
		if c.LastTransitionTime != "" {
			if t, err := time.Parse(time.RFC3339, c.LastTransitionTime); err == nil {
				if t.After(mostRecentTime) {
					mostRecentTime = t
				}
			}
		}
	}

	result.LastSyncTime = mostRecentTime
	return result, nil
}

// parseMainManifestWork parses the main ManifestWork to extract HostedCluster
// conditions, version info, and the management cluster name.
func parseMainManifestWork(jsonStr string) (*mainMWResult, error) {
	var mw struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		Status struct {
			ResourceStatus struct {
				Manifests []struct {
					ResourceMeta struct {
						Group    string `json:"group"`
						Kind     string `json:"kind"`
						Name     string `json:"name"`
						Resource string `json:"resource"`
					} `json:"resourceMeta"`
					StatusFeedback struct {
						Values []FeedbackValue `json:"values"`
					} `json:"statusFeedback"`
				} `json:"manifests"`
			} `json:"resourceStatus"`
		} `json:"status"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &mw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	mgmtCluster := mw.Metadata.Labels["api.openshift.com/management-cluster"]

	var conditions []Condition
	var version VersionInfo
	var cert *CertificateStatus

	for _, manifest := range mw.Status.ResourceStatus.Manifests {
		switch manifest.ResourceMeta.Kind {
		case "HostedCluster":
			conds, extras := parseFeedbackValues(manifest.StatusFeedback.Values)
			conditions = conds

			if v, ok := extras["Version-Current"]; ok {
				version.Current = v
			}
			if v, ok := extras["Version-Desired"]; ok {
				version.Desired = v
			}
			if v, ok := extras["Version-Status"]; ok {
				version.Status = v
			}
			if v, ok := extras["Version-Image"]; ok {
				version.Image = v
			}
			if v, ok := extras["Version-AvailableUpdates"]; ok && v != "" {
				version.AvailableUpdates = strings.Split(v, ",")
			}
		case "Certificate":
			// Certificate resources are present in ManifestWork but statusFeedback
			// doesn't provide complete details due to missing ACM feedback rules.
			// This will be implemented in the future.
			cert = &CertificateStatus{}
			// Mark as present but without detailed status
		}
	}

	return &mainMWResult{
		Conditions:  conditions,
		Version:     version,
		MgmtCluster: mgmtCluster,
		Certificate: cert,
	}, nil
}

// parseManifestWorkNodePools parses a ManifestWork looking for NodePool resources
// in the statusFeedback. Returns zero or more NodePoolStatus entries.
func parseManifestWorkNodePools(jsonStr string) ([]NodePoolStatus, error) {
	var mw struct {
		Status struct {
			ResourceStatus struct {
				Manifests []struct {
					ResourceMeta struct {
						Group    string `json:"group"`
						Kind     string `json:"kind"`
						Name     string `json:"name"`
						Resource string `json:"resource"`
					} `json:"resourceMeta"`
					StatusFeedback struct {
						Values []FeedbackValue `json:"values"`
					} `json:"statusFeedback"`
				} `json:"manifests"`
			} `json:"resourceStatus"`
		} `json:"status"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &mw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	var nodePools []NodePoolStatus
	for _, manifest := range mw.Status.ResourceStatus.Manifests {
		if manifest.ResourceMeta.Kind != "NodePool" {
			continue
		}

		conds, extras := parseFeedbackValues(manifest.StatusFeedback.Values)
		np := NodePoolStatus{
			Name:       manifest.ResourceMeta.Name,
			Conditions: conds,
		}

		if v, ok := extras["Replicas"]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				np.Replicas = n
			}
		}
		if v, ok := extras["Version"]; ok {
			np.Version = v
		}

		nodePools = append(nodePools, np)
	}

	return nodePools, nil
}

// parseFeedbackValues groups flat key-value feedback pairs into Condition structs
// and separates out non-condition values. Feedback keys follow the pattern
// "ConditionType-Field" (e.g., "Available-Status", "Available-Message").
// Non-condition keys (like "Version-Current", "Replicas") are returned in the
// extras map.
func parseFeedbackValues(values []FeedbackValue) ([]Condition, map[string]string) {
	conditionMap := make(map[string]*Condition)
	extras := make(map[string]string)
	var conditionOrder []string

	// Known condition fields
	conditionFields := map[string]bool{
		"Status": true, "Reason": true, "Message": true, "LastTransitionTime": true,
	}

	// Prefixes that are not conditions even if they match the Type-Field pattern
	nonConditionPrefixes := map[string]bool{
		"Version": true,
	}

	for _, fv := range values {
		val := fv.FieldValue.String
		if fv.FieldValue.Type == "Integer" {
			val = fmt.Sprintf("%d", fv.FieldValue.Integer)
		}

		// Try to split into Type-Field. Note: condition types containing
		// hyphens would be split incorrectly; current OCM data does not
		// include such types.
		parts := strings.SplitN(fv.Name, "-", 2)
		if len(parts) == 2 && conditionFields[parts[1]] && !nonConditionPrefixes[parts[0]] {
			condType := parts[0]
			field := parts[1]

			if _, exists := conditionMap[condType]; !exists {
				conditionMap[condType] = &Condition{Type: condType}
				conditionOrder = append(conditionOrder, condType)
			}

			c := conditionMap[condType]
			switch field {
			case "Status":
				c.Status = val
			case "Reason":
				c.Reason = val
			case "Message":
				c.Message = val
			case "LastTransitionTime":
				c.LastTransitionTime = val
			}
		} else {
			extras[fv.Name] = val
		}
	}

	conditions := make([]Condition, 0, len(conditionOrder))
	for _, t := range conditionOrder {
		conditions = append(conditions, *conditionMap[t])
	}

	return conditions, extras
}

// parseCertificate extracts status from a cert-manager Certificate resource.
func parseCertificate(jsonStr string) (*CertificateStatus, error) {
	var cert struct {
		Spec struct {
			DNSNames []string `json:"dnsNames"`
		} `json:"spec"`
		Status struct {
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"conditions"`
			NotAfter    string `json:"notAfter"`
			RenewalTime string `json:"renewalTime"`
		} `json:"status"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &cert); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	cs := &CertificateStatus{
		DNSNames: cert.Spec.DNSNames,
	}

	for _, c := range cert.Status.Conditions {
		if c.Type == "Ready" {
			ready := c.Status == "True"
			cs.Ready = &ready
		}
	}

	if cert.Status.NotAfter != "" {
		t, err := time.Parse(time.RFC3339, cert.Status.NotAfter)
		if err == nil {
			cs.NotAfter = t
		}
	}

	if cert.Status.RenewalTime != "" {
		t, err := time.Parse(time.RFC3339, cert.Status.RenewalTime)
		if err == nil {
			cs.RenewalTime = t
		}
	}

	return cs, nil
}
