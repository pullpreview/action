package lightsail

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ls "github.com/aws/aws-sdk-go-v2/service/lightsail"
	"github.com/aws/aws-sdk-go-v2/service/lightsail/types"
	"github.com/pullpreview/action/internal/pullpreview"
)

type fakeLightsailClient struct {
	instanceStateOutput              *ls.GetInstanceStateOutput
	instanceStateByName              map[string]*ls.GetInstanceStateOutput
	deleteInstanceCalls              int
	createInstancesCalls             int
	createInstancesInput             *ls.CreateInstancesInput
	createInstancesFromSnapshotCalls int
	createInstancesFromSnapshotInput *ls.CreateInstancesFromSnapshotInput
	putInstancePublicPortsCalls      int
	getInstanceAccessDetailsOutput   *ls.GetInstanceAccessDetailsOutput
	getInstanceOutput                *ls.GetInstanceOutput
	getInstanceByName                map[string]*types.Instance
	getInstanceSnapshotsOutput       *ls.GetInstanceSnapshotsOutput
	getInstancesOutput               *ls.GetInstancesOutput
	getRegionsOutput                 *ls.GetRegionsOutput
	getBlueprintsOutput              *ls.GetBlueprintsOutput
	getBundlesOutput                 *ls.GetBundlesOutput
}

func (f *fakeLightsailClient) GetInstanceState(ctx context.Context, input *ls.GetInstanceStateInput, optFns ...func(*ls.Options)) (*ls.GetInstanceStateOutput, error) {
	if f.instanceStateByName != nil {
		if out, ok := f.instanceStateByName[aws.ToString(input.InstanceName)]; ok {
			return out, nil
		}
	}
	if f.instanceStateOutput != nil {
		return f.instanceStateOutput, nil
	}
	return &ls.GetInstanceStateOutput{State: &types.InstanceState{Name: aws.String("running")}}, nil
}

func (f *fakeLightsailClient) DeleteInstance(ctx context.Context, input *ls.DeleteInstanceInput, optFns ...func(*ls.Options)) (*ls.DeleteInstanceOutput, error) {
	f.deleteInstanceCalls++
	if f.getInstanceByName != nil {
		delete(f.getInstanceByName, aws.ToString(input.InstanceName))
	}
	return &ls.DeleteInstanceOutput{
		Operations: []types.Operation{{}},
	}, nil
}

func (f *fakeLightsailClient) CreateInstances(ctx context.Context, input *ls.CreateInstancesInput, optFns ...func(*ls.Options)) (*ls.CreateInstancesOutput, error) {
	f.createInstancesCalls++
	f.createInstancesInput = input
	name := input.InstanceNames[0]
	if f.getInstanceByName == nil {
		f.getInstanceByName = map[string]*types.Instance{}
	}
	f.getInstanceByName[name] = &types.Instance{
		Name: aws.String(name),
		Tags: input.Tags,
	}
	if f.instanceStateByName == nil {
		f.instanceStateByName = map[string]*ls.GetInstanceStateOutput{}
	}
	f.instanceStateByName[name] = &ls.GetInstanceStateOutput{State: &types.InstanceState{Name: aws.String("running")}}
	return &ls.CreateInstancesOutput{}, nil
}

func (f *fakeLightsailClient) CreateInstancesFromSnapshot(ctx context.Context, input *ls.CreateInstancesFromSnapshotInput, optFns ...func(*ls.Options)) (*ls.CreateInstancesFromSnapshotOutput, error) {
	f.createInstancesFromSnapshotCalls++
	f.createInstancesFromSnapshotInput = input
	name := input.InstanceNames[0]
	if f.getInstanceByName == nil {
		f.getInstanceByName = map[string]*types.Instance{}
	}
	f.getInstanceByName[name] = &types.Instance{
		Name: aws.String(name),
		Tags: input.Tags,
	}
	if f.instanceStateByName == nil {
		f.instanceStateByName = map[string]*ls.GetInstanceStateOutput{}
	}
	f.instanceStateByName[name] = &ls.GetInstanceStateOutput{State: &types.InstanceState{Name: aws.String("running")}}
	return &ls.CreateInstancesFromSnapshotOutput{}, nil
}

func (f *fakeLightsailClient) PutInstancePublicPorts(ctx context.Context, input *ls.PutInstancePublicPortsInput, optFns ...func(*ls.Options)) (*ls.PutInstancePublicPortsOutput, error) {
	f.putInstancePublicPortsCalls++
	return &ls.PutInstancePublicPortsOutput{}, nil
}

func (f *fakeLightsailClient) GetInstanceAccessDetails(ctx context.Context, input *ls.GetInstanceAccessDetailsInput, optFns ...func(*ls.Options)) (*ls.GetInstanceAccessDetailsOutput, error) {
	if f.getInstanceAccessDetailsOutput != nil {
		return f.getInstanceAccessDetailsOutput, nil
	}
	return &ls.GetInstanceAccessDetailsOutput{
		AccessDetails: &types.InstanceAccessDetails{
			Username:  aws.String("ec2-user"),
			IpAddress: aws.String("1.2.3.4"),
		},
	}, nil
}

func (f *fakeLightsailClient) GetInstance(ctx context.Context, input *ls.GetInstanceInput, optFns ...func(*ls.Options)) (*ls.GetInstanceOutput, error) {
	name := aws.ToString(input.InstanceName)
	if f.getInstanceByName != nil {
		if inst, ok := f.getInstanceByName[name]; ok {
			return &ls.GetInstanceOutput{Instance: inst}, nil
		}
		return nil, &types.NotFoundException{}
	}
	if f.getInstanceOutput != nil {
		return f.getInstanceOutput, nil
	}
	return nil, &types.NotFoundException{}
}

func (f *fakeLightsailClient) GetInstanceSnapshots(ctx context.Context, input *ls.GetInstanceSnapshotsInput, optFns ...func(*ls.Options)) (*ls.GetInstanceSnapshotsOutput, error) {
	if f.getInstanceSnapshotsOutput != nil {
		return f.getInstanceSnapshotsOutput, nil
	}
	return &ls.GetInstanceSnapshotsOutput{}, nil
}

func (f *fakeLightsailClient) GetInstances(ctx context.Context, input *ls.GetInstancesInput, optFns ...func(*ls.Options)) (*ls.GetInstancesOutput, error) {
	if f.getInstancesOutput != nil {
		return f.getInstancesOutput, nil
	}
	return &ls.GetInstancesOutput{}, nil
}

func (f *fakeLightsailClient) GetRegions(ctx context.Context, input *ls.GetRegionsInput, optFns ...func(*ls.Options)) (*ls.GetRegionsOutput, error) {
	if f.getRegionsOutput != nil {
		return f.getRegionsOutput, nil
	}
	return &ls.GetRegionsOutput{
		Regions: []types.Region{
			{
				Name: types.RegionName(DefaultRegion),
				AvailabilityZones: []types.AvailabilityZone{
					{ZoneName: aws.String(DefaultRegion + "a")},
				},
			},
		},
	}, nil
}

func (f *fakeLightsailClient) GetBlueprints(ctx context.Context, input *ls.GetBlueprintsInput, optFns ...func(*ls.Options)) (*ls.GetBlueprintsOutput, error) {
	if f.getBlueprintsOutput != nil {
		return f.getBlueprintsOutput, nil
	}
	return &ls.GetBlueprintsOutput{
		Blueprints: []types.Blueprint{
			{
				BlueprintId: aws.String("amazon-linux-2023"),
				Group:       aws.String("amazon_linux_2023"),
				IsActive:    aws.Bool(true),
				Platform:    types.InstancePlatformLinuxUnix,
				Type:        types.BlueprintTypeOs,
			},
		},
	}, nil
}

func (f *fakeLightsailClient) GetBundles(ctx context.Context, input *ls.GetBundlesInput, optFns ...func(*ls.Options)) (*ls.GetBundlesOutput, error) {
	if f.getBundlesOutput != nil {
		return f.getBundlesOutput, nil
	}
	return &ls.GetBundlesOutput{
		Bundles: []types.Bundle{
			{
				BundleId:     aws.String("small"),
				InstanceType: aws.String("small"),
				CpuCount:     aws.Int32(2),
				RamSizeInGb:  aws.Float32(2),
			},
		},
	}, nil
}

func TestMergeTags(t *testing.T) {
	merged := mergeTags(
		map[string]string{"stack": "pullpreview", "repo": "action"},
		map[string]string{"repo": "fork", "env": "pr"},
	)
	if merged["stack"] != "pullpreview" || merged["repo"] != "fork" || merged["env"] != "pr" {
		t.Fatalf("unexpected merged tags: %#v", merged)
	}
}

func TestMustAtoi(t *testing.T) {
	cases := map[string]int{
		"22":   22,
		" 443": 443,
		"":     0,
		"abc":  0,
		"12x":  0,
	}
	for input, want := range cases {
		if got := mustAtoi(input); got != want {
			t.Fatalf("mustAtoi(%q)=%d, want %d", input, got, want)
		}
	}
}

func TestMatchTags(t *testing.T) {
	actual := []types.Tag{
		{Key: strPtr("stack"), Value: strPtr("pullpreview")},
		{Key: strPtr("repo"), Value: strPtr("action")},
	}
	if !matchTags(actual, map[string]string{"stack": "pullpreview"}) {
		t.Fatalf("expected required subset to match")
	}
	if matchTags(actual, map[string]string{"repo": "other"}) {
		t.Fatalf("expected mismatched tag to fail")
	}
}

func TestTagsConversions(t *testing.T) {
	input := map[string]string{"a": "1", "b": "2"}
	lightTags := toLightsailTags(input)
	if len(lightTags) != 2 {
		t.Fatalf("unexpected lightsail tags: %#v", lightTags)
	}
	back := tagsToMap(lightTags)
	if back["a"] != "1" || back["b"] != "2" {
		t.Fatalf("unexpected converted map: %#v", back)
	}
}

func TestReverseSizeMap(t *testing.T) {
	if got := reverseSizeMap("small"); got != "S" {
		t.Fatalf("reverseSizeMap(small)=%q, want S", got)
	}
	if got := reverseSizeMap("custom"); got != "custom" {
		t.Fatalf("reverseSizeMap(custom)=%q, want custom", got)
	}
}

func TestUsername(t *testing.T) {
	if got := (&Provider{}).Username(); got != "ec2-user" {
		t.Fatalf("Username()=%q, want ec2-user", got)
	}
}

func TestSupportsDeploymentTarget(t *testing.T) {
	p := &Provider{}
	if !p.SupportsDeploymentTarget(pullpreview.DeploymentTargetCompose) {
		t.Fatalf("expected compose support")
	}
	if !p.SupportsDeploymentTarget(pullpreview.DeploymentTargetHelm) {
		t.Fatalf("expected helm support")
	}
}

func TestBuildUserDataForHelmUsesSharedBootstrap(t *testing.T) {
	p := &Provider{}
	script, err := p.BuildUserData(pullpreview.UserDataOptions{
		AppPath:          "/app",
		Username:         "ec2-user",
		DeploymentTarget: pullpreview.DeploymentTargetHelm,
	})
	if err != nil {
		t.Fatalf("BuildUserData() error: %v", err)
	}
	for _, fragment := range []string{
		"--write-kubeconfig-mode 0644",
		"get-helm-3",
		"test -s /swapfile",
		"systemctl mask tmp.mount",
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("expected script to contain %q, script:\n%s", fragment, script)
		}
	}
}

func TestLaunchOrRestoreSkipsSnapshotForHelm(t *testing.T) {
	client := &fakeLightsailClient{
		getInstanceSnapshotsOutput: &ls.GetInstanceSnapshotsOutput{
			InstanceSnapshots: []types.InstanceSnapshot{
				{
					Name:             aws.String("snap-1"),
					FromInstanceName: aws.String("demo"),
					State:            types.InstanceSnapshotStateAvailable,
				},
			},
		},
	}
	p := &Provider{client: client, ctx: context.Background(), region: DefaultRegion}

	err := p.launchOrRestore("demo", pullpreview.LaunchOptions{
		Tags: map[string]string{"pullpreview_target": "helm"},
	})
	if err != nil {
		t.Fatalf("launchOrRestore() error: %v", err)
	}
	if client.createInstancesCalls != 1 {
		t.Fatalf("expected create instance call, got %d", client.createInstancesCalls)
	}
	if client.createInstancesFromSnapshotCalls != 0 {
		t.Fatalf("did not expect snapshot restore for helm, got %d", client.createInstancesFromSnapshotCalls)
	}
}

func TestLaunchOrRestoreRestoresSnapshotForCompose(t *testing.T) {
	client := &fakeLightsailClient{
		getInstanceSnapshotsOutput: &ls.GetInstanceSnapshotsOutput{
			InstanceSnapshots: []types.InstanceSnapshot{
				{
					Name:             aws.String("snap-1"),
					FromInstanceName: aws.String("demo"),
					State:            types.InstanceSnapshotStateAvailable,
				},
			},
		},
	}
	p := &Provider{client: client, ctx: context.Background(), region: DefaultRegion}

	err := p.launchOrRestore("demo", pullpreview.LaunchOptions{
		Tags: map[string]string{"pullpreview_target": "compose"},
	})
	if err != nil {
		t.Fatalf("launchOrRestore() error: %v", err)
	}
	if client.createInstancesFromSnapshotCalls != 1 {
		t.Fatalf("expected snapshot restore for compose, got %d", client.createInstancesFromSnapshotCalls)
	}
	if client.createInstancesCalls != 0 {
		t.Fatalf("did not expect plain create instance call, got %d", client.createInstancesCalls)
	}
}

func TestLaunchOrRestoreSkipsSnapshotWhenFreshCreateRetryRequested(t *testing.T) {
	client := &fakeLightsailClient{
		getInstanceSnapshotsOutput: &ls.GetInstanceSnapshotsOutput{
			InstanceSnapshots: []types.InstanceSnapshot{
				{
					Name:             aws.String("snap-1"),
					FromInstanceName: aws.String("demo"),
					State:            types.InstanceSnapshotStateAvailable,
				},
			},
		},
	}
	p := &Provider{client: client, ctx: context.Background(), region: DefaultRegion}

	err := p.launchOrRestore("demo", pullpreview.LaunchOptions{
		Tags:        map[string]string{"pullpreview_target": "compose"},
		SkipRestore: true,
	})
	if err != nil {
		t.Fatalf("launchOrRestore() error: %v", err)
	}
	if client.createInstancesCalls != 1 {
		t.Fatalf("expected plain create for skip-restore launch, got %d", client.createInstancesCalls)
	}
	if client.createInstancesFromSnapshotCalls != 0 {
		t.Fatalf("did not expect snapshot restore when skip_restore=true, got %d", client.createInstancesFromSnapshotCalls)
	}
}

func TestLaunchRecreatesMismatchedDeploymentIdentity(t *testing.T) {
	name := "gh-1-pr-1"
	client := &fakeLightsailClient{
		getInstanceByName: map[string]*types.Instance{
			name: {
				Name: aws.String(name),
				Tags: []types.Tag{
					{Key: strPtr("pullpreview_label"), Value: strPtr("pullpreview-helm")},
					{Key: strPtr("pullpreview_target"), Value: strPtr("helm")},
					{Key: strPtr("pullpreview_runtime"), Value: strPtr("k3s")},
				},
			},
		},
	}
	p := &Provider{client: client, ctx: context.Background(), region: DefaultRegion}

	_, err := p.Launch(name, pullpreview.LaunchOptions{
		Tags: map[string]string{
			"pullpreview_label":   "pullpreview",
			"pullpreview_target":  "compose",
			"pullpreview_runtime": "docker",
		},
	})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}
	if client.deleteInstanceCalls != 1 {
		t.Fatalf("expected mismatched instance delete, got %d", client.deleteInstanceCalls)
	}
	if client.createInstancesCalls != 1 {
		t.Fatalf("expected recreate after delete, got %d", client.createInstancesCalls)
	}
	if client.putInstancePublicPortsCalls != 1 {
		t.Fatalf("expected firewall setup after recreate, got %d", client.putInstancePublicPortsCalls)
	}
}

func strPtr(v string) *string { return &v }
