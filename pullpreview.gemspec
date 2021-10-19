Gem::Specification.new do |spec|
  spec.name               = "pullpreview"
  spec.version            = "0.0.2"

  spec.authors = ["Cyril Rohr", "Manuel Fittko"]
  spec.date = "2021-10-19"
  spec.description = "1-click preview environments for GitHub repositories."
  spec.email = "info@mfittko.com"
  spec.files = Dir.glob("lib/**/*")
  spec.executables   = ["pullpreview"]
  spec.homepage = "https://pullpreview.com/"
  spec.summary = "pullpreview!"
  spec.required_ruby_version = ">= 2.4.0"
  spec.add_dependency "aws-sdk-lightsail", "~> 1.30"
  spec.add_dependency "slop", "~> 4.8"
  spec.add_dependency "octokit", "~> 4.18"
  spec.add_dependency "terminal-table", "~> 1.8"
end
