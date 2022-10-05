package cluster

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/openshift/osdctl/internal/utils/globalflags"
	"github.com/openshift/osdctl/pkg/clustercloud"
	awsprovider "github.com/openshift/osdctl/pkg/provider/aws"
	"github.com/openshift/osdctl/pkg/utils"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// resizeControlPlaneNodeOptions defines the struct for running resizeControlPlaneNode command
type resizeControlPlaneNodeOptions struct {
	clusterID      string
	node           string
	newMachineType string

	genericclioptions.IOStreams
	GlobalOptions *globalflags.GlobalOptions
}

// This command requires to previously be logged in via `ocm login`
func newCmdResizeControlPlaneNode(streams genericclioptions.IOStreams, flags *genericclioptions.ConfigFlags, globalOpts *globalflags.GlobalOptions) *cobra.Command {
	ops := newResizeControlPlaneNodeOptions(streams, flags, globalOpts)
	resizeControlPlaneNodeCmd := &cobra.Command{
		Use:               "resize-control-plane-node",
		Short:             "Resize a control plane node. Requires previous login to the api server via `ocm login` and being tunneled to the backplane.",
		Args:              cobra.NoArgs,
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(ops.complete(cmd, args))
			cmdutil.CheckErr(ops.run())
		},
	}
	resizeControlPlaneNodeCmd.Flags().StringVar(&ops.node, "node", "", "The control plane node to resize (e.g. ip-127.0.0.1.eu-west-2.compute.internal)")
	resizeControlPlaneNodeCmd.Flags().StringVar(&ops.newMachineType, "machine-type", "", "The target AWS machine type to resize to (e.g. m5.2xlarge)")
	resizeControlPlaneNodeCmd.Flags().StringVarP(&ops.clusterID, "cluster-id", "c", "", "The internal ID of the cluster to perform actions on")
	resizeControlPlaneNodeCmd.MarkFlagRequired("cluster-id")
	resizeControlPlaneNodeCmd.MarkFlagRequired("node")
	resizeControlPlaneNodeCmd.MarkFlagRequired("machine-type")

	return resizeControlPlaneNodeCmd
}

func newResizeControlPlaneNodeOptions(streams genericclioptions.IOStreams, flags *genericclioptions.ConfigFlags, globalOpts *globalflags.GlobalOptions) *resizeControlPlaneNodeOptions {
	return &resizeControlPlaneNodeOptions{
		IOStreams:     streams,
		GlobalOptions: globalOpts,
	}
}

func (o *resizeControlPlaneNodeOptions) complete(cmd *cobra.Command, _ []string) error {
	err := utils.IsValidClusterKey(o.clusterID)
	if err != nil {
		return err
	}

	connection := utils.CreateConnection()
	defer connection.Close()

	cluster, err := utils.GetCluster(connection, o.clusterID)
	if err != nil {
		return err
	}

	if strings.ToUpper(cluster.CloudProvider().ID()) != "AWS" {
		return fmt.Errorf("This command is only available for AWS clusters")
	}
	/*
		Ideally we would want additional validation here for:
		- the machine type exists
		- the node exists on the cluster

		As this command is idempotent, it will just fail on a later stage if e.g. the
		machine type doesn't exist and can be re-run.
	*/

	return nil
}

type drainDialogResponse int64

const (
	Undefined drainDialogResponse = 0
	Skip                          = 1
	Force                         = 2
	Cancel                        = 3
)

func drainRecoveryDialog() drainDialogResponse {
	fmt.Println("Do you want to skip drain, force drain or cancel this command? (skip/force/cancel):")

	reader := bufio.NewReader(os.Stdin)

	responseBytes, _, err := reader.ReadLine()
	if err != nil {
		log.Fatalln("reader.ReadLine() resulted in an error!")
	}

	response := strings.ToUpper(string(responseBytes))

	switch response {
	case "SKIP":
		return Skip
	case "FORCE":
		return Force
	case "CANCEL":
		return Cancel
	default:
		fmt.Println("Invalid response, expected 'skip', 'force' or 'cancel' (case-insensitive).")
		return drainRecoveryDialog()
	}
}

func drainNode(nodeID string) {
	fmt.Println("Draining node", nodeID)

	// TODO: replace subprocess call with API call
	cmd := fmt.Sprintf("oc adm drain %s --ignore-daemonsets --delete-emptydir-data", nodeID)
	output, err := exec.Command("bash", "-c", cmd).Output()

	if err != nil {
		fmt.Println("Failed to drain node:", strings.TrimSpace(string(output)))

		dialogResponse := drainRecoveryDialog()

		switch dialogResponse {
		case Skip:
			fmt.Println("Skipping node drain")
		case Force:
			// TODO: replace subprocess call with API call
			fmt.Println("Force draining node... This might take a minute or two...")
			cmd := fmt.Sprintf("oc adm drain %s --ignore-daemonsets --delete-emptydir-data --force", nodeID)
			err = exec.Command("bash", "-c", cmd).Run()
			if err != nil {
				log.Fatalln(err)
			}
		case Cancel:
			log.Fatalln("Exiting...")
		}
	}
}

func stopNode(awsClient *awsprovider.Client, nodeID string) {
	fmt.Printf("Stopping ec2 instance %s. This might take a minute or two...\n", nodeID)

	stopInstancesInput := &ec2.StopInstancesInput{InstanceIds: []*string{aws.String(nodeID)}}

	stopInstanceOutput, err := (*awsClient).StopInstances(stopInstancesInput)
	if err != nil {
		log.Fatalf("Unable to request stop of ec2 instance, output: %s. Error %s", stopInstanceOutput, err)
	}

	describeInstancesInput := &ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(nodeID)},
	}

	err = (*awsClient).WaitUntilInstanceStopped(describeInstancesInput)
	if err != nil {
		log.Fatalln("Unable to stop of ec2 instance:", err)
	}
}

func modifyInstanceAttribute(awsClient *awsprovider.Client, nodeID string, newMachineType string) {
	fmt.Println("Modifying machine type of instance:", nodeID, "to", newMachineType)

	modifyInstanceAttributeInput := &ec2.ModifyInstanceAttributeInput{InstanceId: &nodeID, InstanceType: &ec2.AttributeValue{Value: &newMachineType}}

	modifyInstanceOutput, err := (*awsClient).ModifyInstanceAttribute(modifyInstanceAttributeInput)
	if err != nil {
		log.Fatalf("Unable to modify ec2 instance, output: %s. Error: %s", modifyInstanceOutput, err)
	}
}

func startNode(awsClient *awsprovider.Client, nodeID string) {
	fmt.Printf("Starting instance %s. This might take a minute or two...\n", nodeID)

	startInstancesInput := &ec2.StartInstancesInput{InstanceIds: []*string{aws.String(nodeID)}}
	startInstanceOutput, err := (*awsClient).StartInstances(startInstancesInput)
	if err != nil {
		log.Fatalf("Unable to request start of ec2 instance, output: %s. Error %s", startInstanceOutput, err)
	}

	describeInstancesInput := &ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(nodeID)},
	}

	err = (*awsClient).WaitUntilInstanceRunning(describeInstancesInput)
	if err != nil {
		log.Fatalln("Unable to get ec2 instance up and running", err)
	}
}

func uncordonNode(nodeID string) {
	fmt.Println("Uncordoning node", nodeID)

	// TODO: replace subprocess call with API call
	cmd := fmt.Sprintf("oc adm uncordon %s", nodeID)
	_, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		log.Fatalln(err)
	}
}

// Start and stop calls require the internal AWS instance ID
// Machinetype patch requires the tag "Name"
func getNodeAwsInstanceData(node string, awsClient *awsprovider.Client) (string, string) {
	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("private-dns-name"),
				Values: []*string{aws.String(node)},
			},
		},
	}
	ret, err := (*awsClient).DescribeInstances(params)
	if err != nil {
		log.Fatalln(err)
	}

	awsInstanceID := *(ret.Reservations[0].Instances[0].InstanceId)

	var machineName string = ""
	tags := ret.Reservations[0].Instances[0].Tags
	for _, t := range tags {
		if *t.Key == "Name" {
			machineName = *t.Value
		}
	}

	if machineName == "" {
		log.Fatalln("Could not retrieve node machine name.")
	}

	fmt.Println("Node", node, "found as AWS internal InstanceId", awsInstanceID, "with machine name", machineName)

	return machineName, awsInstanceID
}

func patchMachineType(machine string, machineType string) {
	fmt.Println("Patching machine type of machine", machine, "to", machineType)
	cmd := `oc -n openshift-machine-api patch machine ` + machine + ` --patch "{\"spec\":{\"providerSpec\":{\"value\":{\"instanceType\":\"` + machineType + `\"}}}}" --type merge --as backplane-cluster-admin`
	err := exec.Command("bash", "-c", cmd).Run()
	if err != nil {
		log.Fatalln("Could not patch machine type:", err)
	}
}

func (o *resizeControlPlaneNodeOptions) run() error {
	awsClient, err := clustercloud.CreateAWSClient(o.clusterID)
	if err != nil {
		return err
	}

	machineName, nodeAwsID := getNodeAwsInstanceData(o.node, &awsClient)

	// drain node with oc adm drain <node> --ignore-daemonsets --delete-emptydir-data
	drainNode(o.node)

	// Stop the node instance
	stopNode(&awsClient, nodeAwsID)

	// Once stopped, change the instance type
	modifyInstanceAttribute(&awsClient, nodeAwsID, o.newMachineType)

	// Start the node instance
	startNode(&awsClient, nodeAwsID)

	// uncordon node with oc adm uncordon <node>
	uncordonNode(o.node)

	fmt.Println("To continue, please confirm that the node is up and running and that the cluster is in the desired state to proceed.")
	err = utils.ConfirmSend()
	if err != nil {
		return err
	}

	fmt.Println("To finish the node resize, it is suggested to update the machine spec. This requires ***elevated privileges***. Do you want to proceed?")
	err = utils.ConfirmSend()
	if err != nil {
		fmt.Println("Node resized, machine type not patched. Exiting...")
		return err
	}

	// Patch node machine to update .spec
	patchMachineType(machineName, o.newMachineType)

	fmt.Println("Control plane node successfully resized.")

	return nil
}
