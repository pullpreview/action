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
end
