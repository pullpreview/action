require 'terminal-table'

module PullPreview
  class List
    def self.run(opts)
      raise Error, "Invalid org/repo given" if opts.arguments.none?
      org, repo = opts.arguments.first.split("/", 2)

      table = Terminal::Table.new(headings: [
        "Name",
        "IP",
        "Size",
        "Region",
        "AZ",
        "Created on",
        "Tags",
      ])
      tags_to_find = {
        "stack" => STACK_NAME,
      }
      tags_to_find.merge!("repo_name" => repo) if repo
      tags_to_find.merge!("org_name" => org) if org

      PullPreview.provider.list_instances(tags: tags_to_find) do |instance|
        table << [
          instance.name,
          instance.public_ip,
          instance.size,
          instance.region,
          instance.zone,
          instance.created_at.iso8601,
          instance.tags.map{|tag| [tag.key, tag.value].join(":")}.join(","),
        ]
      end
      puts table
    end
  end
end
