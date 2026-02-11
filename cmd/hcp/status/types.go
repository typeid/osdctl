package status

import "time"

// HCPStatus holds the parsed status of an HCP cluster from the live endpoint.
type HCPStatus struct {
	ClusterID               string
	ClusterName             string
	ClusterState            string
	ManagementCluster       string
	Version                 VersionInfo
	APIServerCertificate    *CertificateStatus
	IngressCertificate      *CertificateStatus
	ManifestWorks           []ManifestWorkSync
	HostedClusterConditions []Condition
	NodePools               []NodePoolStatus
}

// ManifestWorkSync represents the sync status of a single ManifestWork.
type ManifestWorkSync struct {
	Name         string
	Applied      bool
	Available    bool
	LastSyncTime time.Time
}

// VersionInfo holds cluster version details.
type VersionInfo struct {
	Current          string
	Desired          string
	Status           string
	Image            string
	AvailableUpdates []string
}

// CertificateStatus holds the certificate details.
type CertificateStatus struct {
	Ready       *bool // nil = unknown, true/false = known status
	NotAfter    time.Time
	RenewalTime time.Time
	DNSNames    []string
}

// Condition represents a single condition from a HostedCluster or NodePool.
type Condition struct {
	Type               string
	Status             string
	Reason             string
	Message            string
	LastTransitionTime string
}

// NodePoolStatus holds the status of a single NodePool.
type NodePoolStatus struct {
	Name       string
	Replicas   int
	Version    string
	Conditions []Condition
}

// mainMWResult holds the parsed output from the main ManifestWork.
type mainMWResult struct {
	Conditions  []Condition
	Version     VersionInfo
	MgmtCluster string
	Certificate *CertificateStatus
}

// FeedbackValue matches the ManifestWork statusFeedback structure.
type FeedbackValue struct {
	Name       string     `json:"name"`
	FieldValue FieldValue `json:"fieldValue"`
}

// FieldValue holds the typed value from statusFeedback.
type FieldValue struct {
	Type    string `json:"type"` // "String" or "Integer"
	String  string `json:"string"`
	Integer int    `json:"integer"`
}
