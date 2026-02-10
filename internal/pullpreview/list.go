package pullpreview

import (
	"errors"
	"fmt"
)

func RunList(opts ListOptions, provider Provider, logger *Logger) error {
	if opts.Org == "" && opts.Repo == "" {
		return errors.New("invalid org/repo given")
	}
	tags := map[string]string{
		"stack": StackName,
	}
	if opts.Repo != "" {
		tags["repo_name"] = opts.Repo
	}
	if opts.Org != "" {
		tags["org_name"] = opts.Org
	}
	instances, err := provider.ListInstances(tags)
	if err != nil {
		return err
	}
	fmt.Printf("Name\tIP\tSize\tRegion\tAZ\tCreated On\tTags\n")
	for _, inst := range instances {
		fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%v\n", inst.Name, inst.PublicIP, inst.Size, inst.Region, inst.Zone, inst.CreatedAt.Format("2006-01-02T15:04:05Z"), inst.Tags)
	}
	return nil
}
