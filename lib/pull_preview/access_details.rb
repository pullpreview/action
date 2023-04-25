module PullPreview
  class AccessDetails
    attr_reader :username, :ip_address, :private_key, :cert_key

    def initialize(username:, ip_address:, private_key: nil, cert_key: nil)
      @username = username
      @ip_address = ip_address
      @private_key = private_key
      @cert_key = cert_key
    end

    def ssh_address
      [username, ip_address].compact.join("@")
    end
  end
end