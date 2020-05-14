require "slop"
require "date"
require "openssl"
require_relative "license"

module Slop
  class DateOption < Option
    def call(value)
      Date.parse(value)
    end
  end
end

module PullPreview
  class LicenseCreate
    def self.run(opts)
      if opts[:expires_in]
        opts[:expires_at] = Date.today + opts[:expires_in]
      end

      # Build a new license.
      license = PullPreview::License.new(opts.to_hash)

      puts "License:"
      puts license

      # Export the license, which encrypts and encodes it.
      data = license.export

      puts "Exported license:"
      puts data

      if opts["output_file"]
        # Write the license to a file to send to a customer.
        File.open(opts["output_file"], "w") { |f| f.write(data) }
      end
    end
  end
end
