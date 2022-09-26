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
	k8spkg "github.com/openshift/osdctl/pkg/k8s"
	awsprovider "github.com/openshift/osdctl/pkg/provider/aws"
	"github.com/openshift/osdctl/pkg/utils"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// resizeNodeOptions defines the struct for running resizeNode command
type resizeNodeOptions struct {
	k8sclusterresourcefactory k8spkg.ClusterResourceFactoryOptions
	node                      string
	newMachineType            string
	verbose                   bool

	genericclioptions.IOStreams
	GlobalOptions *globalflags.GlobalOptions
}

// This command requires to be backplane logged into the target cluster
// During command execution, the OCM context is swapped back and forth: cluster - hive - cluster
func newCmdResizeNode(streams genericclioptions.IOStreams, flags *genericclioptions.ConfigFlags, globalOpts *globalflags.GlobalOptions) *cobra.Command {
	ops := newResizeNodeOptions(streams, flags, globalOpts)
	resizeNodeCmd := &cobra.Command{
		Use:               "resizeNode",
		Short:             "Resize a node. Requires previous login to the target cluster via ocm backplane login <target_cluster>",
		Args:              cobra.NoArgs,
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(ops.complete(cmd, args))
			cmdutil.CheckErr(ops.run())
		},
	}
	resizeNodeCmd.Flags().StringVar(&ops.node, "node", "", "The node to resize (e.g. ip-127.0.0.1.eu-west-2.compute.internal)")
	resizeNodeCmd.Flags().StringVar(&ops.newMachineType, "machine-type", "", "The target AWS machine type to resize to (e.g. m4.xlarge)")
	resizeNodeCmd.Flags().BoolVarP(&ops.verbose, "verbose", "", false, "Verbose output")
	ops.k8sclusterresourcefactory.AttachCobraCliFlags(resizeNodeCmd)
	resizeNodeCmd.MarkFlagRequired("cluster-id")
	resizeNodeCmd.MarkFlagRequired("node")
	resizeNodeCmd.MarkFlagRequired("machine-type")

	return resizeNodeCmd
}

func newResizeNodeOptions(streams genericclioptions.IOStreams, flags *genericclioptions.ConfigFlags, globalOpts *globalflags.GlobalOptions) *resizeNodeOptions {
	return &resizeNodeOptions{
		k8sclusterresourcefactory: k8spkg.ClusterResourceFactoryOptions{
			Flags: flags,
		},
		IOStreams:     streams,
		GlobalOptions: globalOpts,
	}
}

func (o *resizeNodeOptions) complete(cmd *cobra.Command, _ []string) error {
	err := CompleteValidation(&o.k8sclusterresourcefactory, o.IOStreams)
	if err != nil {
		return err
	}

	utils.IsValidClusterKey(o.k8sclusterresourcefactory.ClusterID)

	describedCluster, err := utils.DescribeCluster(o.k8sclusterresourcefactory.ClusterID)
	if err != nil {
		return err
	}

	if strings.ToUpper(describedCluster.CloudProvider().ID()) != "AWS" {
		return fmt.Errorf("This command is only available for AWS clusters")
	}
	/*
		Ideally we would want additional validation here for:
		- the machine type exists
		- the node exists on the cluster
		- this command isn't used on stage

		As this command is idempotent, it will just fail on a later stage if e.g. the
		machine type doesn't exist and can be re-run.
	*/

	return nil
}

func (o *resizeNodeOptions) initAwsCli() *awsprovider.Client {
	fmt.Println("Attempting to initialize AWS Client. Switching to hive context to get credentials.")

	targetCluster := o.k8sclusterresourcefactory.ClusterID
	hiveCluster, err := utils.GetHiveCluster(targetCluster)
	if err != nil {
		log.Fatalln("Unable to get hive cluster:", err)
	}

	utils.SwapOCMContext(hiveCluster)
	if err != nil {
		log.Fatalln("Unable to swap OC/kubectl config context:", err)
	}

	defer func() {
		err := utils.SwapOCMContext(targetCluster)
		if err != nil {
			log.Fatalln(err)
		}
	}()

	/*
		AWS Credentials to the specific cluster are fetched from the hive cluster,
		therfore we always have to be logged in on the corresponding hive shard initially
		to initialize the AWS Cli.
	*/
	awsClient, err := o.k8sclusterresourcefactory.GetCloudProvider(o.verbose)
	if err != nil {
		log.Fatalln("Unable to initialize AWS client")
	}

	fmt.Println("Successfully initalized AWS client on the hive context. Returning to cluster context.")
	return &awsClient
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
	fmt.Printf("Stopping ec2 instance %s. This might take a minute or two...", nodeID)

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
	fmt.Printf("Starting instance %s. This might take a minute or two...", nodeID)

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

func (o *resizeNodeOptions) run() error {
	fmt.Println("(!) This command actively switches the OC/kubectl config context. Please ensure you do not run any other cluster based commands while the AWS Client is being initialized.")

	// Does a quick context switch to the hive cluster and back
	awsClient := o.initAwsCli()

	machineName, nodeAwsID := getNodeAwsInstanceData(o.node, awsClient)

	// drain master node with oc adm drain <node> --ignore-daemonsets --delete-emptydir-data
	drainNode(o.node)

	// Stop the node instance
	stopNode(awsClient, nodeAwsID)

	// Once stopped, change the instance type
	modifyInstanceAttribute(awsClient, nodeAwsID, o.newMachineType)

	// Start the node instance
	startNode(awsClient, nodeAwsID)

	// uncordon node with oc adm uncordon <node>
	uncordonNode(o.node)

	fmt.Println("To continue, please confirm that the node is up and running and that the cluster is in the desired state to proceed.")
	confirmed := utils.ConfirmSend()
	if !confirmed {
		fmt.Println("Exiting...")
		return nil
	}

	fmt.Println("To finish the node resize, it is suggested to update the machine spec. This requires ***elevated privileges***. Do you want to proceed?")
	confirmed = utils.ConfirmSend()
	if !confirmed {
		fmt.Println("Exiting...")
		return nil
	}

	// Patch node machine to update .spec
	patchMachineType(machineName, o.newMachineType)

	fmt.Println("Node successfully resized.")

	return nil
}
