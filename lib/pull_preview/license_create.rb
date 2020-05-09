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
    def self.run(cli_args)
      opts = Slop.parse do |o|
        o.banner = "Usage: pullpreview license-create [options]"
        o.string "--output-file", "output file"
        o.string "--type",       "license type [trial]", required: true
        o.date   "--expires-at", "expiration date"
        o.int    "--expires-in", "expiration days from today"
        o.on "--help" do
          puts command
          puts
          puts "To generate a private key, use:"
          puts "openssl openssl genrsa 2048 -out license_key"
          puts
          puts "To generate the matching public key, use:"
          puts "openssl rsa -in license_key -pubout -out license_key.pub"
          exit
        end
      end

      if opts["expires_in"]
        opts["expires_at"] = Date.today + opts["expires_in"]
      end

      # In the license generation application, load the private key from a file.
      private_key = OpenSSL::PKey::RSA.new File.read("license_key")
      PullPreview::License.encryption_key = private_key

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
