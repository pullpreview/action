require "logger"
require "fileutils"
require "aws-sdk-lightsail"

require_relative "./pull_preview/error"
require_relative "./pull_preview/instance"
require_relative "./pull_preview/up"
require_relative "./pull_preview/down"
require_relative "./pull_preview/sync_with_github"
require_relative "./pull_preview/list"

module PullPreview
  VERSION = "1.0.0"
  REMOTE_APP_PATH = "/app"
  STACK_NAME = "pullpreview"
  LICENSE_STATUS_URL = "https://api.pullpreview.com/license_status"

  class << self
    attr_accessor :logger
    attr_accessor :lightsail
  end

  def self.data_dir
    Pathname.new(__dir__).parent.join("data")
  end

  def self.octokit
    @octokit ||= Octokit::Client.new(access_token: ENV.fetch("GITHUB_TOKEN")).tap do |client|
      client.auto_paginate = false
    end
  end

  def self.faraday
    @faraday ||= Faraday.new(request: { timeout: 10, open_timeout: 5 }) do |conn|
      conn.request(:retry, max: 2)
      conn.request  :url_encoded
    end
  end

  def self.check_license!(params)
    response = PullPreview.faraday.get(LICENSE_STATUS_URL, params)
    if response.status.to_s == "403"
      raise LicenseError, "No valid license found. Please go to https://pullpreview.com to buy or renew your license."
    end
    PullPreview.logger.info "License status: #{response.body.to_s.chomp}"
    true
  rescue Faraday::Error => e
    PullPreview.logger.warn "Got the following error while checking the license: #{e.message}"
    true
  end
end
