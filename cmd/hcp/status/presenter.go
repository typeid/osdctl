package status

import (
	"fmt"
	"math"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// printStatus renders the full HCP cluster status to stdout.
func printStatus(s *HCPStatus) {
	fmt.Printf("HCP Cluster Status: %s (%s)\n", s.ClusterName, s.ClusterID)
	if s.ClusterState != "" {
		fmt.Printf("Cluster State: %s\n", s.ClusterState)
	}
	if s.ManagementCluster != "" {
		fmt.Printf("Management Cluster: %s\n", s.ManagementCluster)
	}
	fmt.Println()

	if len(s.ManifestWorks) == 0 {
		fmt.Println("MANIFEST WORKS (Service Cluster -> Management Cluster)")
		fmt.Println("  No ManifestWork resources found")
		fmt.Println("  (Cluster may not be fully installed yet or may be in a transitional state)")
		fmt.Println()
	} else {
		printManifestWorkSync(s.ManifestWorks)
	}

	if len(s.HostedClusterConditions) == 0 {
		fmt.Println("HOSTED CLUSTER")
		fmt.Println("  No HostedCluster conditions available")
		fmt.Println("  (Cluster may not be fully installed yet or may be in a transitional state)")
		fmt.Println()
	} else {
		printHostedClusterStatus("HOSTED CLUSTER", s.HostedClusterConditions, s.Version)
	}

	// Show cluster API certificate status
	if s.APIServerCertificate != nil {
		fmt.Println("CLUSTER KUBE API CERTIFICATE")
		fmt.Println("  Certificate resource found in ManifestWork")
		fmt.Println("  (Detailed status not available - ACM feedback rules not yet implemented)")
		fmt.Println()
	}

	if s.IngressCertificate != nil {
		printCertificateStatus("DEFAULT INGRESS CERTIFICATE", s.IngressCertificate)
	} else {
		fmt.Println("DEFAULT INGRESS CERTIFICATE")
		fmt.Println("  No certificate information available")
		fmt.Println("  (Cluster may not be fully installed yet or may be in a transitional state)")
		fmt.Println()
	}

	if len(s.NodePools) == 0 {
		fmt.Println("NODEPOOLS")
		fmt.Println("  No NodePool resources found")
		fmt.Println("  (Cluster may not be fully installed yet or may be in a transitional state)")
		fmt.Println()
	} else {
		for _, np := range s.NodePools {
			printNodePoolStatus(np)
		}
	}
}

// printHostedClusterStatus renders the HostedCluster section with version and conditions.
func printHostedClusterStatus(title string, conditions []Condition, version VersionInfo) {
	fmt.Println(title)

	// Print version information first
	fmt.Println("  CONTROL PLANE VERSION")
	w := newTabWriter()
	if version.Current != "" || version.Desired != "" || version.Status != "" {
		if version.Current != "" {
			fmt.Fprintf(w, "    Current:\t%s", version.Current)
		} else {
			fmt.Fprintf(w, "    Current:\t(not available)")
		}
		if version.Desired != "" {
			fmt.Fprintf(w, "\tDesired: %s", version.Desired)
		}
		if version.Status != "" {
			fmt.Fprintf(w, "\tStatus: %s", version.Status)
		}
		fmt.Fprintln(w)

		if len(version.AvailableUpdates) > 0 {
			fmt.Fprintf(w, "    Available Updates:\t%s\n", strings.Join(version.AvailableUpdates, ", "))
		}

		// Add note for non-completed status
		if version.Status != "" && version.Status != "Completed" {
			fmt.Fprintf(w, "    Note:\tCheck ClusterVersion conditions below for details\n")
		}
	} else {
		fmt.Fprintf(w, "    Version:\t(not available)\n")
	}
	w.Flush()
	fmt.Println()

	// Print conditions
	if len(conditions) > 0 {
		fmt.Println("  CONDITIONS")
		w = newTabWriter()
		fmt.Fprintf(w, "    CONDITION\tSTATUS\tMESSAGE\n")
		for _, c := range conditions {
			msg := c.Message
			if msg == "" {
				msg = c.Reason
			}

			lines := strings.Split(msg, "\n")
			// Print first line in the table
			fmt.Fprintf(w, "    %s\t%s\t%s\n", c.Type, c.Status, lines[0])

			// Print continuation lines aligned with the MESSAGE column
			for i := 1; i < len(lines); i++ {
				line := strings.TrimSpace(lines[i])
				if line != "" {
					fmt.Fprintf(w, "    \t\t%s\n", line)
				}
			}
		}
		w.Flush()
	}
	fmt.Println()
}

// printManifestWorkSync renders a compact table of ManifestWork sync status.
func printManifestWorkSync(mws []ManifestWorkSync) {
	if len(mws) == 0 {
		return
	}

	fmt.Println("MANIFEST WORKS (Service Cluster -> Management Cluster)")
	w := newTabWriter()
	fmt.Fprintf(w, "  NAME\tAPPLIED\tAVAILABLE\tLAST SYNC\n")
	for _, mw := range mws {
		lastSync := "(unknown)"
		if !mw.LastSyncTime.IsZero() {
			// Show relative time (e.g., "2m ago")
			duration := time.Since(mw.LastSyncTime)
			if duration < time.Minute {
				lastSync = fmt.Sprintf("%ds ago", int(duration.Seconds()))
			} else if duration < time.Hour {
				lastSync = fmt.Sprintf("%dm ago", int(duration.Minutes()))
			} else if duration < 24*time.Hour {
				lastSync = fmt.Sprintf("%dh ago", int(duration.Hours()))
			} else {
				lastSync = fmt.Sprintf("%dd ago", int(duration.Hours()/24))
			}
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", mw.Name, boolStatus(mw.Applied), boolStatus(mw.Available), lastSync)
	}
	w.Flush()
	fmt.Println()
}

// printCertificateStatus renders the certificate block using tables.
func printCertificateStatus(title string, c *CertificateStatus) {
	fmt.Println(title)
	w := newTabWriter()

	status := "Unknown"
	if c.Ready != nil {
		if *c.Ready {
			status = "Ready"
		} else {
			status = "Not Ready"
		}
	}
	fmt.Fprintf(w, "  Status:\t%s\n", status)

	if !c.NotAfter.IsZero() {
		daysRemaining := int(math.Ceil(time.Until(c.NotAfter).Hours() / 24))
		fmt.Fprintf(w, "  Expires:\t%s (%dd remaining)\n", c.NotAfter.Format("2006-01-02"), daysRemaining)
	}

	if !c.RenewalTime.IsZero() {
		fmt.Fprintf(w, "  Renews:\t%s\n", c.RenewalTime.Format("2006-01-02"))
	}

	if len(c.DNSNames) > 0 {
		fmt.Fprintf(w, "  DNS Names:\t%s\n", c.DNSNames[0])
		for _, name := range c.DNSNames[1:] {
			fmt.Fprintf(w, "\t%s\n", name)
		}
	}

	w.Flush()
	fmt.Println()
}

// printNodePoolStatus renders a single NodePool section.
func printNodePoolStatus(np NodePoolStatus) {
	header := fmt.Sprintf("NODEPOOL: %s", np.Name)
	details := []string{}
	if np.Replicas > 0 {
		details = append(details, fmt.Sprintf("%d replicas", np.Replicas))
	}
	if np.Version != "" {
		details = append(details, fmt.Sprintf("v%s", np.Version))
	}
	if len(details) > 0 {
		header += " (" + strings.Join(details, ", ") + ")"
	}
	fmt.Println(header)

	w := newTabWriter()
	fmt.Fprintf(w, "  CONDITION\tSTATUS\tMESSAGE\n")
	for _, c := range np.Conditions {
		msg := c.Message
		if msg == "" {
			msg = c.Reason
		}

		lines := strings.Split(msg, "\n")
		// Print first line in the table
		fmt.Fprintf(w, "  %s\t%s\t%s\n", c.Type, c.Status, lines[0])

		// Print continuation lines aligned with the MESSAGE column
		for i := 1; i < len(lines); i++ {
			line := strings.TrimSpace(lines[i])
			if line != "" {
				fmt.Fprintf(w, "  \t\t%s\n", line)
			}
		}
	}
	w.Flush()
	fmt.Println()
}

// boolStatus returns "True" or "False" for display.
func boolStatus(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

// newTabWriter creates a tabwriter with intelligent defaults based on content type.
func newTabWriter() *tabwriter.Writer {
	// minwidth: 0 - let content determine minimum width
	// tabwidth: 4 - reasonable tab stops
	// padding: 2 - space between columns for readability
	// padchar: ' ' - spaces for padding
	// flags: 0 - default behavior
	return tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
}
