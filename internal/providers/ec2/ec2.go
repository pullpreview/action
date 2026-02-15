package ec2

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2svc "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"golang.org/x/crypto/ssh"

	"github.com/pullpreview/action/internal/providers/sshca"
	"github.com/pullpreview/action/internal/pullpreview"
)

const (
	defaultEC2SSHRetries  = 12
	defaultEC2SSHInterval = 10 * time.Second
	defaultEC2SSHCertTTL  = 12 * time.Hour
)

var ec2SecurityGroupNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9-]+`)

type ec2Client interface {
	DescribeInstances(context.Context, *ec2svc.DescribeInstancesInput) (*ec2svc.DescribeInstancesOutput, error)
	RunInstances(context.Context, *ec2svc.RunInstancesInput) (*ec2svc.RunInstancesOutput, error)
	TerminateInstances(context.Context, *ec2svc.TerminateInstancesInput) (*ec2svc.TerminateInstancesOutput, error)
	StartInstances(context.Context, *ec2svc.StartInstancesInput) (*ec2svc.StartInstancesOutput, error)
	ModifyInstanceAttribute(context.Context, *ec2svc.ModifyInstanceAttributeInput) (*ec2svc.ModifyInstanceAttributeOutput, error)

	DescribeSubnets(context.Context, *ec2svc.DescribeSubnetsInput) (*ec2svc.DescribeSubnetsOutput, error)
	DescribeInstanceTypes(context.Context, *ec2svc.DescribeInstanceTypesInput) (*ec2svc.DescribeInstanceTypesOutput, error)
	DescribeImages(context.Context, *ec2svc.DescribeImagesInput) (*ec2svc.DescribeImagesOutput, error)

	DescribeSecurityGroups(context.Context, *ec2svc.DescribeSecurityGroupsInput) (*ec2svc.DescribeSecurityGroupsOutput, error)
	CreateSecurityGroup(context.Context, *ec2svc.CreateSecurityGroupInput) (*ec2svc.CreateSecurityGroupOutput, error)
	DeleteSecurityGroup(context.Context, *ec2svc.DeleteSecurityGroupInput) (*ec2svc.DeleteSecurityGroupOutput, error)
	AuthorizeSecurityGroupIngress(context.Context, *ec2svc.AuthorizeSecurityGroupIngressInput) (*ec2svc.AuthorizeSecurityGroupIngressOutput, error)
	RevokeSecurityGroupIngress(context.Context, *ec2svc.RevokeSecurityGroupIngressInput) (*ec2svc.RevokeSecurityGroupIngressOutput, error)

	CreateTags(context.Context, *ec2svc.CreateTagsInput) (*ec2svc.CreateTagsOutput, error)
	CreateKeyPair(context.Context, *ec2svc.CreateKeyPairInput) (*ec2svc.CreateKeyPairOutput, error)
	DeleteKeyPair(context.Context, *ec2svc.DeleteKeyPairInput) (*ec2svc.DeleteKeyPairOutput, error)
}

type ec2ClientAdapter struct {
	client *ec2svc.Client
}

func (a ec2ClientAdapter) DescribeInstances(ctx context.Context, input *ec2svc.DescribeInstancesInput) (*ec2svc.DescribeInstancesOutput, error) {
	return a.client.DescribeInstances(ctx, input)
}

func (a ec2ClientAdapter) RunInstances(ctx context.Context, input *ec2svc.RunInstancesInput) (*ec2svc.RunInstancesOutput, error) {
	return a.client.RunInstances(ctx, input)
}

func (a ec2ClientAdapter) TerminateInstances(ctx context.Context, input *ec2svc.TerminateInstancesInput) (*ec2svc.TerminateInstancesOutput, error) {
	return a.client.TerminateInstances(ctx, input)
}

func (a ec2ClientAdapter) StartInstances(ctx context.Context, input *ec2svc.StartInstancesInput) (*ec2svc.StartInstancesOutput, error) {
	return a.client.StartInstances(ctx, input)
}

func (a ec2ClientAdapter) ModifyInstanceAttribute(ctx context.Context, input *ec2svc.ModifyInstanceAttributeInput) (*ec2svc.ModifyInstanceAttributeOutput, error) {
	return a.client.ModifyInstanceAttribute(ctx, input)
}

func (a ec2ClientAdapter) DescribeSubnets(ctx context.Context, input *ec2svc.DescribeSubnetsInput) (*ec2svc.DescribeSubnetsOutput, error) {
	return a.client.DescribeSubnets(ctx, input)
}

func (a ec2ClientAdapter) DescribeInstanceTypes(ctx context.Context, input *ec2svc.DescribeInstanceTypesInput) (*ec2svc.DescribeInstanceTypesOutput, error) {
	return a.client.DescribeInstanceTypes(ctx, input)
}

func (a ec2ClientAdapter) DescribeImages(ctx context.Context, input *ec2svc.DescribeImagesInput) (*ec2svc.DescribeImagesOutput, error) {
	return a.client.DescribeImages(ctx, input)
}

func (a ec2ClientAdapter) DescribeSecurityGroups(ctx context.Context, input *ec2svc.DescribeSecurityGroupsInput) (*ec2svc.DescribeSecurityGroupsOutput, error) {
	return a.client.DescribeSecurityGroups(ctx, input)
}

func (a ec2ClientAdapter) CreateSecurityGroup(ctx context.Context, input *ec2svc.CreateSecurityGroupInput) (*ec2svc.CreateSecurityGroupOutput, error) {
	return a.client.CreateSecurityGroup(ctx, input)
}

func (a ec2ClientAdapter) DeleteSecurityGroup(ctx context.Context, input *ec2svc.DeleteSecurityGroupInput) (*ec2svc.DeleteSecurityGroupOutput, error) {
	return a.client.DeleteSecurityGroup(ctx, input)
}

func (a ec2ClientAdapter) AuthorizeSecurityGroupIngress(ctx context.Context, input *ec2svc.AuthorizeSecurityGroupIngressInput) (*ec2svc.AuthorizeSecurityGroupIngressOutput, error) {
	return a.client.AuthorizeSecurityGroupIngress(ctx, input)
}

func (a ec2ClientAdapter) RevokeSecurityGroupIngress(ctx context.Context, input *ec2svc.RevokeSecurityGroupIngressInput) (*ec2svc.RevokeSecurityGroupIngressOutput, error) {
	return a.client.RevokeSecurityGroupIngress(ctx, input)
}

func (a ec2ClientAdapter) CreateTags(ctx context.Context, input *ec2svc.CreateTagsInput) (*ec2svc.CreateTagsOutput, error) {
	return a.client.CreateTags(ctx, input)
}

func (a ec2ClientAdapter) CreateKeyPair(ctx context.Context, input *ec2svc.CreateKeyPairInput) (*ec2svc.CreateKeyPairOutput, error) {
	return a.client.CreateKeyPair(ctx, input)
}

func (a ec2ClientAdapter) DeleteKeyPair(ctx context.Context, input *ec2svc.DeleteKeyPairInput) (*ec2svc.DeleteKeyPairOutput, error) {
	return a.client.DeleteKeyPair(ctx, input)
}

var runSSHCommand = func(ctx context.Context, keyFile, certFile, user, host string) ([]byte, error) {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=8",
		"-i", keyFile,
	}
	if strings.TrimSpace(certFile) != "" {
		args = append(args, "-o", fmt.Sprintf("CertificateFile=%s", certFile))
	}
	args = append(args,
		fmt.Sprintf("%s@%s", user, host),
		"echo", "ok",
	)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	return cmd.CombinedOutput()
}

type Provider struct {
	client        ec2Client
	ctx           context.Context
	region        string
	image         string
	sshUser       string
	caSigner      ssh.Signer
	caPublicKey   string
	sshRetryCount int
	sshRetryDelay time.Duration
	logger        *pullpreview.Logger
}

func newProviderWithClient(ctx context.Context, cfg Config, logger *pullpreview.Logger, client ec2Client) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("client cannot be nil")
	}
	parsedCA, err := sshca.Parse(cfg.CAKey, cfg.CAKeyEnv)
	if err != nil {
		return nil, err
	}
	if logger != nil {
		logger.Infof("EC2 SSH CA pre-check passed (%s)", parsedCA.Source)
	}
	return &Provider{
		client:        client,
		ctx:           pullpreview.EnsureContext(ctx),
		region:        cfg.Region,
		image:         cfg.Image,
		sshUser:       cfg.SSHUsername,
		caSigner:      parsedCA.Signer,
		caPublicKey:   parsedCA.PublicKey,
		sshRetryCount: defaultEC2SSHRetries,
		sshRetryDelay: defaultEC2SSHInterval,
		logger:        logger,
	}, nil
}

func (p *Provider) Name() string {
	return "ec2"
}

func (p *Provider) DisplayName() string {
	return "AWS EC2"
}

func (p *Provider) SupportsSnapshots() bool {
	return false
}

func (p *Provider) SupportsRestore() bool {
	return false
}

func (p *Provider) SupportsFirewall() bool {
	return true
}

func (p *Provider) Username() string {
	return p.sshUser
}

func (p *Provider) BuildUserData(options pullpreview.UserDataOptions) (string, error) {
	lines := []string{
		"#!/usr/bin/env bash",
		"set -xe ; set -o pipefail",
	}
	homeDir := pullpreview.HomeDirForUser(options.Username)
	lines = append(lines, fmt.Sprintf("mkdir -p %s/.ssh", homeDir))
	if len(options.SSHPublicKeys) > 0 {
		lines = append(lines, fmt.Sprintf("echo '%s' >> %s/.ssh/authorized_keys", strings.Join(options.SSHPublicKeys, "\n"), homeDir))
		lines = append(lines,
			fmt.Sprintf("chown -R %s:%s %s/.ssh", options.Username, options.Username, homeDir),
			fmt.Sprintf("chmod 0700 %s/.ssh && chmod 0600 %s/.ssh/authorized_keys", homeDir, homeDir),
		)
	}
	lines = append(lines,
		fmt.Sprintf("mkdir -p %s && chown -R %s:%s %s", options.AppPath, options.Username, options.Username, options.AppPath),
		"mkdir -p /etc/profile.d",
		fmt.Sprintf("echo 'cd %s' > /etc/profile.d/pullpreview.sh", options.AppPath),
		"if command -v dnf >/dev/null 2>&1; then",
		"  dnf -y install dnf-plugins-core",
		"  dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo",
		"  dnf -y install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin",
		"  systemctl restart docker",
		"elif command -v yum >/dev/null 2>&1; then",
		"  yum -y install yum-utils",
		"  yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo",
		"  yum -y install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin",
		"  systemctl restart docker",
		"elif command -v apt-get >/dev/null 2>&1; then",
		"  mkdir -p /etc/apt/keyrings",
		"  install -m 0755 -d /etc/apt/keyrings",
		"  apt-get update",
		"  apt-get install -y ca-certificates curl gnupg lsb-release",
		"  if grep -qi ubuntu /etc/os-release; then DISTRO=ubuntu; else DISTRO=debian; fi",
		"  curl -fsSL https://download.docker.com/linux/$DISTRO/gpg -o /etc/apt/keyrings/docker.asc",
		"  chmod a+r /etc/apt/keyrings/docker.asc",
		"  echo \"deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/$DISTRO $(lsb_release -cs) stable\" > /etc/apt/sources.list.d/docker.list",
		"  apt-get update",
		"  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin",
		"  systemctl restart docker",
		"else",
		"  echo 'unsupported OS family; expected dnf, yum, or apt'",
		"  exit 1",
		"fi",
		"mkdir -p /etc/ssh/sshd_config.d",
		fmt.Sprintf("cat <<'EOF' > /etc/ssh/pullpreview-user-ca.pub\n%s\nEOF", p.caPublicKey),
		"cat <<'EOF' > /etc/ssh/sshd_config.d/pullpreview.conf",
		"TrustedUserCAKeys /etc/ssh/pullpreview-user-ca.pub",
		"EOF",
		"systemctl restart ssh || systemctl restart sshd || true",
		"mkdir -p /etc/pullpreview && touch /etc/pullpreview/ready",
		fmt.Sprintf("chown -R %s:%s /etc/pullpreview", options.Username, options.Username),
	)
	return strings.Join(lines, "\n"), nil
}

func (p *Provider) Launch(name string, opts pullpreview.LaunchOptions) (pullpreview.AccessDetails, error) {
	for {
		existing, err := p.instanceByName(name)
		if err != nil {
			return pullpreview.AccessDetails{}, err
		}
		if existing == nil {
			return p.createInstance(name, opts)
		}
		if err := p.ensureInstanceRunning(existing); err != nil {
			return pullpreview.AccessDetails{}, err
		}
		existing, err = p.instanceByID(aws.ToString(existing.InstanceId))
		if err != nil {
			return pullpreview.AccessDetails{}, err
		}
		if existing == nil {
			continue
		}
		publicIP := p.publicIPAddress(existing)
		if publicIP == "" {
			if p.logger != nil {
				p.logger.Warnf("Existing EC2 instance %q missing public IP; recreating", name)
			}
			if err := p.destroyInstanceAndSecurityGroups(existing, name); err != nil {
				return pullpreview.AccessDetails{}, err
			}
			continue
		}
		sgID, _, err := p.ensureSecurityGroup(name, aws.ToString(existing.VpcId), opts.Ports, opts.CIDRs)
		if err != nil {
			return pullpreview.AccessDetails{}, err
		}
		if err := p.ensureInstanceSecurityGroup(existing, sgID); err != nil {
			return pullpreview.AccessDetails{}, err
		}
		privateKey, cert, err := p.generateSignedAccessCredentials()
		if err != nil {
			return pullpreview.AccessDetails{}, err
		}
		if err := p.validateSSHAccessWithRetry(existing, privateKey, cert, 0); err != nil {
			if p.logger != nil {
				p.logger.Warnf("Existing EC2 instance %q SSH cert check failed; recreating (%v)", name, err)
			}
			if err := p.destroyInstanceAndSecurityGroups(existing, name); err != nil {
				return pullpreview.AccessDetails{}, err
			}
			continue
		}
		if p.logger != nil {
			p.logger.Infof("Reusing existing EC2 instance %s with cert-based SSH credentials", name)
		}
		return pullpreview.AccessDetails{
			Username:   p.sshUser,
			IPAddress:  publicIP,
			PrivateKey: strings.TrimSpace(privateKey),
			CertKey:    strings.TrimSpace(cert),
		}, nil
	}
}

func (p *Provider) createInstance(name string, opts pullpreview.LaunchOptions) (pullpreview.AccessDetails, error) {
	instanceType := resolveEC2InstanceType(opts.Size)
	supportedArchs, err := p.instanceTypeArchitectures(instanceType)
	if err != nil {
		return pullpreview.AccessDetails{}, err
	}
	image, err := p.resolveImage(supportedArchs)
	if err != nil {
		return pullpreview.AccessDetails{}, err
	}
	subnet, err := p.findTaggedPublicSubnet()
	if err != nil {
		return pullpreview.AccessDetails{}, err
	}
	vpcID := aws.ToString(subnet.VpcId)
	sgID, sgCreated, err := p.ensureSecurityGroup(name, vpcID, opts.Ports, opts.CIDRs)
	if err != nil {
		return pullpreview.AccessDetails{}, err
	}
	keyName, bootstrapPrivateKey, err := p.createBootstrapKey(name)
	if err != nil {
		return pullpreview.AccessDetails{}, p.cleanupCreateFailure(name, "", keyName, "", false, err)
	}

	instanceTags := mergeTags(map[string]string{"stack": pullpreview.StackName}, opts.Tags)
	instanceTags["pullpreview_instance_name"] = name
	instanceTags["Name"] = name

	userData := base64.StdEncoding.EncodeToString([]byte(opts.UserData))
	runOut, err := p.client.RunInstances(p.ctx, &ec2svc.RunInstancesInput{
		ImageId:      image.ImageId,
		InstanceType: ec2types.InstanceType(instanceType),
		MinCount:     ptrInt32(1),
		MaxCount:     ptrInt32(1),
		KeyName:      aws.String(keyName),
		UserData:     aws.String(userData),
		NetworkInterfaces: []ec2types.InstanceNetworkInterfaceSpecification{
			{
				DeviceIndex:              ptrInt32(0),
				SubnetId:                 subnet.SubnetId,
				Groups:                   []string{sgID},
				AssociatePublicIpAddress: aws.Bool(true),
			},
		},
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeInstance,
				Tags:         toEC2Tags(instanceTags),
			},
		},
	})
	if err != nil {
		return pullpreview.AccessDetails{}, p.cleanupCreateFailure(name, "", keyName, sgID, sgCreated, err)
	}
	if len(runOut.Instances) == 0 || runOut.Instances[0].InstanceId == nil {
		return pullpreview.AccessDetails{}, p.cleanupCreateFailure(name, "", keyName, sgID, sgCreated, fmt.Errorf("ec2 did not return created instance"))
	}
	instanceID := aws.ToString(runOut.Instances[0].InstanceId)
	instance, err := p.waitForInstanceState(instanceID, ec2types.InstanceStateNameRunning, 60, 5*time.Second)
	if err != nil {
		return pullpreview.AccessDetails{}, p.cleanupCreateFailure(name, instanceID, keyName, sgID, sgCreated, err)
	}
	if err := p.validateSSHAccessWithRetry(instance, bootstrapPrivateKey, "", 0); err != nil {
		return pullpreview.AccessDetails{}, p.cleanupCreateFailure(name, instanceID, keyName, sgID, sgCreated, err)
	}
	if err := p.deleteKeyPairIfExists(keyName); err != nil && p.logger != nil {
		p.logger.Warnf("Unable to delete temporary EC2 key pair %s: %v", keyName, err)
	}

	privateKey, cert, err := p.generateSignedAccessCredentials()
	if err != nil {
		return pullpreview.AccessDetails{}, p.cleanupCreateFailure(name, instanceID, "", sgID, sgCreated, err)
	}
	if err := p.validateSSHAccessWithRetry(instance, privateKey, cert, 0); err != nil {
		return pullpreview.AccessDetails{}, p.cleanupCreateFailure(name, instanceID, "", sgID, sgCreated, err)
	}
	publicIP := p.publicIPAddress(instance)
	if publicIP == "" {
		return pullpreview.AccessDetails{}, p.cleanupCreateFailure(name, instanceID, "", sgID, sgCreated, fmt.Errorf("created instance missing public IP"))
	}

	return pullpreview.AccessDetails{
		Username:   p.sshUser,
		IPAddress:  publicIP,
		PrivateKey: strings.TrimSpace(privateKey),
		CertKey:    strings.TrimSpace(cert),
	}, nil
}

func (p *Provider) cleanupCreateFailure(name, instanceID, keyName, securityGroupID string, deleteSecurityGroup bool, cause error) error {
	if strings.TrimSpace(instanceID) != "" {
		if err := p.terminateInstanceAndWait(instanceID); err != nil && p.logger != nil {
			p.logger.Warnf("Create cleanup: unable to terminate EC2 instance %s: %v", instanceID, err)
		}
	}
	if strings.TrimSpace(keyName) != "" {
		if err := p.deleteKeyPairIfExists(keyName); err != nil && p.logger != nil {
			p.logger.Warnf("Create cleanup: unable to delete EC2 key pair %s: %v", keyName, err)
		}
	}
	if deleteSecurityGroup && strings.TrimSpace(securityGroupID) != "" {
		if err := p.deleteSecurityGroupByID(securityGroupID); err != nil && p.logger != nil {
			p.logger.Warnf("Create cleanup: unable to delete security group %s: %v", securityGroupID, err)
		}
	}
	if cause != nil {
		return cause
	}
	return fmt.Errorf("create cleanup failed for %q", name)
}

func (p *Provider) Terminate(name string) error {
	instance, err := p.instanceByName(name)
	if err != nil {
		return err
	}
	if instance != nil {
		if err := p.terminateInstanceAndWait(aws.ToString(instance.InstanceId)); err != nil {
			return err
		}
	}
	if err := p.deleteSecurityGroupsForInstance(name); err != nil && p.logger != nil {
		p.logger.Warnf("Unable to delete EC2 security group for %s: %v", name, err)
	}
	return nil
}

func (p *Provider) Running(name string) (bool, error) {
	instance, err := p.instanceByName(name)
	if err != nil {
		return false, err
	}
	if instance == nil {
		return false, nil
	}
	return instanceStateName(instance) == ec2types.InstanceStateNameRunning, nil
}

func (p *Provider) ListInstances(tags map[string]string) ([]pullpreview.InstanceSummary, error) {
	filters := []ec2types.Filter{
		{Name: aws.String("instance-state-name"), Values: []string{"pending", "running", "stopping", "stopped"}},
	}
	for key, value := range tags {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		filters = append(filters, ec2types.Filter{Name: aws.String("tag:" + key), Values: []string{value}})
	}
	instances, err := p.describeInstancesAll(&ec2svc.DescribeInstancesInput{Filters: filters})
	if err != nil {
		return nil, err
	}
	result := make([]pullpreview.InstanceSummary, 0, len(instances))
	for _, instance := range instances {
		summary := pullpreview.InstanceSummary{
			Name:      instanceName(instance),
			PublicIP:  p.publicIPAddress(&instance),
			Size:      string(instance.InstanceType),
			Region:    p.region,
			CreatedAt: aws.ToTime(instance.LaunchTime),
			Tags:      tagsToMap(instance.Tags),
		}
		if instance.Placement != nil {
			summary.Zone = aws.ToString(instance.Placement.AvailabilityZone)
		}
		result = append(result, summary)
	}
	return result, nil
}

func (p *Provider) resolveImage(supportedArchs map[string]struct{}) (ec2types.Image, error) {
	value := strings.TrimSpace(p.image)
	if strings.HasPrefix(value, "ami-") {
		output, err := p.client.DescribeImages(p.ctx, &ec2svc.DescribeImagesInput{ImageIds: []string{value}})
		if err != nil {
			return ec2types.Image{}, err
		}
		if len(output.Images) == 0 {
			return ec2types.Image{}, fmt.Errorf("ami %q not found", value)
		}
		image := output.Images[0]
		if image.State != ec2types.ImageStateAvailable {
			return ec2types.Image{}, fmt.Errorf("ami %q is not available (state=%s)", value, image.State)
		}
		if err := ensureAMIArchitectureCompatible(image, supportedArchs, value); err != nil {
			return ec2types.Image{}, err
		}
		return image, nil
	}

	prefix := value
	if prefix == "" {
		prefix = defaultEC2ImagePrefix
	}
	output, err := p.client.DescribeImages(p.ctx, &ec2svc.DescribeImagesInput{
		Owners: []string{"self", "amazon"},
		Filters: []ec2types.Filter{
			{Name: aws.String("name"), Values: []string{prefix + "*"}},
			{Name: aws.String("state"), Values: []string{string(ec2types.ImageStateAvailable)}},
		},
	})
	if err != nil {
		return ec2types.Image{}, err
	}
	if len(output.Images) == 0 {
		return ec2types.Image{}, fmt.Errorf("no available AMI matched prefix %q (owners: self, amazon)", prefix)
	}
	images := append([]ec2types.Image{}, output.Images...)
	sort.Slice(images, func(i, j int) bool {
		left := parseImageCreationDate(images[i].CreationDate)
		right := parseImageCreationDate(images[j].CreationDate)
		if !left.Equal(right) {
			return left.After(right)
		}
		return aws.ToString(images[i].ImageId) > aws.ToString(images[j].ImageId)
	})
	selected := images[0]
	if err := ensureAMIArchitectureCompatible(selected, supportedArchs, prefix); err != nil {
		return ec2types.Image{}, err
	}
	return selected, nil
}

func ensureAMIArchitectureCompatible(image ec2types.Image, supportedArchs map[string]struct{}, imageSource string) error {
	arch := strings.TrimSpace(string(image.Architecture))
	if arch == "" {
		return fmt.Errorf("selected AMI %q from %q has empty architecture", aws.ToString(image.ImageId), imageSource)
	}
	if _, ok := supportedArchs[arch]; ok {
		return nil
	}
	allowed := make([]string, 0, len(supportedArchs))
	for value := range supportedArchs {
		allowed = append(allowed, value)
	}
	sort.Strings(allowed)
	return fmt.Errorf("selected AMI %q architecture %q is incompatible with instance type supported architectures [%s]", aws.ToString(image.ImageId), arch, strings.Join(allowed, ", "))
}

func parseImageCreationDate(raw *string) time.Time {
	value := strings.TrimSpace(aws.ToString(raw))
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return parsed
	}
	parsed, err = time.Parse("2006-01-02T15:04:05.000Z", value)
	if err == nil {
		return parsed
	}
	return time.Time{}
}

func (p *Provider) instanceTypeArchitectures(instanceType string) (map[string]struct{}, error) {
	output, err := p.client.DescribeInstanceTypes(p.ctx, &ec2svc.DescribeInstanceTypesInput{
		InstanceTypes: []ec2types.InstanceType{ec2types.InstanceType(instanceType)},
	})
	if err != nil {
		return nil, err
	}
	if len(output.InstanceTypes) == 0 {
		return nil, fmt.Errorf("instance type %q not found", instanceType)
	}
	architectures := map[string]struct{}{}
	for _, arch := range output.InstanceTypes[0].ProcessorInfo.SupportedArchitectures {
		value := strings.TrimSpace(string(arch))
		if value == "" {
			continue
		}
		architectures[value] = struct{}{}
	}
	if len(architectures) == 0 {
		return nil, fmt.Errorf("instance type %q does not report supported architectures", instanceType)
	}
	return architectures, nil
}

func (p *Provider) findTaggedPublicSubnet() (*ec2types.Subnet, error) {
	output, err := p.client.DescribeSubnets(p.ctx, &ec2svc.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("tag:pullpreview-enabled"), Values: []string{"true"}},
			{Name: aws.String("state"), Values: []string{"available"}},
		},
	})
	if err != nil {
		return nil, err
	}
	publicSubnets := make([]ec2types.Subnet, 0, len(output.Subnets))
	for _, subnet := range output.Subnets {
		if aws.ToBool(subnet.MapPublicIpOnLaunch) {
			publicSubnets = append(publicSubnets, subnet)
		}
	}
	if len(publicSubnets) == 0 {
		return nil, fmt.Errorf("no public subnet with tag pullpreview-enabled=true found in region %s", p.region)
	}
	sort.Slice(publicSubnets, func(i, j int) bool {
		return aws.ToString(publicSubnets[i].SubnetId) < aws.ToString(publicSubnets[j].SubnetId)
	})
	selected := publicSubnets[0]
	return &selected, nil
}

func (p *Provider) createBootstrapKey(name string) (string, string, error) {
	keyName := fmt.Sprintf("pullpreview-%s-%d", sanitizeSecurityGroupName(name), time.Now().UnixNano())
	output, err := p.client.CreateKeyPair(p.ctx, &ec2svc.CreateKeyPairInput{KeyName: aws.String(keyName)})
	if err != nil {
		return "", "", err
	}
	privateKey := strings.TrimSpace(aws.ToString(output.KeyMaterial))
	if privateKey == "" {
		return "", "", fmt.Errorf("ec2 create-key-pair returned empty private key material")
	}
	return keyName, privateKey, nil
}

func (p *Provider) deleteKeyPairIfExists(keyName string) error {
	keyName = strings.TrimSpace(keyName)
	if keyName == "" {
		return nil
	}
	_, err := p.client.DeleteKeyPair(p.ctx, &ec2svc.DeleteKeyPairInput{KeyName: aws.String(keyName)})
	if err != nil && containsAWSError(err, "InvalidKeyPair.NotFound") {
		return nil
	}
	return err
}

func (p *Provider) validateSSHAccessWithRetry(instance *ec2types.Instance, privateKey, certKey string, attempts int) error {
	if attempts <= 0 {
		if p.sshRetryCount > 0 {
			attempts = p.sshRetryCount
		} else {
			attempts = 1
		}
	}
	delay := p.sshRetryDelay
	if delay <= 0 {
		delay = defaultEC2SSHInterval
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := p.validateSSHAccess(instance, privateKey, certKey); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i < attempts-1 {
			if p.logger != nil {
				p.logger.Warnf("SSH access validation failed for EC2 instance %q (attempt %d/%d): %v", instanceNameValue(instance), i+1, attempts, lastErr)
			}
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("ssh access validation failed for instance %q after %d attempts: %w", instanceNameValue(instance), attempts, lastErr)
}

func (p *Provider) validateSSHAccess(instance *ec2types.Instance, privateKey, certKey string) error {
	privateKey = strings.TrimSpace(privateKey)
	if privateKey == "" {
		return fmt.Errorf("empty private key")
	}
	publicIP := p.publicIPAddress(instance)
	if publicIP == "" {
		return fmt.Errorf("instance %q missing public IP", instanceNameValue(instance))
	}
	keyFile, err := os.CreateTemp("", "pullpreview-ec2-key-*")
	if err != nil {
		return err
	}
	if err := keyFile.Close(); err != nil {
		_ = os.Remove(keyFile.Name())
		return err
	}
	if err := os.WriteFile(keyFile.Name(), []byte(privateKey+"\n"), 0600); err != nil {
		_ = os.Remove(keyFile.Name())
		return err
	}
	if err := os.Chmod(keyFile.Name(), 0600); err != nil {
		_ = os.Remove(keyFile.Name())
		return err
	}
	certFile := ""
	if strings.TrimSpace(certKey) != "" {
		certFile = keyFile.Name() + "-cert.pub"
		if err := os.WriteFile(certFile, []byte(strings.TrimSpace(certKey)+"\n"), 0600); err != nil {
			_ = os.Remove(keyFile.Name())
			return err
		}
		defer os.Remove(certFile)
	}
	defer os.Remove(keyFile.Name())

	output, err := runSSHCommand(p.ctx, keyFile.Name(), certFile, p.sshUser, publicIP)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (p *Provider) generateSignedAccessCredentials() (string, string, error) {
	_, privateKey, signer, err := sshca.GenerateSSHKeyPairWithSigner()
	if err != nil {
		return "", "", err
	}
	cert, err := sshca.GenerateUserCertificate(p.caSigner, signer, p.sshUser, defaultEC2SSHCertTTL)
	if err != nil {
		return "", "", err
	}
	return privateKey, cert, nil
}

func (p *Provider) instanceByName(name string) (*ec2types.Instance, error) {
	instances, err := p.describeInstancesAll(&ec2svc.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("tag:pullpreview_instance_name"), Values: []string{strings.TrimSpace(name)}},
			{Name: aws.String("instance-state-name"), Values: []string{"pending", "running", "stopping", "stopped"}},
		},
	})
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return nil, nil
	}
	sort.Slice(instances, func(i, j int) bool {
		left := aws.ToTime(instances[i].LaunchTime)
		right := aws.ToTime(instances[j].LaunchTime)
		if !left.Equal(right) {
			return left.After(right)
		}
		return aws.ToString(instances[i].InstanceId) > aws.ToString(instances[j].InstanceId)
	})
	selected := instances[0]
	return &selected, nil
}

func (p *Provider) instanceByID(instanceID string) (*ec2types.Instance, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil, nil
	}
	instances, err := p.describeInstancesAll(&ec2svc.DescribeInstancesInput{InstanceIds: []string{instanceID}})
	if err != nil {
		if containsAWSError(err, "InvalidInstanceID.NotFound") {
			return nil, nil
		}
		return nil, err
	}
	if len(instances) == 0 {
		return nil, nil
	}
	instance := instances[0]
	return &instance, nil
}

func (p *Provider) describeInstancesAll(input *ec2svc.DescribeInstancesInput) ([]ec2types.Instance, error) {
	if input == nil {
		input = &ec2svc.DescribeInstancesInput{}
	}
	instances := []ec2types.Instance{}
	token := input.NextToken
	for {
		copyInput := *input
		copyInput.NextToken = token
		output, err := p.client.DescribeInstances(p.ctx, &copyInput)
		if err != nil {
			return nil, err
		}
		for _, reservation := range output.Reservations {
			instances = append(instances, reservation.Instances...)
		}
		if output.NextToken == nil || strings.TrimSpace(*output.NextToken) == "" {
			break
		}
		token = output.NextToken
	}
	return instances, nil
}

func (p *Provider) ensureInstanceRunning(instance *ec2types.Instance) error {
	if instance == nil {
		return nil
	}
	state := instanceStateName(instance)
	if state == ec2types.InstanceStateNameRunning {
		return nil
	}
	if state == ec2types.InstanceStateNameStopped {
		_, err := p.client.StartInstances(p.ctx, &ec2svc.StartInstancesInput{InstanceIds: []string{aws.ToString(instance.InstanceId)}})
		if err != nil {
			return err
		}
		_, err = p.waitForInstanceState(aws.ToString(instance.InstanceId), ec2types.InstanceStateNameRunning, 60, 5*time.Second)
		return err
	}
	if state == ec2types.InstanceStateNameStopping {
		_, err := p.waitForInstanceState(aws.ToString(instance.InstanceId), ec2types.InstanceStateNameStopped, 60, 5*time.Second)
		if err != nil {
			return err
		}
		_, err = p.client.StartInstances(p.ctx, &ec2svc.StartInstancesInput{InstanceIds: []string{aws.ToString(instance.InstanceId)}})
		if err != nil {
			return err
		}
		_, err = p.waitForInstanceState(aws.ToString(instance.InstanceId), ec2types.InstanceStateNameRunning, 60, 5*time.Second)
		return err
	}
	if state == ec2types.InstanceStateNamePending {
		_, err := p.waitForInstanceState(aws.ToString(instance.InstanceId), ec2types.InstanceStateNameRunning, 60, 5*time.Second)
		return err
	}
	return fmt.Errorf("instance %q is in unsupported state %s", instanceNameValue(instance), state)
}

func (p *Provider) waitForInstanceState(instanceID string, desired ec2types.InstanceStateName, attempts int, delay time.Duration) (*ec2types.Instance, error) {
	if attempts <= 0 {
		attempts = 1
	}
	if delay <= 0 {
		delay = 5 * time.Second
	}
	var lastState ec2types.InstanceStateName
	for i := 0; i < attempts; i++ {
		instance, err := p.instanceByID(instanceID)
		if err != nil {
			return nil, err
		}
		if instance != nil {
			lastState = instanceStateName(instance)
			if lastState == desired {
				return instance, nil
			}
		}
		if desired == ec2types.InstanceStateNameTerminated && instance == nil {
			return nil, nil
		}
		if i < attempts-1 {
			time.Sleep(delay)
		}
	}
	return nil, fmt.Errorf("timeout waiting for instance %s state %s (last=%s)", instanceID, desired, lastState)
}

func (p *Provider) terminateInstanceAndWait(instanceID string) error {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil
	}
	_, err := p.client.TerminateInstances(p.ctx, &ec2svc.TerminateInstancesInput{InstanceIds: []string{instanceID}})
	if err != nil {
		if containsAWSError(err, "InvalidInstanceID.NotFound") {
			return nil
		}
		return err
	}
	_, err = p.waitForInstanceState(instanceID, ec2types.InstanceStateNameTerminated, 80, 5*time.Second)
	if err != nil {
		return err
	}
	return nil
}

func (p *Provider) ensureSecurityGroup(name, vpcID string, ports, cidrs []string) (string, bool, error) {
	if strings.TrimSpace(vpcID) == "" {
		return "", false, fmt.Errorf("missing VPC for security group setup")
	}
	groups, err := p.securityGroupsForInstance(name, vpcID)
	if err != nil {
		return "", false, err
	}
	created := false
	groupID := ""
	if len(groups) == 0 {
		groupName := securityGroupName(name)
		createdOut, err := p.client.CreateSecurityGroup(p.ctx, &ec2svc.CreateSecurityGroupInput{
			GroupName:   aws.String(groupName),
			Description: aws.String("PullPreview preview environment access"),
			VpcId:       aws.String(vpcID),
		})
		if err != nil {
			if containsAWSError(err, "InvalidGroup.Duplicate") {
				lookup, lookupErr := p.client.DescribeSecurityGroups(p.ctx, &ec2svc.DescribeSecurityGroupsInput{
					Filters: []ec2types.Filter{
						{Name: aws.String("group-name"), Values: []string{groupName}},
						{Name: aws.String("vpc-id"), Values: []string{vpcID}},
					},
				})
				if lookupErr != nil {
					return "", false, lookupErr
				}
				if len(lookup.SecurityGroups) > 0 {
					groupID = aws.ToString(lookup.SecurityGroups[0].GroupId)
				}
				if strings.TrimSpace(groupID) == "" {
					return "", false, err
				}
			} else {
				return "", false, err
			}
		}
		if strings.TrimSpace(groupID) == "" {
			groupID = aws.ToString(createdOut.GroupId)
		}
		if strings.TrimSpace(groupID) == "" {
			return "", false, fmt.Errorf("create security group returned empty group id")
		}
		_, _ = p.client.CreateTags(p.ctx, &ec2svc.CreateTagsInput{
			Resources: []string{groupID},
			Tags: toEC2Tags(map[string]string{
				"Name":                      groupName,
				"stack":                     pullpreview.StackName,
				"pullpreview_instance_name": strings.TrimSpace(name),
			}),
		})
		created = true
	} else {
		sort.Slice(groups, func(i, j int) bool {
			return aws.ToString(groups[i].GroupId) < aws.ToString(groups[j].GroupId)
		})
		groupID = aws.ToString(groups[0].GroupId)
	}
	rules, err := parseSecurityGroupIngressRules(ports, cidrs)
	if err != nil {
		return "", false, err
	}
	if err := p.syncSecurityGroupRules(groupID, rules); err != nil {
		return "", false, err
	}
	return groupID, created, nil
}

func (p *Provider) securityGroupsForInstance(name, vpcID string) ([]ec2types.SecurityGroup, error) {
	filters := []ec2types.Filter{
		{Name: aws.String("tag:pullpreview_instance_name"), Values: []string{strings.TrimSpace(name)}},
	}
	if strings.TrimSpace(vpcID) != "" {
		filters = append(filters, ec2types.Filter{Name: aws.String("vpc-id"), Values: []string{strings.TrimSpace(vpcID)}})
	}
	output, err := p.client.DescribeSecurityGroups(p.ctx, &ec2svc.DescribeSecurityGroupsInput{Filters: filters})
	if err != nil {
		return nil, err
	}
	return output.SecurityGroups, nil
}

func (p *Provider) syncSecurityGroupRules(groupID string, rules []ec2types.IpPermission) error {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return fmt.Errorf("missing security group id")
	}
	output, err := p.client.DescribeSecurityGroups(p.ctx, &ec2svc.DescribeSecurityGroupsInput{GroupIds: []string{groupID}})
	if err != nil {
		return err
	}
	if len(output.SecurityGroups) == 0 {
		return fmt.Errorf("security group %s not found", groupID)
	}
	existing := output.SecurityGroups[0].IpPermissions
	if len(existing) > 0 {
		_, err = p.client.RevokeSecurityGroupIngress(p.ctx, &ec2svc.RevokeSecurityGroupIngressInput{
			GroupId:       aws.String(groupID),
			IpPermissions: existing,
		})
		if err != nil && !containsAWSError(err, "InvalidPermission.NotFound") {
			return err
		}
	}
	if len(rules) > 0 {
		_, err = p.client.AuthorizeSecurityGroupIngress(p.ctx, &ec2svc.AuthorizeSecurityGroupIngressInput{
			GroupId:       aws.String(groupID),
			IpPermissions: rules,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) ensureInstanceSecurityGroup(instance *ec2types.Instance, groupID string) error {
	if instance == nil {
		return fmt.Errorf("missing instance")
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return fmt.Errorf("missing security group")
	}
	groups := []string{}
	hasGroup := false
	for _, group := range instance.SecurityGroups {
		id := strings.TrimSpace(aws.ToString(group.GroupId))
		if id == "" {
			continue
		}
		if id == groupID {
			hasGroup = true
		}
		groups = append(groups, id)
	}
	if hasGroup {
		return nil
	}
	groups = append(groups, groupID)
	_, err := p.client.ModifyInstanceAttribute(p.ctx, &ec2svc.ModifyInstanceAttributeInput{
		InstanceId: instance.InstanceId,
		Groups:     groups,
	})
	return err
}

func (p *Provider) deleteSecurityGroupsForInstance(name string) error {
	groups, err := p.securityGroupsForInstance(name, "")
	if err != nil {
		return err
	}
	var firstErr error
	for _, group := range groups {
		groupID := aws.ToString(group.GroupId)
		if err := p.deleteSecurityGroupByID(groupID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p *Provider) deleteSecurityGroupByID(groupID string) error {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil
	}
	_, err := p.client.DeleteSecurityGroup(p.ctx, &ec2svc.DeleteSecurityGroupInput{GroupId: aws.String(groupID)})
	if err != nil && (containsAWSError(err, "InvalidGroup.NotFound") || containsAWSError(err, "DependencyViolation")) {
		return nil
	}
	return err
}

func (p *Provider) destroyInstanceAndSecurityGroups(instance *ec2types.Instance, name string) error {
	if instance != nil {
		if err := p.terminateInstanceAndWait(aws.ToString(instance.InstanceId)); err != nil {
			return fmt.Errorf("failed to delete instance %q: %w", name, err)
		}
	}
	if err := p.deleteSecurityGroupsForInstance(name); err != nil && p.logger != nil {
		p.logger.Warnf("Unable to cleanup security groups for %s: %v", name, err)
	}
	return nil
}

func (p *Provider) publicIPAddress(instance *ec2types.Instance) string {
	if instance == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(instance.PublicIpAddress))
}

func resolveEC2InstanceType(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultEC2InstanceType
	}
	return value
}

func parseSecurityGroupIngressRules(ports, cidrs []string) ([]ec2types.IpPermission, error) {
	normalizedCIDRs := normalizeCIDRs(cidrs)
	rules := map[string]ec2types.IpPermission{}
	for _, raw := range ports {
		start, end, protocol, err := parseFirewallPort(raw)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%d-%d/%s/%s", start, end, protocol, strings.Join(normalizedCIDRs, ","))
		if _, exists := rules[key]; exists {
			continue
		}
		rules[key] = buildIPPermission(start, end, protocol, normalizedCIDRs)
	}
	const sshPort = 22
	sshCIDRs := []string{"0.0.0.0/0"}
	sshKey := fmt.Sprintf("%d-%d/tcp/%s", sshPort, sshPort, strings.Join(sshCIDRs, ","))
	if _, exists := rules[sshKey]; !exists {
		rules[sshKey] = buildIPPermission(sshPort, sshPort, "tcp", sshCIDRs)
	}
	result := make([]ec2types.IpPermission, 0, len(rules))
	for _, rule := range rules {
		result = append(result, rule)
	}
	return result, nil
}

func buildIPPermission(start, end int, protocol string, cidrs []string) ec2types.IpPermission {
	permission := ec2types.IpPermission{IpProtocol: aws.String(protocol)}
	permission.FromPort = ptrInt32(int32(start))
	permission.ToPort = ptrInt32(int32(end))
	for _, cidr := range cidrs {
		if strings.Contains(cidr, ":") {
			permission.Ipv6Ranges = append(permission.Ipv6Ranges, ec2types.Ipv6Range{CidrIpv6: aws.String(cidr)})
			continue
		}
		permission.IpRanges = append(permission.IpRanges, ec2types.IpRange{CidrIp: aws.String(cidr)})
	}
	return permission
}

func parseFirewallPort(raw string) (int, int, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, "", fmt.Errorf("empty port definition")
	}
	portRange := raw
	protocol := "tcp"
	if idx := strings.Index(raw, "/"); idx >= 0 {
		portRange = strings.TrimSpace(raw[:idx])
		protocol = strings.ToLower(strings.TrimSpace(raw[idx+1:]))
	}
	if protocol == "" {
		protocol = "tcp"
	}
	if protocol != "tcp" && protocol != "udp" && protocol != "icmp" {
		return 0, 0, "", fmt.Errorf("unsupported protocol %s in port definition %q", protocol, raw)
	}
	if strings.Contains(portRange, "-") {
		parts := strings.SplitN(portRange, "-", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return 0, 0, "", fmt.Errorf("invalid port range %q", raw)
		}
		start, err := mustParsePort(parts[0])
		if err != nil {
			return 0, 0, "", fmt.Errorf("invalid port range %q: %w", raw, err)
		}
		end, err := mustParsePort(parts[1])
		if err != nil {
			return 0, 0, "", fmt.Errorf("invalid port range %q: %w", raw, err)
		}
		return start, end, protocol, nil
	}
	port, err := mustParsePort(portRange)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid port %q: %w", raw, err)
	}
	return port, port, protocol, nil
}

func mustParsePort(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty port")
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if value <= 0 || value > 65535 {
		return 0, fmt.Errorf("invalid port %d", value)
	}
	return value, nil
}

func normalizeCIDRs(raw []string) []string {
	if len(raw) == 0 {
		raw = []string{"0.0.0.0/0"}
	}
	normalized := []string{}
	seen := map[string]struct{}{}
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parsed := parseCIDR(value)
		if parsed == "" {
			continue
		}
		if _, ok := seen[parsed]; ok {
			continue
		}
		seen[parsed] = struct{}{}
		normalized = append(normalized, parsed)
	}
	if len(normalized) == 0 {
		normalized = append(normalized, "0.0.0.0/0")
	}
	sort.Strings(normalized)
	return normalized
}

func parseCIDR(value string) string {
	if _, parsed, err := net.ParseCIDR(value); err == nil {
		return parsed.String()
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return ""
	}
	if ip.To4() != nil {
		return fmt.Sprintf("%s/32", ip.String())
	}
	return fmt.Sprintf("%s/128", ip.String())
}

func mergeTags(base, extra map[string]string) map[string]string {
	result := map[string]string{}
	for key, value := range base {
		result[key] = value
	}
	for key, value := range extra {
		result[key] = value
	}
	return result
}

func toEC2Tags(input map[string]string) []ec2types.Tag {
	tags := make([]ec2types.Tag, 0, len(input))
	for key, value := range input {
		k := strings.TrimSpace(key)
		v := strings.TrimSpace(value)
		if k == "" || v == "" {
			continue
		}
		tags = append(tags, ec2types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return tags
}

func tagsToMap(input []ec2types.Tag) map[string]string {
	result := map[string]string{}
	for _, tag := range input {
		result[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return result
}

func instanceName(instance ec2types.Instance) string {
	if name := instanceNameValue(&instance); strings.TrimSpace(name) != "" {
		return name
	}
	return aws.ToString(instance.InstanceId)
}

func instanceNameValue(instance *ec2types.Instance) string {
	if instance == nil {
		return ""
	}
	for _, tag := range instance.Tags {
		if strings.EqualFold(strings.TrimSpace(aws.ToString(tag.Key)), "Name") {
			return strings.TrimSpace(aws.ToString(tag.Value))
		}
	}
	for _, tag := range instance.Tags {
		if strings.EqualFold(strings.TrimSpace(aws.ToString(tag.Key)), "pullpreview_instance_name") {
			return strings.TrimSpace(aws.ToString(tag.Value))
		}
	}
	return strings.TrimSpace(aws.ToString(instance.InstanceId))
}

func instanceStateName(instance *ec2types.Instance) ec2types.InstanceStateName {
	if instance == nil || instance.State == nil {
		return ""
	}
	return instance.State.Name
}

func securityGroupName(instanceName string) string {
	name := sanitizeSecurityGroupName(instanceName)
	if len(name) > 240 {
		name = name[:240]
	}
	if name == "" {
		name = "instance"
	}
	return "pullpreview-" + name
}

func sanitizeSecurityGroupName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "instance"
	}
	value = ec2SecurityGroupNameSanitizer.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		value = "instance"
	}
	return value
}

func containsAWSError(err error, code string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), code)
}

func ptrInt32(value int32) *int32 {
	return &value
}
