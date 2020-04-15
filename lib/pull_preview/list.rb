require 'terminal-table'

module PullPreview
  class List
    def self.run(cli_args)
      next_page_token = nil
      table = Terminal::Table.new(headings: [
        "Name",
        "IP",
        "Region",
        "AZ",
        "Created on",
      ])
      begin
        result = PullPreview.lightsail.get_instances(next_page_token: next_page_token) 
        next_page_token = result.next_page_token
        result.instances.each do |instance|
          if instance.tags.find{|tag| tag.key == "stack" && tag.value == STACK_NAME}
            table << [
              instance.name,
              instance.public_ip_address,
              instance.location.region_name,
              instance.location.availability_zone,
              instance.created_at.iso8601,
            ]
          end
        end
      end while not next_page_token.nil?
      puts table
    end
  end
end
