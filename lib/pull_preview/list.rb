require 'terminal-table'

module PullPreview
  class List
    def self.run(cli_args)
      opts = Slop.parse do |o|
        o.banner = "Usage: pullpreview list [options]"
        o.string '--org', 'Restrict to given organization name'
        o.string '--repo', 'Restrict to given repository name'
        o.on '--help' do
          puts o
          exit
        end
      end

      next_page_token = nil
      table = Terminal::Table.new(headings: [
        "Name",
        "IP",
        "Type",
        "Region",
        "AZ",
        "Created on",
        "Tags",
      ])
      tags_to_find = {
        "stack" => STACK_NAME,
      }
      tags_to_find.merge!("repo_name" => opts[:repo]) if opts[:repo]
      tags_to_find.merge!("org_name" => opts[:org]) if opts[:org]

      begin
        result = PullPreview.lightsail.get_instances(next_page_token: next_page_token) 
        next_page_token = result.next_page_token
        result.instances.each do |instance|
          matching_tags = Hash[instance.tags.select{|tag| tags_to_find.keys.include?(tag.key)}.map{|tag| [tag.key, tag.value]}]
          if matching_tags == tags_to_find
            table << [
              instance.name,
              instance.public_ip_address,
              instance.bundle_id,
              instance.location.region_name,
              instance.location.availability_zone,
              instance.created_at.iso8601,
              instance.tags.map{|tag| [tag.key, tag.value].join(":")}.join(","),
            ]
          end
        end
      end while not next_page_token.nil?
      puts table
    end
  end
end
