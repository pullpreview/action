package ec2

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2svc "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/pullpreview/action/internal/providers/sshca"
	"github.com/pullpreview/action/internal/pullpreview"
)

func TestParseConfigFromEnv(t *testing.T) {
	caKey := mustTestCAKey(t)

	cfgRaw, err := ParseConfigFromEnv(map[string]string{
		"REGION":             "us-west-2",
		"IMAGE":              "my-prefix",
		"PULLPREVIEW_CA_KEY": caKey,
	})
	if err != nil {
		t.Fatalf("ParseConfigFromEnv() error: %v", err)
	}
	cfg := cfgRaw.(Config)
	if cfg.Region != "us-west-2" {
		t.Fatalf("expected region override, got %q", cfg.Region)
	}
	if cfg.Image != "my-prefix" {
		t.Fatalf("expected image prefix, got %q", cfg.Image)
	}
	if cfg.CAKey != caKey {
		t.Fatalf("expected CA key, got %q", cfg.CAKey)
	}
	if cfg.SSHUsername != defaultEC2SSHUser {
		t.Fatalf("expected default username %q, got %q", defaultEC2SSHUser, cfg.SSHUsername)
	}

	cfgRaw, err = ParseConfigFromEnv(map[string]string{
		"AWS_REGION":         "eu-central-1",
		"PULLPREVIEW_CA_KEY": caKey,
	})
	if err != nil {
		t.Fatalf("ParseConfigFromEnv() fallback region error: %v", err)
	}
	cfg = cfgRaw.(Config)
	if cfg.Region != "eu-central-1" {
		t.Fatalf("expected AWS_REGION fallback, got %q", cfg.Region)
	}

	if _, err := ParseConfigFromEnv(map[string]string{"REGION": "us-east-1"}); err == nil {
		t.Fatalf("expected missing CA key error")
	}
}

func TestResolveImageByAMIID(t *testing.T) {
	provider, fake := newTestProvider(t, Config{
		Region:      "us-east-1",
		Image:       "ami-1234567890",
		CAKey:       mustTestCAKey(t),
		CAKeyEnv:    "PULLPREVIEW_CA_KEY",
		SSHUsername: defaultEC2SSHUser,
	})

	fake.describeImagesFn = func(_ context.Context, input *ec2svc.DescribeImagesInput) (*ec2svc.DescribeImagesOutput, error) {
		if len(input.ImageIds) != 1 || input.ImageIds[0] != "ami-1234567890" {
			return nil, fmt.Errorf("unexpected image id lookup: %#v", input.ImageIds)
		}
		return &ec2svc.DescribeImagesOutput{Images: []ec2types.Image{
			{ImageId: aws.String("ami-1234567890"), State: ec2types.ImageStateAvailable, Architecture: ec2types.ArchitectureValuesX8664},
		}}, nil
	}

	image, err := provider.resolveImage(map[string]struct{}{"x86_64": {}})
	if err != nil {
		t.Fatalf("resolveImage() error: %v", err)
	}
	if aws.ToString(image.ImageId) != "ami-1234567890" {
		t.Fatalf("unexpected selected image: %q", aws.ToString(image.ImageId))
	}
}

func TestResolveImagePrefixUsesAvailabilityFilterOnlyAndNewest(t *testing.T) {
	provider, fake := newTestProvider(t, Config{
		Region:      "us-east-1",
		Image:       "pullpreview-app",
		CAKey:       mustTestCAKey(t),
		CAKeyEnv:    "PULLPREVIEW_CA_KEY",
		SSHUsername: defaultEC2SSHUser,
	})

	fake.describeImagesFn = func(_ context.Context, input *ec2svc.DescribeImagesInput) (*ec2svc.DescribeImagesOutput, error) {
		if len(input.Owners) != 2 || input.Owners[0] != "self" || input.Owners[1] != "amazon" {
			return nil, fmt.Errorf("unexpected owners: %#v", input.Owners)
		}
		hasStateFilter := false
		hasArchFilter := false
		for _, filter := range input.Filters {
			if aws.ToString(filter.Name) == "state" {
				hasStateFilter = true
			}
			if aws.ToString(filter.Name) == "architecture" {
				hasArchFilter = true
			}
		}
		if !hasStateFilter {
			return nil, fmt.Errorf("missing state=available filter")
		}
		if hasArchFilter {
			return nil, fmt.Errorf("unexpected architecture filter")
		}
		return &ec2svc.DescribeImagesOutput{Images: []ec2types.Image{
			{ImageId: aws.String("ami-older"), State: ec2types.ImageStateAvailable, CreationDate: aws.String("2026-01-01T00:00:00Z"), Architecture: ec2types.ArchitectureValuesX8664},
			{ImageId: aws.String("ami-newest"), State: ec2types.ImageStateAvailable, CreationDate: aws.String("2026-01-02T00:00:00Z"), Architecture: ec2types.ArchitectureValuesX8664},
		}}, nil
	}

	image, err := provider.resolveImage(map[string]struct{}{"x86_64": {}})
	if err != nil {
		t.Fatalf("resolveImage() error: %v", err)
	}
	if aws.ToString(image.ImageId) != "ami-newest" {
		t.Fatalf("expected newest AMI to be selected, got %q", aws.ToString(image.ImageId))
	}
}

func TestResolveImageFailsFastOnArchMismatchWithoutFallback(t *testing.T) {
	provider, fake := newTestProvider(t, Config{
		Region:      "us-east-1",
		Image:       "pullpreview-app",
		CAKey:       mustTestCAKey(t),
		CAKeyEnv:    "PULLPREVIEW_CA_KEY",
		SSHUsername: defaultEC2SSHUser,
	})

	fake.describeImagesFn = func(_ context.Context, _ *ec2svc.DescribeImagesInput) (*ec2svc.DescribeImagesOutput, error) {
		return &ec2svc.DescribeImagesOutput{Images: []ec2types.Image{
			{ImageId: aws.String("ami-newest-arm"), State: ec2types.ImageStateAvailable, CreationDate: aws.String("2026-01-03T00:00:00Z"), Architecture: ec2types.ArchitectureValuesArm64},
			{ImageId: aws.String("ami-older-x86"), State: ec2types.ImageStateAvailable, CreationDate: aws.String("2026-01-01T00:00:00Z"), Architecture: ec2types.ArchitectureValuesX8664},
		}}, nil
	}

	_, err := provider.resolveImage(map[string]struct{}{"x86_64": {}})
	if err == nil {
		t.Fatalf("expected architecture mismatch failure")
	}
	if !strings.Contains(err.Error(), "ami-newest-arm") {
		t.Fatalf("expected failure to reference newest incompatible AMI, got: %v", err)
	}
	if strings.Contains(err.Error(), "ami-older-x86") {
		t.Fatalf("expected no fallback to older compatible AMI, got: %v", err)
	}
}

func TestFindTaggedPublicSubnetFailsWhenNoPublicSubnet(t *testing.T) {
	provider, fake := newTestProvider(t, Config{
		Region:      "us-east-1",
		CAKey:       mustTestCAKey(t),
		CAKeyEnv:    "PULLPREVIEW_CA_KEY",
		SSHUsername: defaultEC2SSHUser,
	})

	fake.describeSubnetsFn = func(_ context.Context, _ *ec2svc.DescribeSubnetsInput) (*ec2svc.DescribeSubnetsOutput, error) {
		return &ec2svc.DescribeSubnetsOutput{Subnets: []ec2types.Subnet{
			{SubnetId: aws.String("subnet-private"), MapPublicIpOnLaunch: aws.Bool(false)},
		}}, nil
	}

	_, err := provider.findTaggedPublicSubnet()
	if err == nil {
		t.Fatalf("expected missing tagged public subnet error")
	}
}

func TestLaunchReusesExistingInstance(t *testing.T) {
	provider, fake := newTestProvider(t, Config{
		Region:      "us-east-1",
		CAKey:       mustTestCAKey(t),
		CAKeyEnv:    "PULLPREVIEW_CA_KEY",
		SSHUsername: defaultEC2SSHUser,
	})
	instance := ec2types.Instance{
		InstanceId:       aws.String("i-1"),
		InstanceType:     ec2types.InstanceTypeT3Small,
		VpcId:            aws.String("vpc-1"),
		PublicIpAddress:  aws.String("203.0.113.10"),
		SecurityGroups:   []ec2types.GroupIdentifier{{GroupId: aws.String("sg-1")}},
		State:            &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
		Tags:             []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String("gh-1-pr-1")}, {Key: aws.String("pullpreview_instance_name"), Value: aws.String("gh-1-pr-1")}},
		LaunchTime:       aws.Time(time.Now()),
		Placement:        &ec2types.Placement{AvailabilityZone: aws.String("us-east-1a")},
		PrivateIpAddress: aws.String("10.0.0.1"),
	}

	fake.describeInstancesFn = func(_ context.Context, _ *ec2svc.DescribeInstancesInput) (*ec2svc.DescribeInstancesOutput, error) {
		return &ec2svc.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{instance}}}}, nil
	}
	fake.describeSecurityGroupsFn = func(_ context.Context, input *ec2svc.DescribeSecurityGroupsInput) (*ec2svc.DescribeSecurityGroupsOutput, error) {
		if len(input.GroupIds) > 0 {
			return &ec2svc.DescribeSecurityGroupsOutput{SecurityGroups: []ec2types.SecurityGroup{{GroupId: aws.String("sg-1"), IpPermissions: nil}}}, nil
		}
		return &ec2svc.DescribeSecurityGroupsOutput{SecurityGroups: []ec2types.SecurityGroup{{GroupId: aws.String("sg-1")}}}, nil
	}
	fake.authorizeSecurityGroupIngressFn = func(_ context.Context, _ *ec2svc.AuthorizeSecurityGroupIngressInput) (*ec2svc.AuthorizeSecurityGroupIngressOutput, error) {
		return &ec2svc.AuthorizeSecurityGroupIngressOutput{}, nil
	}

	origRunSSH := runSSHCommand
	runSSHCommand = func(context.Context, string, string, string, string) ([]byte, error) {
		return []byte("ok"), nil
	}
	defer func() { runSSHCommand = origRunSSH }()

	access, err := provider.Launch("gh-1-pr-1", pullpreview.LaunchOptions{Ports: []string{"80/tcp"}, CIDRs: []string{"0.0.0.0/0"}})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}
	if access.IPAddress != "203.0.113.10" {
		t.Fatalf("unexpected access ip: %q", access.IPAddress)
	}
	if strings.TrimSpace(access.CertKey) == "" {
		t.Fatalf("expected cert key on reused launch")
	}
	if fake.runInstancesCalls != 0 {
		t.Fatalf("expected no RunInstances calls, got %d", fake.runInstancesCalls)
	}
}

func TestLaunchRecreatesWhenExistingSSHValidationFails(t *testing.T) {
	provider, fake := newTestProvider(t, Config{
		Region:      "us-east-1",
		CAKey:       mustTestCAKey(t),
		CAKeyEnv:    "PULLPREVIEW_CA_KEY",
		SSHUsername: defaultEC2SSHUser,
	})
	provider.sshRetryCount = 1
	provider.sshRetryDelay = 1 * time.Millisecond

	oldInstance := ec2types.Instance{
		InstanceId:      aws.String("i-old"),
		InstanceType:    ec2types.InstanceTypeT3Small,
		VpcId:           aws.String("vpc-1"),
		PublicIpAddress: aws.String("203.0.113.10"),
		SecurityGroups:  []ec2types.GroupIdentifier{{GroupId: aws.String("sg-old")}},
		State:           &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
		Tags: []ec2types.Tag{
			{Key: aws.String("Name"), Value: aws.String("gh-9-pr-9")},
			{Key: aws.String("pullpreview_instance_name"), Value: aws.String("gh-9-pr-9")},
		},
		LaunchTime: aws.Time(time.Now().Add(-time.Hour)),
		Placement:  &ec2types.Placement{AvailabilityZone: aws.String("us-east-1a")},
	}
	newInstance := ec2types.Instance{
		InstanceId:      aws.String("i-new"),
		InstanceType:    ec2types.InstanceTypeT3Small,
		VpcId:           aws.String("vpc-1"),
		PublicIpAddress: aws.String("198.51.100.20"),
		SecurityGroups:  []ec2types.GroupIdentifier{{GroupId: aws.String("sg-new")}},
		State:           &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
		Tags: []ec2types.Tag{
			{Key: aws.String("Name"), Value: aws.String("gh-9-pr-9")},
			{Key: aws.String("pullpreview_instance_name"), Value: aws.String("gh-9-pr-9")},
		},
		LaunchTime: aws.Time(time.Now()),
		Placement:  &ec2types.Placement{AvailabilityZone: aws.String("us-east-1a")},
	}

	state := "existing"
	sgDeleted := false

	fake.describeInstancesFn = func(_ context.Context, input *ec2svc.DescribeInstancesInput) (*ec2svc.DescribeInstancesOutput, error) {
		if len(input.InstanceIds) > 0 {
			id := input.InstanceIds[0]
			switch id {
			case "i-old":
				if state == "existing" {
					return &ec2svc.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{oldInstance}}}}, nil
				}
				return &ec2svc.DescribeInstancesOutput{}, nil
			case "i-new":
				if state == "created" {
					return &ec2svc.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{newInstance}}}}, nil
				}
				return &ec2svc.DescribeInstancesOutput{}, nil
			}
			return &ec2svc.DescribeInstancesOutput{}, nil
		}
		if state == "existing" {
			return &ec2svc.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{oldInstance}}}}, nil
		}
		if state == "created" {
			return &ec2svc.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{newInstance}}}}, nil
		}
		return &ec2svc.DescribeInstancesOutput{}, nil
	}
	fake.describeSecurityGroupsFn = func(_ context.Context, input *ec2svc.DescribeSecurityGroupsInput) (*ec2svc.DescribeSecurityGroupsOutput, error) {
		if len(input.GroupIds) > 0 {
			groupID := input.GroupIds[0]
			return &ec2svc.DescribeSecurityGroupsOutput{SecurityGroups: []ec2types.SecurityGroup{{GroupId: aws.String(groupID)}}}, nil
		}
		if state == "existing" && !sgDeleted {
			return &ec2svc.DescribeSecurityGroupsOutput{SecurityGroups: []ec2types.SecurityGroup{{GroupId: aws.String("sg-old")}}}, nil
		}
		if state == "created" {
			return &ec2svc.DescribeSecurityGroupsOutput{SecurityGroups: []ec2types.SecurityGroup{{GroupId: aws.String("sg-new")}}}, nil
		}
		return &ec2svc.DescribeSecurityGroupsOutput{}, nil
	}
	fake.authorizeSecurityGroupIngressFn = func(_ context.Context, _ *ec2svc.AuthorizeSecurityGroupIngressInput) (*ec2svc.AuthorizeSecurityGroupIngressOutput, error) {
		return &ec2svc.AuthorizeSecurityGroupIngressOutput{}, nil
	}
	fake.terminateInstancesFn = func(_ context.Context, _ *ec2svc.TerminateInstancesInput) (*ec2svc.TerminateInstancesOutput, error) {
		state = "creating"
		return &ec2svc.TerminateInstancesOutput{}, nil
	}
	fake.deleteSecurityGroupFn = func(_ context.Context, _ *ec2svc.DeleteSecurityGroupInput) (*ec2svc.DeleteSecurityGroupOutput, error) {
		sgDeleted = true
		return &ec2svc.DeleteSecurityGroupOutput{}, nil
	}
	fake.describeSubnetsFn = func(_ context.Context, _ *ec2svc.DescribeSubnetsInput) (*ec2svc.DescribeSubnetsOutput, error) {
		return &ec2svc.DescribeSubnetsOutput{Subnets: []ec2types.Subnet{{SubnetId: aws.String("subnet-1"), VpcId: aws.String("vpc-1"), MapPublicIpOnLaunch: aws.Bool(true)}}}, nil
	}
	fake.describeImagesFn = func(_ context.Context, _ *ec2svc.DescribeImagesInput) (*ec2svc.DescribeImagesOutput, error) {
		return &ec2svc.DescribeImagesOutput{Images: []ec2types.Image{
			{ImageId: aws.String("ami-123"), State: ec2types.ImageStateAvailable, CreationDate: aws.String("2026-01-02T00:00:00Z"), Architecture: ec2types.ArchitectureValuesX8664},
		}}, nil
	}
	fake.runInstancesFn = func(_ context.Context, _ *ec2svc.RunInstancesInput) (*ec2svc.RunInstancesOutput, error) {
		state = "created"
		return &ec2svc.RunInstancesOutput{Instances: []ec2types.Instance{{InstanceId: aws.String("i-new")}}}, nil
	}

	origRunSSH := runSSHCommand
	runSSHCommand = func(_ context.Context, _ string, certFile string, _ string, host string) ([]byte, error) {
		if host == "203.0.113.10" && strings.TrimSpace(certFile) != "" {
			return nil, fmt.Errorf("cert rejected")
		}
		return []byte("ok"), nil
	}
	defer func() { runSSHCommand = origRunSSH }()

	access, err := provider.Launch("gh-9-pr-9", pullpreview.LaunchOptions{Ports: []string{"80/tcp"}, CIDRs: []string{"0.0.0.0/0"}})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}
	if access.IPAddress != "198.51.100.20" {
		t.Fatalf("expected recreated instance IP, got %q", access.IPAddress)
	}
	if fake.runInstancesCalls == 0 {
		t.Fatalf("expected create path to run after stale SSH failure")
	}
	if fake.terminateInstancesCalls == 0 {
		t.Fatalf("expected stale instance termination")
	}
}

func TestTerminateAndListInstances(t *testing.T) {
	provider, fake := newTestProvider(t, Config{
		Region:      "us-east-1",
		CAKey:       mustTestCAKey(t),
		CAKeyEnv:    "PULLPREVIEW_CA_KEY",
		SSHUsername: defaultEC2SSHUser,
	})
	instance := ec2types.Instance{
		InstanceId:      aws.String("i-terminate"),
		InstanceType:    ec2types.InstanceTypeT3Small,
		PublicIpAddress: aws.String("198.51.100.8"),
		VpcId:           aws.String("vpc-1"),
		State:           &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
		Tags: []ec2types.Tag{
			{Key: aws.String("Name"), Value: aws.String("gh-2-pr-5")},
			{Key: aws.String("pullpreview_instance_name"), Value: aws.String("gh-2-pr-5")},
			{Key: aws.String("repo_name"), Value: aws.String("action")},
			{Key: aws.String("org_name"), Value: aws.String("pullpreview")},
			{Key: aws.String("stack"), Value: aws.String(pullpreview.StackName)},
		},
		LaunchTime: aws.Time(time.Unix(0, 0)),
		Placement:  &ec2types.Placement{AvailabilityZone: aws.String("us-east-1a")},
	}

	fake.describeInstancesFn = func(_ context.Context, input *ec2svc.DescribeInstancesInput) (*ec2svc.DescribeInstancesOutput, error) {
		if len(input.InstanceIds) > 0 {
			return &ec2svc.DescribeInstancesOutput{}, nil
		}
		return &ec2svc.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{instance}}}}, nil
	}
	fake.terminateInstancesFn = func(_ context.Context, _ *ec2svc.TerminateInstancesInput) (*ec2svc.TerminateInstancesOutput, error) {
		fake.terminateInstancesCalls++
		return &ec2svc.TerminateInstancesOutput{}, nil
	}
	fake.describeSecurityGroupsFn = func(_ context.Context, _ *ec2svc.DescribeSecurityGroupsInput) (*ec2svc.DescribeSecurityGroupsOutput, error) {
		return &ec2svc.DescribeSecurityGroupsOutput{SecurityGroups: []ec2types.SecurityGroup{{GroupId: aws.String("sg-x")}}}, nil
	}
	fake.deleteSecurityGroupFn = func(_ context.Context, _ *ec2svc.DeleteSecurityGroupInput) (*ec2svc.DeleteSecurityGroupOutput, error) {
		fake.deleteSecurityGroupCalls++
		return &ec2svc.DeleteSecurityGroupOutput{}, nil
	}

	if err := provider.Terminate("gh-2-pr-5"); err != nil {
		t.Fatalf("Terminate() error: %v", err)
	}
	if fake.terminateInstancesCalls == 0 {
		t.Fatalf("expected TerminateInstances call")
	}
	if fake.deleteSecurityGroupCalls == 0 {
		t.Fatalf("expected security group cleanup call")
	}

	instances, err := provider.ListInstances(map[string]string{"stack": pullpreview.StackName})
	if err != nil {
		t.Fatalf("ListInstances() error: %v", err)
	}
	if len(instances) != 1 || instances[0].Name != "gh-2-pr-5" {
		t.Fatalf("unexpected list result: %#v", instances)
	}
}

func newTestProvider(t *testing.T, cfg Config) (*Provider, *fakeEC2Client) {
	t.Helper()
	if strings.TrimSpace(cfg.CAKey) == "" {
		cfg.CAKey = mustTestCAKey(t)
	}
	if cfg.CAKeyEnv == "" {
		cfg.CAKeyEnv = "PULLPREVIEW_CA_KEY"
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.SSHUsername == "" {
		cfg.SSHUsername = defaultEC2SSHUser
	}
	fake := &fakeEC2Client{}
	provider, err := newProviderWithClient(context.Background(), cfg, nil, fake)
	if err != nil {
		t.Fatalf("newProviderWithClient() error: %v", err)
	}
	return provider, fake
}

func mustTestCAKey(t *testing.T) string {
	t.Helper()
	_, privateKey, _, err := sshca.GenerateSSHKeyPairWithSigner()
	if err != nil {
		t.Fatalf("GenerateSSHKeyPairWithSigner() error: %v", err)
	}
	return privateKey
}

type fakeEC2Client struct {
	describeInstancesFn             func(context.Context, *ec2svc.DescribeInstancesInput) (*ec2svc.DescribeInstancesOutput, error)
	runInstancesFn                  func(context.Context, *ec2svc.RunInstancesInput) (*ec2svc.RunInstancesOutput, error)
	terminateInstancesFn            func(context.Context, *ec2svc.TerminateInstancesInput) (*ec2svc.TerminateInstancesOutput, error)
	startInstancesFn                func(context.Context, *ec2svc.StartInstancesInput) (*ec2svc.StartInstancesOutput, error)
	modifyInstanceAttributeFn       func(context.Context, *ec2svc.ModifyInstanceAttributeInput) (*ec2svc.ModifyInstanceAttributeOutput, error)
	describeSubnetsFn               func(context.Context, *ec2svc.DescribeSubnetsInput) (*ec2svc.DescribeSubnetsOutput, error)
	describeInstanceTypesFn         func(context.Context, *ec2svc.DescribeInstanceTypesInput) (*ec2svc.DescribeInstanceTypesOutput, error)
	describeImagesFn                func(context.Context, *ec2svc.DescribeImagesInput) (*ec2svc.DescribeImagesOutput, error)
	describeSecurityGroupsFn        func(context.Context, *ec2svc.DescribeSecurityGroupsInput) (*ec2svc.DescribeSecurityGroupsOutput, error)
	createSecurityGroupFn           func(context.Context, *ec2svc.CreateSecurityGroupInput) (*ec2svc.CreateSecurityGroupOutput, error)
	deleteSecurityGroupFn           func(context.Context, *ec2svc.DeleteSecurityGroupInput) (*ec2svc.DeleteSecurityGroupOutput, error)
	authorizeSecurityGroupIngressFn func(context.Context, *ec2svc.AuthorizeSecurityGroupIngressInput) (*ec2svc.AuthorizeSecurityGroupIngressOutput, error)
	revokeSecurityGroupIngressFn    func(context.Context, *ec2svc.RevokeSecurityGroupIngressInput) (*ec2svc.RevokeSecurityGroupIngressOutput, error)
	createTagsFn                    func(context.Context, *ec2svc.CreateTagsInput) (*ec2svc.CreateTagsOutput, error)
	createKeyPairFn                 func(context.Context, *ec2svc.CreateKeyPairInput) (*ec2svc.CreateKeyPairOutput, error)
	deleteKeyPairFn                 func(context.Context, *ec2svc.DeleteKeyPairInput) (*ec2svc.DeleteKeyPairOutput, error)

	runInstancesCalls        int
	terminateInstancesCalls  int
	deleteSecurityGroupCalls int
}

func (f *fakeEC2Client) DescribeInstances(ctx context.Context, input *ec2svc.DescribeInstancesInput) (*ec2svc.DescribeInstancesOutput, error) {
	if f.describeInstancesFn != nil {
		return f.describeInstancesFn(ctx, input)
	}
	return &ec2svc.DescribeInstancesOutput{}, nil
}

func (f *fakeEC2Client) RunInstances(ctx context.Context, input *ec2svc.RunInstancesInput) (*ec2svc.RunInstancesOutput, error) {
	f.runInstancesCalls++
	if f.runInstancesFn != nil {
		return f.runInstancesFn(ctx, input)
	}
	return &ec2svc.RunInstancesOutput{}, nil
}

func (f *fakeEC2Client) TerminateInstances(ctx context.Context, input *ec2svc.TerminateInstancesInput) (*ec2svc.TerminateInstancesOutput, error) {
	f.terminateInstancesCalls++
	if f.terminateInstancesFn != nil {
		return f.terminateInstancesFn(ctx, input)
	}
	return &ec2svc.TerminateInstancesOutput{}, nil
}

func (f *fakeEC2Client) StartInstances(ctx context.Context, input *ec2svc.StartInstancesInput) (*ec2svc.StartInstancesOutput, error) {
	if f.startInstancesFn != nil {
		return f.startInstancesFn(ctx, input)
	}
	return &ec2svc.StartInstancesOutput{}, nil
}

func (f *fakeEC2Client) ModifyInstanceAttribute(ctx context.Context, input *ec2svc.ModifyInstanceAttributeInput) (*ec2svc.ModifyInstanceAttributeOutput, error) {
	if f.modifyInstanceAttributeFn != nil {
		return f.modifyInstanceAttributeFn(ctx, input)
	}
	return &ec2svc.ModifyInstanceAttributeOutput{}, nil
}

func (f *fakeEC2Client) DescribeSubnets(ctx context.Context, input *ec2svc.DescribeSubnetsInput) (*ec2svc.DescribeSubnetsOutput, error) {
	if f.describeSubnetsFn != nil {
		return f.describeSubnetsFn(ctx, input)
	}
	return &ec2svc.DescribeSubnetsOutput{}, nil
}

func (f *fakeEC2Client) DescribeInstanceTypes(ctx context.Context, input *ec2svc.DescribeInstanceTypesInput) (*ec2svc.DescribeInstanceTypesOutput, error) {
	if f.describeInstanceTypesFn != nil {
		return f.describeInstanceTypesFn(ctx, input)
	}
	return &ec2svc.DescribeInstanceTypesOutput{InstanceTypes: []ec2types.InstanceTypeInfo{{ProcessorInfo: &ec2types.ProcessorInfo{SupportedArchitectures: []ec2types.ArchitectureType{ec2types.ArchitectureTypeX8664}}}}}, nil
}

func (f *fakeEC2Client) DescribeImages(ctx context.Context, input *ec2svc.DescribeImagesInput) (*ec2svc.DescribeImagesOutput, error) {
	if f.describeImagesFn != nil {
		return f.describeImagesFn(ctx, input)
	}
	return &ec2svc.DescribeImagesOutput{}, nil
}

func (f *fakeEC2Client) DescribeSecurityGroups(ctx context.Context, input *ec2svc.DescribeSecurityGroupsInput) (*ec2svc.DescribeSecurityGroupsOutput, error) {
	if f.describeSecurityGroupsFn != nil {
		return f.describeSecurityGroupsFn(ctx, input)
	}
	return &ec2svc.DescribeSecurityGroupsOutput{}, nil
}

func (f *fakeEC2Client) CreateSecurityGroup(ctx context.Context, input *ec2svc.CreateSecurityGroupInput) (*ec2svc.CreateSecurityGroupOutput, error) {
	if f.createSecurityGroupFn != nil {
		return f.createSecurityGroupFn(ctx, input)
	}
	return &ec2svc.CreateSecurityGroupOutput{GroupId: aws.String("sg-created")}, nil
}

func (f *fakeEC2Client) DeleteSecurityGroup(ctx context.Context, input *ec2svc.DeleteSecurityGroupInput) (*ec2svc.DeleteSecurityGroupOutput, error) {
	f.deleteSecurityGroupCalls++
	if f.deleteSecurityGroupFn != nil {
		return f.deleteSecurityGroupFn(ctx, input)
	}
	return &ec2svc.DeleteSecurityGroupOutput{}, nil
}

func (f *fakeEC2Client) AuthorizeSecurityGroupIngress(ctx context.Context, input *ec2svc.AuthorizeSecurityGroupIngressInput) (*ec2svc.AuthorizeSecurityGroupIngressOutput, error) {
	if f.authorizeSecurityGroupIngressFn != nil {
		return f.authorizeSecurityGroupIngressFn(ctx, input)
	}
	return &ec2svc.AuthorizeSecurityGroupIngressOutput{}, nil
}

func (f *fakeEC2Client) RevokeSecurityGroupIngress(ctx context.Context, input *ec2svc.RevokeSecurityGroupIngressInput) (*ec2svc.RevokeSecurityGroupIngressOutput, error) {
	if f.revokeSecurityGroupIngressFn != nil {
		return f.revokeSecurityGroupIngressFn(ctx, input)
	}
	return &ec2svc.RevokeSecurityGroupIngressOutput{}, nil
}

func (f *fakeEC2Client) CreateTags(ctx context.Context, input *ec2svc.CreateTagsInput) (*ec2svc.CreateTagsOutput, error) {
	if f.createTagsFn != nil {
		return f.createTagsFn(ctx, input)
	}
	return &ec2svc.CreateTagsOutput{}, nil
}

func (f *fakeEC2Client) CreateKeyPair(ctx context.Context, input *ec2svc.CreateKeyPairInput) (*ec2svc.CreateKeyPairOutput, error) {
	if f.createKeyPairFn != nil {
		return f.createKeyPairFn(ctx, input)
	}
	return &ec2svc.CreateKeyPairOutput{KeyMaterial: aws.String("PRIVATE")}, nil
}

func (f *fakeEC2Client) DeleteKeyPair(ctx context.Context, input *ec2svc.DeleteKeyPairInput) (*ec2svc.DeleteKeyPairOutput, error) {
	if f.deleteKeyPairFn != nil {
		return f.deleteKeyPairFn(ctx, input)
	}
	return &ec2svc.DeleteKeyPairOutput{}, nil
}
