package lightsail

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	ls "github.com/aws/aws-sdk-go-v2/service/lightsail"
	"github.com/aws/aws-sdk-go-v2/service/lightsail/types"
	"github.com/pullpreview/action/internal/pullpreview"
)

var sizeMap = map[string]string{
	"XXS": "nano",
	"XS":  "micro",
	"S":   "small",
	"M":   "medium",
	"L":   "large",
	"XL":  "xlarge",
	"2XL": "2xlarge",
}

type Provider struct {
	client *ls.Client
	region string
	logger *pullpreview.Logger
}

func New(ctx context.Context, region string, logger *pullpreview.Logger) (*Provider, error) {
	if region == "" {
		region = "us-east-1"
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return &Provider{
		client: ls.NewFromConfig(cfg),
		region: region,
		logger: logger,
	}, nil
}

func (p *Provider) Running(name string) (bool, error) {
	resp, err := p.client.GetInstanceState(context.Background(), &ls.GetInstanceStateInput{InstanceName: aws.String(name)})
	if err != nil {
		var nf *types.NotFoundException
		if errors.As(err, &nf) {
			return false, nil
		}
		return false, err
	}
	return resp.State != nil && aws.ToString(resp.State.Name) == "running", nil
}

func (p *Provider) Terminate(name string) error {
	resp, err := p.client.DeleteInstance(context.Background(), &ls.DeleteInstanceInput{InstanceName: aws.String(name)})
	if err != nil {
		return err
	}
	if len(resp.Operations) == 0 {
		return nil
	}
	if resp.Operations[0].ErrorCode != nil {
		return errors.New(*resp.Operations[0].ErrorCode)
	}
	return nil
}

func (p *Provider) Launch(name string, opts pullpreview.LaunchOptions) (pullpreview.AccessDetails, error) {
	running, err := p.Running(name)
	if err != nil {
		return pullpreview.AccessDetails{}, err
	}
	if !running {
		if err := p.launchOrRestore(name, opts); err != nil {
			return pullpreview.AccessDetails{}, err
		}
		if err := p.waitUntilRunning(name); err != nil {
			return pullpreview.AccessDetails{}, err
		}
	}
	if err := p.setupFirewall(name, opts.CIDRs, opts.Ports); err != nil {
		return pullpreview.AccessDetails{}, err
	}
	return p.fetchAccessDetails(name)
}

func (p *Provider) launchOrRestore(name string, opts pullpreview.LaunchOptions) error {
	bundleID, err := p.bundleID(opts.Size)
	if err != nil {
		return err
	}
	zones, err := p.availabilityZones()
	if err != nil || len(zones) == 0 {
		return errors.New("no availability zones")
	}
	params := &ls.CreateInstancesInput{
		InstanceNames:    []string{name},
		AvailabilityZone: aws.String(zones[0]),
		BundleId:         aws.String(bundleID),
		Tags:             toLightsailTags(mergeTags(map[string]string{"stack": pullpreview.StackName}, opts.Tags)),
		UserData:         aws.String(opts.UserData),
		BlueprintId:      aws.String(p.blueprintID()),
	}

	snapshot := p.latestSnapshot(name)
	if snapshot != nil {
		if p.logger != nil {
			p.logger.Infof("Found snapshot to restore from: %s", aws.ToString(snapshot.Name))
		}
		_, err := p.client.CreateInstancesFromSnapshot(context.Background(), &ls.CreateInstancesFromSnapshotInput{
			InstanceNames:        []string{name},
			AvailabilityZone:     aws.String(zones[0]),
			BundleId:             aws.String(bundleID),
			Tags:                 params.Tags,
			UserData:             aws.String(opts.UserData),
			InstanceSnapshotName: snapshot.Name,
		})
		return err
	}

	_, err = p.client.CreateInstances(context.Background(), params)
	return err
}

func mergeTags(base, extra map[string]string) map[string]string {
	result := map[string]string{}
	for k, v := range base {
		result[k] = v
	}
	for k, v := range extra {
		result[k] = v
	}
	return result
}

func (p *Provider) waitUntilRunning(name string) error {
	ok := pullpreview.WaitUntil(30, 5*time.Second, func() bool {
		running, err := p.Running(name)
		if err != nil {
			return false
		}
		return running
	})
	if !ok {
		return errors.New("timeout while waiting for instance running")
	}
	return nil
}

func (p *Provider) setupFirewall(name string, cidrs, ports []string) error {
	portInfos := []types.PortInfo{}
	for _, portDef := range ports {
		portDef = strings.TrimSpace(portDef)
		if portDef == "" {
			continue
		}
		portRange := portDef
		protocol := "tcp"
		if strings.Contains(portDef, "/") {
			parts := strings.SplitN(portDef, "/", 2)
			portRange = parts[0]
			if parts[1] != "" {
				protocol = parts[1]
			}
		}
		startEnd := strings.SplitN(portRange, "-", 2)
		start := startEnd[0]
		end := start
		if len(startEnd) == 2 && startEnd[1] != "" {
			end = startEnd[1]
		}
		useCIDRs := cidrs
		if start == "22" {
			useCIDRs = []string{"0.0.0.0/0"}
		}
		portInfos = append(portInfos, types.PortInfo{
			FromPort: int32(mustAtoi(start)),
			ToPort:   int32(mustAtoi(end)),
			Protocol: types.NetworkProtocol(protocol),
			Cidrs:    useCIDRs,
		})
	}
	_, err := p.client.PutInstancePublicPorts(context.Background(), &ls.PutInstancePublicPortsInput{
		InstanceName: aws.String(name),
		PortInfos:    portInfos,
	})
	return err
}

func mustAtoi(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	result := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		result = result*10 + int(r-'0')
	}
	return result
}

func (p *Provider) fetchAccessDetails(name string) (pullpreview.AccessDetails, error) {
	resp, err := p.client.GetInstanceAccessDetails(context.Background(), &ls.GetInstanceAccessDetailsInput{
		InstanceName: aws.String(name),
		Protocol:     types.InstanceAccessProtocolSsh,
	})
	if err != nil {
		return pullpreview.AccessDetails{}, err
	}
	if resp.AccessDetails == nil {
		return pullpreview.AccessDetails{}, errors.New("missing access details")
	}
	return pullpreview.AccessDetails{
		Username:   aws.ToString(resp.AccessDetails.Username),
		IPAddress:  aws.ToString(resp.AccessDetails.IpAddress),
		CertKey:    aws.ToString(resp.AccessDetails.CertKey),
		PrivateKey: aws.ToString(resp.AccessDetails.PrivateKey),
	}, nil
}

func (p *Provider) latestSnapshot(name string) *types.InstanceSnapshot {
	resp, err := p.client.GetInstanceSnapshots(context.Background(), &ls.GetInstanceSnapshotsInput{})
	if err != nil {
		return nil
	}
	snapshots := resp.InstanceSnapshots
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].CreatedAt == nil {
			return false
		}
		if snapshots[j].CreatedAt == nil {
			return true
		}
		return snapshots[i].CreatedAt.After(*snapshots[j].CreatedAt)
	})
	for _, snap := range snapshots {
		if snap.State == types.InstanceSnapshotStateAvailable && aws.ToString(snap.FromInstanceName) == name {
			return &snap
		}
	}
	return nil
}

func (p *Provider) ListInstances(tags map[string]string) ([]pullpreview.InstanceSummary, error) {
	result := []pullpreview.InstanceSummary{}
	var token *string
	for {
		resp, err := p.client.GetInstances(context.Background(), &ls.GetInstancesInput{PageToken: token})
		if err != nil {
			return nil, err
		}
		for _, inst := range resp.Instances {
			if !matchTags(inst.Tags, tags) {
				continue
			}
			region := ""
			zone := ""
			if inst.Location != nil {
				region = string(inst.Location.RegionName)
				zone = aws.ToString(inst.Location.AvailabilityZone)
			}
			result = append(result, pullpreview.InstanceSummary{
				Name:      aws.ToString(inst.Name),
				PublicIP:  aws.ToString(inst.PublicIpAddress),
				Size:      reverseSizeMap(aws.ToString(inst.BundleId)),
				Region:    region,
				Zone:      zone,
				CreatedAt: aws.ToTime(inst.CreatedAt),
				Tags:      tagsToMap(inst.Tags),
			})
		}
		if resp.NextPageToken == nil || *resp.NextPageToken == "" {
			break
		}
		token = resp.NextPageToken
	}
	return result, nil
}

func matchTags(actual []types.Tag, required map[string]string) bool {
	if len(required) == 0 {
		return true
	}
	lookup := map[string]string{}
	for _, tag := range actual {
		lookup[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	for k, v := range required {
		if lookup[k] != v {
			return false
		}
	}
	return true
}

func tagsToMap(tags []types.Tag) map[string]string {
	result := map[string]string{}
	for _, tag := range tags {
		result[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return result
}

func toLightsailTags(tags map[string]string) []types.Tag {
	result := make([]types.Tag, 0, len(tags))
	for k, v := range tags {
		key := k
		val := v
		result = append(result, types.Tag{Key: &key, Value: &val})
	}
	return result
}

func (p *Provider) availabilityZones() ([]string, error) {
	resp, err := p.client.GetRegions(context.Background(), &ls.GetRegionsInput{IncludeAvailabilityZones: aws.Bool(true)})
	if err != nil {
		return nil, err
	}
	for _, region := range resp.Regions {
		if string(region.Name) == p.region {
			zones := []string{}
			for _, az := range region.AvailabilityZones {
				zones = append(zones, aws.ToString(az.ZoneName))
			}
			return zones, nil
		}
	}
	return nil, errors.New("region not found")
}

func (p *Provider) blueprintID() string {
	resp, err := p.client.GetBlueprints(context.Background(), &ls.GetBlueprintsInput{})
	if err != nil {
		return ""
	}
	for _, bp := range resp.Blueprints {
		if bp.Platform == types.InstancePlatformLinuxUnix &&
			aws.ToString(bp.Group) == "amazon_linux_2023" &&
			aws.ToBool(bp.IsActive) &&
			bp.Type == types.BlueprintTypeOs {
			return aws.ToString(bp.BlueprintId)
		}
	}
	return ""
}

func (p *Provider) bundleID(size string) (string, error) {
	instanceType := ""
	if size != "" {
		instanceType = sizeMap[size]
		if instanceType == "" {
			instanceType = strings.TrimSuffix(size, "_2_0")
		}
	}
	resp, err := p.client.GetBundles(context.Background(), &ls.GetBundlesInput{})
	if err != nil {
		return "", err
	}
	for _, bundle := range resp.Bundles {
		if instanceType == "" {
			if aws.ToInt32(bundle.CpuCount) >= 1 &&
				aws.ToFloat32(bundle.RamSizeInGb) >= 2 &&
				aws.ToFloat32(bundle.RamSizeInGb) <= 3 {
				return aws.ToString(bundle.BundleId), nil
			}
			continue
		}
		if aws.ToString(bundle.InstanceType) == instanceType {
			return aws.ToString(bundle.BundleId), nil
		}
	}
	return "", errors.New("bundle not found")
}

func reverseSizeMap(bundleID string) string {
	for k, v := range sizeMap {
		if v == bundleID {
			return k
		}
	}
	return bundleID
}

func (p *Provider) Username() string {
	return "ec2-user"
}
