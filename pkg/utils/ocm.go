package utils

import (
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/openshift-online/ocm-cli/pkg/ocm"
	sdk "github.com/openshift-online/ocm-sdk-go"
	v1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"
)

func GetClusters(ocmClient *sdk.Connection, clusterIds []string) []*v1.Cluster {
	for i, id := range clusterIds {
		clusterIds[i] = GenerateQuery(id)
	}

	clusters, err := ApplyFilters(ocmClient, []string{strings.Join(clusterIds, " or ")})
	if err != nil {
		log.Fatalf("error while retrieving cluster(s) from ocm: %[1]s", err)
	}

	return clusters
}

// ApplyFilters retrieves clusters in OCM which match the filters given
func ApplyFilters(ocmClient *sdk.Connection, filters []string) ([]*v1.Cluster, error) {
	if len(filters) < 1 {
		return nil, nil
	}

	for k, v := range filters {
		filters[k] = fmt.Sprintf("(%s)", v)
	}

	requestSize := 50
	full_filters := strings.Join(filters, " and ")

	request := ocmClient.ClustersMgmt().V1().Clusters().List().Search(full_filters).Size(requestSize)
	response, err := request.Send()
	if err != nil {
		return nil, err
	}

	items := response.Items().Slice()
	for response.Size() >= requestSize {
		request.Page(response.Page() + 1)
		response, err = request.Send()
		if err != nil {
			return nil, err
		}
		items = append(items, response.Items().Slice()...)
	}

	return items, err
}

// GenerateQuery returns an OCM search query to retrieve all clusters matching an expression (ie- "foo%")
func GenerateQuery(clusterIdentifier string) string {
	return strings.TrimSpace(fmt.Sprintf("(id like '%[1]s' or external_id like '%[1]s' or display_name like '%[1]s')", clusterIdentifier))
}

func CreateConnection() *sdk.Connection {
	connection, err := ocm.NewConnection().Build()
	if err != nil {
		if strings.Contains(err.Error(), "Not logged in, run the") {
			log.Fatalf("Failed to create OCM connection: Authentication error, run the 'ocm login' command first.")
		}
		log.Fatalf("Failed to create OCM connection: %v", err)
	}
	return connection
}

// Performs a backplane login into a given cluster
func SwapOCMContext(clusterID string) error {
	// TODO: replace subprocess call with API call
	cmd := fmt.Sprintf("ocm backplane login %s", clusterID)
	err := exec.Command("bash", "-c", cmd).Run()
	if err != nil {
		return err
	}
	return nil

}

//This command implements the ocm describe cluster call via osm-sdk.
//This call requires the ocm API Token https://cloud.redhat.com/openshift/token be available in the OCM_TOKEN env variable.
//Example: export OCM_TOKEN=$(jq -r .refresh_token ~/.ocm.json)
func DescribeCluster(clusterID string) (*v1.Cluster, error) {

	connection := CreateConnection()
	defer connection.Close()

	// Get the client for the resource that manages the collection of clusters:
	collection := connection.ClustersMgmt().V1().Clusters()
	resource := collection.Cluster(clusterID)
	// Send the request to retrieve the cluster:
	response, err := resource.Get().Send()
	if err != nil {
		return nil, fmt.Errorf("Can't retrieve cluster: %v", err)
	}

	cluster := response.Body()

	return cluster, err
}

// Returns the hive shard corresponding to a cluster
// e.g. https://api.<hive_cluster>.byo5.p1.openshiftapps.com:6443
func GetHiveShard(cluster string) (string, error) {
	connection := CreateConnection()
	defer connection.Close()

	shardPath, err := connection.ClustersMgmt().V1().Clusters().
		Cluster(cluster).
		ProvisionShard().
		Get().
		Send()

	var shard string

	if shardPath != nil && err == nil {
		shard = shardPath.Body().HiveConfig().Server()
	}

	if shard == "" {
		return "", fmt.Errorf("Unable to retrieve shard for cluster %s", cluster)
	}

	return shard, nil
}

// Returns the corresponding hive cluster identifier of a cluster (e.g. hivep04ew2)
func GetHiveCluster(cluster string) (string, error) {
	shard, err := GetHiveShard(cluster)
	if err != nil {
		return "", err
	}
	// We know that the shard is not empty, as we're past the error
	// https://api.<hive_cluster>.byo5.p1.openshiftapps.com:6443
	splitted := strings.Split(shard, ".")

	if len(splitted) != 6 {
		return "", fmt.Errorf("Unable to parse the hive shard %s. Should be in the format https://api.<hive_cluster>.byo5.p1.openshiftapps.com:6443", shard)
	}

	return splitted[1], nil
}
