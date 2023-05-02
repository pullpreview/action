require "logger"
require "fileutils"

require_relative "./pull_preview/providers"
require_relative "./pull_preview/error"
require_relative "./pull_preview/utils"
require_relative "./pull_preview/user_data"
require_relative "./pull_preview/access_details"
require_relative "./pull_preview/instance"
require_relative "./pull_preview/up"
require_relative "./pull_preview/down"
require_relative "./pull_preview/github_sync"
require_relative "./pull_preview/list"
require_relative "./pull_preview/license"

module PullPreview
  VERSION = "1.0.0"
  REMOTE_APP_PATH = "/app"
  STACK_NAME = "pullpreview"

  class << self
    attr_accessor :logger
    attr_reader :license, :provider
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

  def self.license=(value)
    @license = value
    @license = nil if @license.empty?
    @license
  end

  # +value+: one of lightsail,pullpreview
  def self.provider=(value)
    @provider = Providers.fetch(value)
  end
end
